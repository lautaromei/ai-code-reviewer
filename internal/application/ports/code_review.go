// Package ports defines the interfaces owned by application use cases.
//
// Adapters implement these interfaces; neither the domain nor the use cases
// depend on a concrete provider such as GitHub.
package ports

import (
	"context"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type Cursor struct {
	Since         time.Time
	LastCommentID int64
}

// ReviewComment is the provider-neutral data required to ingest a review.
type ReviewComment struct {
	GitHubCommentID   int64
	PullRequestNumber int64
	ReviewID          int64
	InReplyToID       *int64
	Author            string
	AuthorIsBot       bool
	Body              string
	Path              string
	DiffHunk          string
	URL               string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ReactionRecord struct {
	GitHubReactionID int64
	User             string
	Content          string
	CreatedAt        time.Time
}

type ReactionPage struct {
	Reactions   []ReactionRecord
	ETag        string
	NotModified bool
}

type Language struct {
	Name  string
	Bytes int64
}

type PublicationRequest struct {
	Repository    string
	Branch        string
	DefaultBranch string
	FilePath      string
	Content       []byte
	ContentHash   string
	CommitMessage string
	PRTitle       string
	PRBodyMarker  string
	Draft         bool
}

type PublicationResult struct {
	Status         domain.PublicationStatus
	CommitSHA      string
	PullRequestURL string
}

// CodeReviewGateway is the outbound port for the code-hosting capabilities
// needed by the use cases. The GitHub CLI adapter is one implementation.
type CodeReviewGateway interface {
	ListReviewComments(ctx context.Context, repo string, since Cursor) ([]ReviewComment, Cursor, error)
	ListReactions(ctx context.Context, repo string, commentID int64, etag string) (ReactionPage, error)
	RepositoryLanguages(ctx context.Context, repo string) ([]Language, error)
	DefaultBranch(ctx context.Context, repo string) (string, error)
	ReadFile(ctx context.Context, repo string, path string, branch string) ([]byte, error)
	UpsertRulesPullRequest(ctx context.Context, request PublicationRequest) (PublicationResult, error)
}
