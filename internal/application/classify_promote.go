package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

const commonLanguage = "common"

type ClassifyAndPromoteInput struct {
	PromptVersion      string
	MaxCommentsPerCall int
	RulePrefix         string
	IDWidth            int
	Languages          []string
	LanguageCodes      map[string]string
	Thresholds         PromotionThresholds
}

type ClassifyAndPromoteSummary struct {
	CommentsClassified int
	CandidatesMerged   int
	RulesPromoted      []string
}

type ClassifyAndPromoteDeps struct {
	Comments        classifyCommentRepository
	Classifications classifyClassificationRepository
	Candidates      classifyCandidateRepository
	Reactions       classifyReactionRepository
	Rules           classifyRuleRepository
}

type classifyCommentRepository interface {
	GetByID(context.Context, int64) (domain.Comment, bool, error)
	ListPendingClassification(context.Context, string) ([]domain.Comment, error)
}
type classifyClassificationRepository interface {
	Save(context.Context, domain.Classification) error
}
type classifyCandidateRepository interface {
	Upsert(context.Context, domain.CandidateCluster) (domain.CandidateCluster, error)
	Get(context.Context, string) (domain.CandidateCluster, bool, error)
	ListOpen(context.Context, string) ([]domain.CandidateCluster, error)
	ListMergedInto(context.Context, string) ([]domain.CandidateCluster, error)
	ListEvidence(context.Context, string) ([]domain.CandidateEvidence, error)
	AttachEvidence(context.Context, domain.CandidateEvidence) error
	Merge(context.Context, string, []string) error
	Promote(context.Context, string, string) error
}
type classifyReactionRepository interface {
	CountByComment(context.Context, int64, string) (int, error)
}
type classifyRuleRepository interface {
	Insert(context.Context, domain.Rule) error
	Get(context.Context, string) (domain.Rule, bool, error)
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
	NextSequence(context.Context, string) (int, error)
}

func ClassifyAndPromote(ctx context.Context, classifier ports.Classifier, repos ClassifyAndPromoteDeps, clock ports.Clock, input ClassifyAndPromoteInput) (ClassifyAndPromoteSummary, error) {
	summary := ClassifyAndPromoteSummary{}

	pending, err := repos.Comments.ListPendingClassification(ctx, input.PromptVersion)
	if err != nil {
		return summary, fmt.Errorf("application: classify: list pending comments: %w", err)
	}

	byLanguage := make(map[string][]domain.Comment)
	for _, comment := range pending {
		if comment.Origin != domain.OriginHuman && comment.Origin != domain.OriginAICandidate {
			continue
		}
		byLanguage[comment.Language] = append(byLanguage[comment.Language], comment)
	}

	for language, comments := range byLanguage {
		catalog, err := buildCatalog(ctx, repos.Rules, language)
		if err != nil {
			return summary, err
		}

		for _, batch := range chunkComments(comments, input.MaxCommentsPerCall) {
			openCandidates, err := repos.Candidates.ListOpen(ctx, language)
			if err != nil {
				return summary, err
			}

			result, err := classifier.Classify(ctx, ports.ClassificationInput{
				PromptVersion:  input.PromptVersion,
				Comments:       buildCommentInputs(batch),
				Catalog:        catalog,
				OpenCandidates: buildCandidateEntries(openCandidates),
			})
			if err != nil {
				return summary, fmt.Errorf("application: classify: %s: %w", language, err)
			}

			verdictByCommentID := make(map[int64]ports.CommentVerdict, len(result.Verdicts))
			for _, v := range result.Verdicts {
				verdictByCommentID[v.CommentID] = v
			}

			for _, comment := range batch {
				verdict, ok := verdictByCommentID[comment.ID]
				if !ok {
					return summary, fmt.Errorf("application: classify: comment %d has no verdict in the response", comment.ID)
				}

				if err := repos.Classifications.Save(ctx, domain.Classification{
					CommentID:     comment.ID,
					InputHash:     comment.InputHash,
					PromptVersion: input.PromptVersion,
					Model:         result.Model,
					Verdict:       verdict.Verdict,
					RawPayload:    verdict.RawPayload,
					CreatedAt:     clock.Now(),
				}); err != nil {
					return summary, fmt.Errorf("application: classify: save classification for %d: %w", comment.ID, err)
				}
				summary.CommentsClassified++

				switch verdict.Verdict {
				case domain.VerdictNew, domain.VerdictNewCandidate:
					if err := attachCandidateEvidence(ctx, repos.Candidates, repos.Reactions, clock, language, verdict.CandidateKey, comment); err != nil {
						return summary, err
					}
				case domain.VerdictCandidateMatch:
					candidate, ok, err := repos.Candidates.Get(ctx, verdict.CandidateID)
					if err != nil {
						return summary, err
					}
					if !ok || candidate.Status != domain.CandidateOpen {
						return summary, fmt.Errorf("application: classify: unknown open candidate %q", verdict.CandidateID)
					}
					if err := repos.Candidates.AttachEvidence(ctx, domain.CandidateEvidence{CandidateID: candidate.ID, CommentID: comment.ID, Kind: domain.EvidenceHuman, EvaluatedAt: clock.Now()}); err != nil {
						return summary, err
					}
				}
			}
		}
	}

	for _, language := range input.Languages {
		merged, err := compactCandidates(ctx, classifier, repos.Candidates, language)
		if err != nil {
			return summary, err
		}
		summary.CandidatesMerged += merged

		promoted, err := promoteEligibleCandidates(ctx, classifier, repos, clock, language, input)
		if err != nil {
			return summary, err
		}
		summary.RulesPromoted = append(summary.RulesPromoted, promoted...)
	}

	return summary, nil
}

func attachCandidateEvidence(ctx context.Context, candidates classifyCandidateRepository, reactions classifyReactionRepository, clock ports.Clock, language string, candidateKey string, comment domain.Comment) error {
	if candidateKey == "" {
		return fmt.Errorf("application: classify: new verdict has no candidate_key (comment %d)", comment.ID)
	}

	id := candidateID(language, candidateKey)
	targetID := id

	existing, ok, err := candidates.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("application: classify: look up candidate %q: %w", id, err)
	}
	switch {
	case !ok:
		now := clock.Now()
		if _, err := candidates.Upsert(ctx, domain.CandidateCluster{
			ID:        id,
			Language:  language,
			Label:     candidateKey,
			Status:    domain.CandidateOpen,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("application: classify: create candidate %q: %w", id, err)
		}
	case existing.Status == domain.CandidateMerged && existing.MergedInto != "":
		targetID = existing.MergedInto
	case existing.Status != domain.CandidateOpen:
		return nil
	}

	kind := domain.EvidenceHuman
	thumbsUp, thumbsDown := 0, 0
	if comment.AuthorKind == domain.AuthorAI {
		kind = domain.EvidenceAICandidate
		if thumbsUp, err = reactions.CountByComment(ctx, comment.ID, "+1"); err != nil {
			return fmt.Errorf("application: classify: contar +1 de %d: %w", comment.ID, err)
		}
		if thumbsDown, err = reactions.CountByComment(ctx, comment.ID, "-1"); err != nil {
			return fmt.Errorf("application: classify: contar -1 de %d: %w", comment.ID, err)
		}
	}

	if err := candidates.AttachEvidence(ctx, domain.CandidateEvidence{
		CandidateID: targetID,
		CommentID:   comment.ID,
		Kind:        kind,
		ThumbsUp:    thumbsUp,
		ThumbsDown:  thumbsDown,
		EvaluatedAt: clock.Now(),
	}); err != nil {
		return fmt.Errorf("application: classify: attach evidence to %q: %w", targetID, err)
	}
	return nil
}

func compactCandidates(ctx context.Context, classifier ports.Classifier, candidates classifyCandidateRepository, language string) (int, error) {
	open, err := candidates.ListOpen(ctx, language)
	if err != nil {
		return 0, fmt.Errorf("application: compact: list candidates for %s: %w", language, err)
	}
	if len(open) < 2 {
		return 0, nil
	}

	result, err := classifier.Compact(ctx, ports.CompactionInput{Language: language, Candidates: buildCandidateEntries(open)})
	if err != nil {
		return 0, fmt.Errorf("application: compact: %s: %w", language, err)
	}

	merged := 0
	for _, m := range result.Merges {
		if err := candidates.Merge(ctx, m.SurvivingCandidateID, m.MergedCandidateIDs); err != nil {
			return merged, fmt.Errorf("application: compact: fusionar en %q: %w", m.SurvivingCandidateID, err)
		}
		merged += len(m.MergedCandidateIDs)
	}
	return merged, nil
}

func promoteEligibleCandidates(ctx context.Context, classifier ports.Classifier, repos ClassifyAndPromoteDeps, clock ports.Clock, language string, input ClassifyAndPromoteInput) ([]string, error) {
	languageCode, hasCode := input.LanguageCodes[language]

	open, err := repos.Candidates.ListOpen(ctx, language)
	if err != nil {
		return nil, fmt.Errorf("application: promote: list candidates for %s: %w", language, err)
	}

	var promoted []string
	for _, candidate := range open {
		evidence, err := candidateEvidenceIncludingMerged(ctx, repos.Candidates, candidate.ID)
		if err != nil {
			return promoted, err
		}
		if !EvaluateCandidatePromotion(evidence, input.Thresholds) {
			continue
		}
		if !hasCode {
			return promoted, fmt.Errorf("application: promote: %q has no code configured in rules.language_codes; cannot promote %q", language, candidate.ID)
		}

		evidenceComments, err := loadEvidenceComments(ctx, repos.Comments, evidence)
		if err != nil {
			return promoted, err
		}

		draft, err := classifier.DraftRule(ctx, ports.DraftRuleInput{CandidateID: candidate.ID, Language: language, Evidence: evidenceComments})
		if err != nil {
			return promoted, fmt.Errorf("application: promote: draft rule for %q: %w", candidate.ID, err)
		}
		if draft.Rule == "" {
			draft.Rule = draft.Body
		}
		if strings.TrimSpace(draft.Rule) == "" {
			return promoted, fmt.Errorf("application: promote: draft rule for %q has no normalized rule", candidate.ID)
		}
		normalizedContent := renderNormalizedRule(draft)
		draft.Body = strings.TrimSpace(draft.Rule)

		ruleID, err := nextAvailableRuleID(ctx, repos.Rules, input.RulePrefix, language, languageCode, input.IDWidth)
		if err != nil {
			return promoted, err
		}

		now := clock.Now()
		if err := repos.Rules.Insert(ctx, domain.Rule{
			RuleID:      ruleID,
			Language:    language,
			Title:       draft.Title,
			Scope:       draft.Scope,
			Body:        draft.Body,
			Rule:        draft.Rule,
			Exception:   draft.Exception,
			BadExample:  draft.Bad,
			GoodExample: draft.Good,
			Status:      domain.RuleActive,
			CreatedAt:   now,
			ActivatedAt: now,
			Source:      "classifier",
			ContentHash: contentHash(normalizedContent),
		}); err != nil {
			return promoted, fmt.Errorf("application: promote: insert %q: %w", ruleID, err)
		}
		if err := repos.Candidates.Promote(ctx, candidate.ID, ruleID); err != nil {
			return promoted, fmt.Errorf("application: promote: mark %q as promoted: %w", candidate.ID, err)
		}
		promoted = append(promoted, ruleID)
	}
	return promoted, nil
}

func renderNormalizedRule(draft ports.RuleDraft) string {
	fields := [][2]string{{"Scope", draft.Scope}, {"Rule", draft.Rule}, {"Exception", draft.Exception}, {"Bad", draft.Bad}, {"Good", draft.Good}}
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field[1]); value != "" {
			if field[0] == "Scope" {
				value = "`" + strings.Trim(value, "`") + "`"
			}
			lines = append(lines, "- **"+field[0]+":** "+value)
		}
	}
	return strings.Join(lines, "\n")
}

func nextAvailableRuleID(ctx context.Context, rules classifyRuleRepository, prefix string, language string, languageCode string, idWidth int) (string, error) {
	for {
		seq, err := rules.NextSequence(ctx, languageCode)
		if err != nil {
			return "", fmt.Errorf("application: promote: siguiente secuencia de %q: %w", languageCode, err)
		}
		ruleID := fmt.Sprintf("%s-%s-%0*d", prefix, languageCode, idWidth, seq)
		if _, exists, err := rules.Get(ctx, ruleID); err != nil {
			return "", fmt.Errorf("application: promote: check ID %q for %s: %w", ruleID, language, err)
		} else if !exists {
			return ruleID, nil
		}
	}
}

func candidateEvidenceIncludingMerged(ctx context.Context, candidates classifyCandidateRepository, candidateID string) ([]domain.CandidateEvidence, error) {
	evidence, err := candidates.ListEvidence(ctx, candidateID)
	if err != nil {
		return nil, fmt.Errorf("application: promote: list evidence for %q: %w", candidateID, err)
	}

	mergedChildren, err := candidates.ListMergedInto(ctx, candidateID)
	if err != nil {
		return nil, fmt.Errorf("application: promote: list candidates merged into %q: %w", candidateID, err)
	}
	for _, child := range mergedChildren {
		childEvidence, err := candidates.ListEvidence(ctx, child.ID)
		if err != nil {
			return nil, fmt.Errorf("application: promote: list evidence for %q: %w", child.ID, err)
		}
		evidence = append(evidence, childEvidence...)
	}
	return evidence, nil
}

func loadEvidenceComments(ctx context.Context, comments classifyCommentRepository, evidence []domain.CandidateEvidence) ([]ports.CommentInput, error) {
	out := make([]ports.CommentInput, 0, len(evidence))
	for _, ev := range evidence {
		comment, ok, err := comments.GetByID(ctx, ev.CommentID)
		if err != nil {
			return nil, fmt.Errorf("application: promote: load comment %d: %w", ev.CommentID, err)
		}
		if !ok {
			continue
		}
		out = append(out, ports.CommentInput{CommentID: comment.ID, PullRequestNumber: comment.PullRequestNumber, Path: comment.Path, Language: comment.Language, Body: comment.Body, DiffHunk: comment.DiffHunk})
	}
	return out, nil
}

func buildCatalog(ctx context.Context, rules classifyRuleRepository, language string) ([]ports.CatalogEntry, error) {
	var entries []ports.CatalogEntry
	for _, status := range []domain.RuleStatus{domain.RuleActive, domain.RuleDisabled, domain.RuleExpired} {
		statusRules, err := rules.ListByStatus(ctx, status)
		if err != nil {
			return nil, fmt.Errorf("application: catalog: list %s rules: %w", status, err)
		}
		for _, rule := range statusRules {
			if rule.Language != language && rule.Language != commonLanguage {
				continue
			}
			body := rule.Rule
			if body == "" {
				body = rule.Body
			}
			entries = append(entries, ports.CatalogEntry{RuleID: rule.RuleID, Category: rule.Language, Title: rule.Title, Body: body, Scope: rule.Scope, Status: rule.Status})
		}
	}
	return entries, nil
}

func buildCandidateEntries(open []domain.CandidateCluster) []ports.CandidateEntry {
	out := make([]ports.CandidateEntry, 0, len(open))
	for _, c := range open {
		out = append(out, ports.CandidateEntry{CandidateID: c.ID, Label: c.Label, Category: c.Language})
	}
	return out
}

func buildCommentInputs(comments []domain.Comment) []ports.CommentInput {
	out := make([]ports.CommentInput, 0, len(comments))
	for _, c := range comments {
		out = append(out, ports.CommentInput{CommentID: c.ID, PullRequestNumber: c.PullRequestNumber, Path: c.Path, Language: c.Language, Body: c.Body, DiffHunk: c.DiffHunk})
	}
	return out
}

func chunkComments(comments []domain.Comment, size int) [][]domain.Comment {
	if len(comments) == 0 {
		return nil
	}
	if size <= 0 {
		return [][]domain.Comment{comments}
	}
	var out [][]domain.Comment
	for i := 0; i < len(comments); i += size {
		end := i + size
		if end > len(comments) {
			end = len(comments)
		}
		out = append(out, comments[i:end])
	}
	return out
}

func candidateID(language string, key string) string {
	sum := sha256.Sum256([]byte(language + "|" + key))
	return hex.EncodeToString(sum[:])[:16]
}
