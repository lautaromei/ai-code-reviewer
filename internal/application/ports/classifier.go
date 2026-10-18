package ports

import (
	"context"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type CommentInput struct {
	CommentID         int64  `json:"comment_id"`
	Repository        string `json:"repository"`
	PullRequestNumber int64  `json:"pull_request_number"`
	Path              string `json:"path"`
	Language          string `json:"suggested_category"`
	Body              string `json:"body"`
	DiffHunk          string `json:"diff_hunk"`
	ContextTruncated  bool   `json:"context_truncated"`
}

type CatalogEntry struct {
	RuleID                 string            `json:"rule_id"`
	Category               string            `json:"category"`
	Title                  string            `json:"title"`
	Body                   string            `json:"body"`
	Scope                  string            `json:"scope"`
	RepositoryRestrictions []string          `json:"repository_restrictions"`
	Status                 domain.RuleStatus `json:"status"`
}

type CandidateEntry struct {
	CandidateID   string         `json:"candidate_id"`
	Label         string         `json:"description"`
	Category      string         `json:"category"`
	Scope         string         `json:"scope"`
	EvidenceCount int            `json:"evidence_count"`
	Evidence      []CommentInput `json:"evidence"`
}

type ClassificationInput struct {
	PromptVersion  string
	Comments       []CommentInput
	Catalog        []CatalogEntry
	OpenCandidates []CandidateEntry
}

type CommentVerdict struct {
	CommentID    int64                        `json:"comment_id"`
	Verdict      domain.ClassificationVerdict `json:"verdict"`
	RuleID       string                       `json:"rule_id"`
	CandidateKey string                       `json:"candidate_key"`
	CandidateID  string                       `json:"candidate_id"`
	Reason       string                       `json:"reason"`
	RawPayload   string                       `json:"-"`
}

type ClassificationResult struct {
	Model    string
	Verdicts []CommentVerdict
}

type CompactionInput struct {
	Language   string
	Candidates []CandidateEntry
}

type CompactionMerge struct {
	SurvivingCandidateID string   `json:"surviving_candidate_id"`
	MergedCandidateIDs   []string `json:"merged_candidate_ids"`
}

type CompactionResult struct {
	Merges []CompactionMerge `json:"merges"`
}

type DraftRuleInput struct {
	CandidateID string
	Language    string
	Evidence    []CommentInput
}

type RuleDraft struct {
	Title     string `json:"title"`
	Scope     string `json:"scope"`
	Rule      string `json:"rule"`
	Exception string `json:"exception"`
	Bad       string `json:"bad"`
	Good      string `json:"good"`
	Body      string `json:"body,omitempty"` // legacy classifier compatibility
}

// Classifier is the outbound port for classifying and drafting rules.
type Classifier interface {
	Classify(ctx context.Context, input ClassificationInput) (ClassificationResult, error)
	Compact(ctx context.Context, input CompactionInput) (CompactionResult, error)
	DraftRule(ctx context.Context, input DraftRuleInput) (RuleDraft, error)
}
