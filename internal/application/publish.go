package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/markdown"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

const publishCommonLanguage = "common"

type PublishDeps struct {
	GitHub       ports.CodeReviewGateway
	Classifier   ports.Classifier
	Repository   publishRepositoryRepository
	Rules        publishRuleRepository
	Publications publishPublicationRepository
	Snapshots    publishSnapshotRepository
	Clock        ports.Clock
}

type publishRepositoryRepository interface {
	Upsert(context.Context, domain.Repository) (domain.Repository, error)
	GetByFullName(context.Context, string) (domain.Repository, bool, error)
}
type publishRuleRepository interface {
	ListActiveByLanguages(context.Context, []string) ([]domain.Rule, error)
}
type publishPublicationRepository interface {
	Save(context.Context, domain.Publication) error
	LastForRepository(context.Context, int64) (domain.Publication, bool, error)
}
type publishSnapshotRepository interface {
	Latest(context.Context) (domain.Snapshot, bool, error)
}

type PublishActionableRepository struct {
	FullName  string
	RulesPath string
	Languages []string
}

type RepositoryPublicationResult struct {
	Repository string
	Status     domain.PublicationStatus
	Error      string
}

type PublishOptions struct {
	DryRun        bool
	Branch        string
	Title         string
	CommitMessage string
	Draft         bool
	Repositories  []PublishActionableRepository
}

const (
	defaultPublishBranch        = "automation/ai-review-rules"
	defaultPublishTitle         = "chore: update AI review rules"
	defaultPublishCommitMessage = "chore: update AI review rules"
)

// NewPublishOptions creates publish options with their domain defaults applied.
func NewPublishOptions(opts PublishOptions) PublishOptions {
	if opts.Branch == "" {
		opts.Branch = defaultPublishBranch
	}
	if opts.Title == "" {
		opts.Title = defaultPublishTitle
	}
	if opts.CommitMessage == "" {
		opts.CommitMessage = defaultPublishCommitMessage
	}
	return opts
}

func Publish(ctx context.Context, deps PublishDeps, opts PublishOptions) (Summary, error) {
	if ctx == nil {
		return Summary{}, errors.New("application: ctx is nil")
	}
	if deps.GitHub == nil || deps.Classifier == nil || deps.Repository == nil || deps.Rules == nil || deps.Publications == nil || deps.Snapshots == nil || deps.Clock == nil {
		return Summary{}, errors.New("application: publish: repository or service dependency is nil")
	}
	results := make([]RepositoryPublicationResult, 0, len(opts.Repositories))
	if opts.DryRun {
		return Summary{
			Command:      "publish",
			DryRun:       true,
			Message:      fmt.Sprintf("dry-run: no rules would be published to %d repository(ies)", len(opts.Repositories)),
			Repositories: results,
		}, nil
	}

	var failures int

	for _, cfg := range opts.Repositories {
		result, err := publishRepository(ctx, deps, opts, cfg)
		if err != nil {
			failures++
			results = append(results, RepositoryPublicationResult{Repository: cfg.FullName, Status: domain.PublicationFailed, Error: err.Error()})
			continue
		}
		results = append(results, result)
	}

	summary := Summary{
		Command:      "publish",
		DryRun:       opts.DryRun,
		Message:      fmt.Sprintf("publication: %d repository(ies) processed, %d failed", len(results), failures),
		Repositories: results,
	}
	if failures > 0 {
		return summary, fmt.Errorf("application: publish: publication failed for %d repository(ies)", failures)
	}
	return summary, nil
}

func publishRepository(ctx context.Context, deps PublishDeps, opts PublishOptions, cfg PublishActionableRepository) (RepositoryPublicationResult, error) {
	repo, ok, err := deps.Repository.GetByFullName(ctx, cfg.FullName)
	if err != nil {
		return RepositoryPublicationResult{}, err
	}
	if !ok {
		repo, err = deps.Repository.Upsert(ctx, domain.Repository{
			FullName:   cfg.FullName,
			Actionable: true,
			RulesPath:  cfg.RulesPath,
			Languages:  append([]string(nil), cfg.Languages...),
		})
		if err != nil {
			return RepositoryPublicationResult{}, fmt.Errorf("create actionable repository: %w", err)
		}
	}

	defaultBranch, err := deps.GitHub.DefaultBranch(ctx, cfg.FullName)
	if err != nil {
		return RepositoryPublicationResult{}, fmt.Errorf("resolve default branch: %w", err)
	}
	if defaultBranch == "" {
		return RepositoryPublicationResult{}, fmt.Errorf("GitHub returned an empty default branch")
	}
	remoteContent, err := deps.GitHub.ReadFile(ctx, cfg.FullName, cfg.RulesPath, defaultBranch)
	if err != nil {
		return RepositoryPublicationResult{}, fmt.Errorf("read remote rules file: %w", err)
	}
	repo.DefaultBranch = defaultBranch
	repo.Actionable = true
	repo.RulesPath = cfg.RulesPath

	languages := normalizeLanguages(cfg.Languages)
	if len(languages) == 0 {
		detected, detectErr := deps.GitHub.RepositoryLanguages(ctx, cfg.FullName)
		if detectErr != nil {
			return RepositoryPublicationResult{}, fmt.Errorf("detect languages: %w", detectErr)
		}
		for _, language := range detected {
			if normalized := normalizeLanguage(language.Name); normalized != "" {
				languages = append(languages, normalized)
			}
		}
	}
	languages = normalizeLanguages(languages)
	repo.Languages = append([]string(nil), languages...)
	repo.LanguagesAt = deps.Clock.Now()
	if _, err := deps.Repository.Upsert(ctx, repo); err != nil {
		return RepositoryPublicationResult{}, fmt.Errorf("update repository metadata: %w", err)
	}

	languages = append([]string{publishCommonLanguage}, languages...)
	rules, err := deps.Rules.ListActiveByLanguages(ctx, languages)
	if err != nil {
		return RepositoryPublicationResult{}, err
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].RuleID < rules[j].RuleID })

	rendered := markdown.Merge(remoteContent, opts.Title, rules)
	filteredSnapshot := markdown.Render(opts.Title, rules)
	sum := sha256.Sum256(filteredSnapshot)
	hash := hex.EncodeToString(sum[:])

	last, hasLast, err := deps.Publications.LastForRepository(ctx, repo.ID)
	if err != nil {
		return RepositoryPublicationResult{}, err
	}
	if hasLast && last.FilteredSnapshotHash == hash {
		return RepositoryPublicationResult{Repository: cfg.FullName, Status: domain.PublicationAlreadyCurrent}, nil
	}

	snapshotID := int64(0)
	if latest, hasSnapshot, snapshotErr := deps.Snapshots.Latest(ctx); snapshotErr != nil {
		return RepositoryPublicationResult{}, fmt.Errorf("load latest snapshot: %w", snapshotErr)
	} else if hasSnapshot {
		snapshotID = latest.ID
	}

	request := ports.PublicationRequest{
		Repository:    cfg.FullName,
		Branch:        opts.Branch,
		DefaultBranch: defaultBranch,
		FilePath:      cfg.RulesPath,
		Content:       rendered,
		ContentHash:   hash,
		CommitMessage: opts.CommitMessage,
		PRTitle:       opts.Title,
		PRBodyMarker:  publicationMarker(cfg.FullName),
		Draft:         opts.Draft,
	}

	prResult, err := deps.GitHub.UpsertRulesPullRequest(ctx, request)
	if err != nil {
		return RepositoryPublicationResult{}, err
	}

	publication := domain.Publication{
		SnapshotID:           snapshotID,
		RepositoryID:         repo.ID,
		FilteredSnapshotHash: hash,
		Branch:               opts.Branch,
		CommitSHA:            prResult.CommitSHA,
		PullRequestURL:       prResult.PullRequestURL,
		Status:               prResult.Status,
		CreatedAt:            deps.Clock.Now(),
	}
	if err := deps.Publications.Save(ctx, publication); err != nil {
		return RepositoryPublicationResult{}, err
	}

	return RepositoryPublicationResult{Repository: cfg.FullName, Status: prResult.Status}, nil
}

func publicationMarker(repository string) string {
	return fmt.Sprintf("<!-- ai-rules:managed repository=%s -->", repository)
}

func normalizeLanguages(languages []string) []string {
	seen := make(map[string]struct{}, len(languages))
	out := make([]string, 0, len(languages))
	for _, language := range languages {
		if normalized := normalizeLanguage(language); normalized != "" {
			if _, exists := seen[normalized]; exists {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}
