package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type Summary struct {
	Command      string
	DryRun       bool
	Message      string
	Repositories []RepositoryPublicationResult
}

type SyncDeps struct {
	GitHub          ports.CodeReviewGateway
	Classifier      ports.Classifier
	Repositories    syncRepositoryRepository
	Cursors         syncCursorRepository
	Comments        syncCommentRepository
	Reactions       syncReactionRepository
	Rules           syncRuleRepository
	Candidates      syncCandidateRepository
	Classifications syncClassificationRepository
	Runs            syncRunRepository
	Snapshots       syncSnapshotRepository
	Publications    syncPublicationRepository
	Clock           ports.Clock
}

type syncRepositoryRepository interface {
	Upsert(context.Context, domain.Repository) (domain.Repository, error)
	GetByFullName(context.Context, string) (domain.Repository, bool, error)
	List(context.Context) ([]domain.Repository, error)
}
type syncCursorRepository interface {
	Get(context.Context, int64, string) (domain.Cursor, bool, error)
	Advance(context.Context, domain.Cursor) error
}
type syncCommentRepository interface {
	Upsert(context.Context, domain.Comment) (domain.Comment, error)
	GetByID(context.Context, int64) (domain.Comment, bool, error)
	ListPendingClassification(context.Context, string) ([]domain.Comment, error)
	ListTrackedAI(context.Context) ([]domain.Comment, error)
	ListAllOrdered(context.Context) ([]domain.Comment, error)
}
type syncReactionRepository interface {
	ReplaceForComment(context.Context, int64, []domain.Reaction) error
	CountByRule(context.Context, string, string) (int, error)
	CountByComment(context.Context, int64, string) (int, error)
}
type syncRuleRepository interface {
	Get(context.Context, string) (domain.Rule, bool, error)
	Insert(context.Context, domain.Rule) error
	RecordUsage(context.Context, domain.RuleUsage) error
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
	ListActiveByLanguages(context.Context, []string) ([]domain.Rule, error)
	UpdateStatus(context.Context, domain.RuleStatusEvent) error
	NextSequence(context.Context, string) (int, error)
}
type syncCandidateRepository interface {
	Upsert(context.Context, domain.CandidateCluster) (domain.CandidateCluster, error)
	Get(context.Context, string) (domain.CandidateCluster, bool, error)
	ListOpen(context.Context, string) ([]domain.CandidateCluster, error)
	ListMergedInto(context.Context, string) ([]domain.CandidateCluster, error)
	ListEvidence(context.Context, string) ([]domain.CandidateEvidence, error)
	AttachEvidence(context.Context, domain.CandidateEvidence) error
	Merge(context.Context, string, []string) error
	Promote(context.Context, string, string) error
}
type syncClassificationRepository interface {
	Save(context.Context, domain.Classification) error
}
type syncRunRepository interface {
	Start(context.Context, domain.Run) (domain.Run, error)
	Finish(context.Context, domain.Run) error
}
type syncSnapshotRepository interface {
	Save(context.Context, domain.Snapshot) error
	Latest(context.Context) (domain.Snapshot, bool, error)
}
type syncPublicationRepository interface {
	Save(context.Context, domain.Publication) error
	LastForRepository(context.Context, int64) (domain.Publication, bool, error)
}

type SyncOptions struct {
	DryRun  bool
	SeedDir string

	AnalyzedRepositories []string
	CursorOverlap        time.Duration
	InitialSince         time.Time
	AIAuthors            []string
	ExtensionToLanguage  map[string]string
	RulePrefix           string
	IncludeReplies       bool
	JSONLPath            string

	PromptVersion       string
	MaxCommentsPerCall  int
	LanguageCodes       map[string]string
	IDWidth             int
	MinRepeatedComments int
	MinThumbsUp         int
	MinThumbsDown       int
	ExpireAfter         time.Duration
	SnapshotTitle       string
}

func validateDeps(deps SyncDeps) error {
	switch {
	case deps.GitHub == nil:
		return errors.New("application: ports.CodeReviewGateway dependency is nil")
	case deps.Classifier == nil:
		return errors.New("application: ports.Classifier dependency is nil")
	case deps.Repositories == nil || deps.Cursors == nil || deps.Comments == nil || deps.Reactions == nil || deps.Rules == nil || deps.Candidates == nil || deps.Classifications == nil || deps.Runs == nil:
		return errors.New("application: repository dependencies are incomplete")
	case deps.Clock == nil:
		return errors.New("application: ports.Clock dependency is nil")
	}
	return nil
}

func Sync(ctx context.Context, deps SyncDeps, opts SyncOptions) (Summary, error) {
	if ctx == nil {
		return Summary{}, errors.New("application: ctx is nil")
	}
	if err := validateDeps(deps); err != nil {
		return Summary{}, err
	}

	if opts.DryRun {
		message := "dry-run: no side effects were applied"
		if opts.SeedDir != "" {
			message = fmt.Sprintf("dry-run: seeds from %q would be imported without applying changes", opts.SeedDir)
		}
		if len(opts.AnalyzedRepositories) > 0 {
			message = fmt.Sprintf("%s; %d analyzed repository(ies) would be mined without applying changes", message, len(opts.AnalyzedRepositories))
		}
		return Summary{
			Command: "sync",
			DryRun:  true,
			Message: message,
		}, nil
	}

	run, startErr := deps.Runs.Start(ctx, domain.Run{
		Command:   "sync",
		Status:    domain.RunRunning,
		StartedAt: deps.Clock.Now(),
	})
	if startErr != nil {
		return Summary{}, fmt.Errorf("application: sync: record run start: %w", startErr)
	}

	seedSummary, runErr := runSeedImport(ctx, deps, opts)
	summary := seedSummary
	partial := false

	if runErr == nil {
		mining, mineErr := runMining(ctx, deps, opts)
		summary.Message = summary.Message + "; " + mining.Message
		if mineErr != nil {
			runErr = mineErr
		} else {
			partial = mining.HadFailure

			if opts.JSONLPath != "" {
				if exportErr := ExportCommentsJSONL(ctx, deps.Repositories, deps.Comments, opts.JSONLPath); exportErr != nil {
					runErr = fmt.Errorf("application: sync: exportar JSONL: %w", exportErr)
					summary.Message = fmt.Sprintf("%s; JSONL export to %q failed: %v", summary.Message, opts.JSONLPath, exportErr)
				} else {
					summary.Message = fmt.Sprintf("%s; JSONL exported to %q", summary.Message, opts.JSONLPath)
				}
			}
		}
	}

	if runErr == nil {
		reactions, reactionsErr := ReconcileReactions(ctx, deps.GitHub, deps.Repositories, deps.Comments, deps.Reactions, deps.Clock)
		if reactionsErr != nil {
			runErr = fmt.Errorf("application: sync: reconciliar reacciones: %w", reactionsErr)
		} else {
			summary.Message = fmt.Sprintf("%s; reactions: %d comment(s) reviewed, %d replaced", summary.Message, reactions.CommentsProcessed, reactions.ReactionsReplaced)
		}
	}

	if runErr == nil {
		classifyPromote, classifyErr := ClassifyAndPromote(ctx, deps.Classifier, ClassifyAndPromoteDeps{
			Comments: deps.Comments, Classifications: deps.Classifications, Candidates: deps.Candidates, Reactions: deps.Reactions, Rules: deps.Rules,
		}, deps.Clock, ClassifyAndPromoteInput{
			PromptVersion:      opts.PromptVersion,
			MaxCommentsPerCall: opts.MaxCommentsPerCall,
			RulePrefix:         opts.RulePrefix,
			IDWidth:            opts.IDWidth,
			Languages:          languageCodeKeys(opts.LanguageCodes),
			LanguageCodes:      opts.LanguageCodes,
			Thresholds:         PromotionThresholds{MinRepeatedComments: opts.MinRepeatedComments, MinThumbsUp: opts.MinThumbsUp},
		})
		if classifyErr != nil {
			runErr = fmt.Errorf("application: sync: classify and promote: %w", classifyErr)
		} else {
			summary.Message = fmt.Sprintf("%s; classification: %d comment(s) classified, %d candidate(s) merged, %d rule(s) promoted",
				summary.Message, classifyPromote.CommentsClassified, classifyPromote.CandidatesMerged, len(classifyPromote.RulesPromoted))
		}
	}

	if runErr == nil {
		lifecycle, lifecycleErr := EnforceRuleLifecycle(ctx, deps.Rules, deps.Reactions, deps.Clock, RuleLifecycleInput{
			RunID:         run.ID,
			MinThumbsDown: opts.MinThumbsDown,
			ExpireAfter:   opts.ExpireAfter,
		})
		if lifecycleErr != nil {
			runErr = fmt.Errorf("application: sync: enforce rule lifecycle: %w", lifecycleErr)
		} else {
			summary.Message = fmt.Sprintf("%s; lifecycle: %d rule(s) disabled by negative feedback", summary.Message, len(lifecycle.RulesDisabled))
		}
	}

	finished := deps.Clock.Now()
	run.FinishedAt = &finished
	run.Summary = summary.Message
	switch {
	case runErr != nil:
		run.Status = domain.RunFailed
		run.Error = runErr.Error()
	case partial:
		run.Status = domain.RunPartial
	default:
		run.Status = domain.RunSucceeded
	}
	if finishErr := deps.Runs.Finish(ctx, run); finishErr != nil {
		if runErr != nil {
			return summary, fmt.Errorf("application: sync: %w (also failed to record run completion: %v)", runErr, finishErr)
		}
		return summary, fmt.Errorf("application: sync: record run finish: %w", finishErr)
	}

	return summary, runErr
}

type miningResult struct {
	Message    string
	HadFailure bool
}

func runMining(ctx context.Context, deps SyncDeps, opts SyncOptions) (miningResult, error) {
	if len(opts.AnalyzedRepositories) == 0 {
		return miningResult{Message: "repositories.analyzed is not configured: skipping mining"}, nil
	}

	pattern, err := BuildRuleIDPattern(opts.RulePrefix)
	if err != nil {
		return miningResult{}, fmt.Errorf("application: sync: build rule ID pattern (prefix %q): %w", opts.RulePrefix, err)
	}

	var (
		reposOK, reposFailed           int
		commentsSeen, commentsUpserted int
		errMessages                    []string
	)

	for _, fullName := range opts.AnalyzedRepositories {
		repo, err := resolveAnalyzedRepository(ctx, deps.Repositories, fullName)
		if err != nil {
			reposFailed++
			errMessages = append(errMessages, fmt.Sprintf("%s: %v", fullName, err))
			continue
		}

		mineSummary, mineErr := MineRepository(ctx, deps.GitHub, deps.Cursors, deps.Comments, deps.Rules, deps.Clock, MineRepositoryInput{
			Repository:          repo,
			CursorOverlap:       opts.CursorOverlap,
			InitialSince:        opts.InitialSince,
			AIAuthors:           opts.AIAuthors,
			ExtensionToLanguage: opts.ExtensionToLanguage,
			RulePattern:         pattern,
			IncludeReplies:      opts.IncludeReplies,
		})
		commentsSeen += mineSummary.CommentsSeen
		commentsUpserted += mineSummary.CommentsUpserted
		if mineErr != nil {
			reposFailed++
			errMessages = append(errMessages, fmt.Sprintf("%s: %v", fullName, mineErr))
			continue
		}
		reposOK++
	}

	message := fmt.Sprintf(
		"mining: %d repository(ies) succeeded, %d failed, %d comment(s) seen, %d inserted/updated",
		reposOK, reposFailed, commentsSeen, commentsUpserted,
	)
	if len(errMessages) > 0 {
		message = fmt.Sprintf("%s; errores: %s", message, strings.Join(errMessages, " | "))
	}

	return miningResult{Message: message, HadFailure: reposFailed > 0}, nil
}

func languageCodeKeys(languageCodes map[string]string) []string {
	out := make([]string, 0, len(languageCodes))
	for lang := range languageCodes {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

func resolveAnalyzedRepository(ctx context.Context, repositories syncRepositoryRepository, fullName string) (domain.Repository, error) {
	repo, ok, err := repositories.GetByFullName(ctx, fullName)
	if err != nil {
		return domain.Repository{}, fmt.Errorf("look up repository: %w", err)
	}
	if !ok {
		repo, err = repositories.Upsert(ctx, domain.Repository{FullName: fullName, Analyzed: true})
		if err != nil {
			return domain.Repository{}, fmt.Errorf("create repository: %w", err)
		}
		return repo, nil
	}
	if !repo.Analyzed {
		repo.Analyzed = true
		repo, err = repositories.Upsert(ctx, repo)
		if err != nil {
			return domain.Repository{}, fmt.Errorf("mark repository as analyzed: %w", err)
		}
	}
	return repo, nil
}

func runSeedImport(ctx context.Context, deps SyncDeps, opts SyncOptions) (Summary, error) {
	if opts.SeedDir == "" {
		return Summary{
			Command: "sync",
			DryRun:  false,
			Message: "rules.seed_dir is not configured: skipping seed import",
		}, nil
	}

	seedSummary, err := ImportSeeds(ctx, deps.Rules, deps.Clock, opts.SeedDir)
	if err != nil {
		return Summary{
			Command: "sync",
			DryRun:  false,
			Message: fmt.Sprintf("seed import failed: %v", err),
		}, fmt.Errorf("application: sync: import seeds: %w", err)
	}

	message := fmt.Sprintf(
		"seeds: %d file(s) scanned, %d rule(s) found, %d inserted, %d already existed (skipped)",
		seedSummary.FilesScanned, seedSummary.RulesFound, seedSummary.Inserted, seedSummary.SkippedExisting,
	)
	if len(seedSummary.ParseErrors) > 0 {
		message = fmt.Sprintf("%s; %d error(es) de parseo: %s", message, len(seedSummary.ParseErrors), strings.Join(seedSummary.ParseErrors, " | "))
	}

	return Summary{
		Command: "sync",
		DryRun:  false,
		Message: message,
	}, nil
}
