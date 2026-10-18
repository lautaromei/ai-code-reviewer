//go:build !testigo_bootstrap

package sqlitetest

import (
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

// StoredReviewComment starts from Testigo's reproducible random base and fixes
// only the fields relevant to the SQLite repository contracts.
func StoredReviewComment(repositoryID int64, origin domain.CommentOrigin, at time.Time) domain.Comment {
	return Comment.
		WithRepositoryID(repositoryID).
		WithOrigin(origin).
		WithCreatedAt(at).
		WithUpdatedAt(at).
		WithIngestedAt(at).
		WithInputHash("input-hash").
		Bare()
}

// ClassificationFor returns the persisted classification contract for a comment.
func ClassificationFor(comment domain.Comment, verdict domain.ClassificationVerdict, at time.Time) domain.Classification {
	return Classification.
		WithCommentID(comment.ID).
		WithInputHash(comment.InputHash).
		WithPromptVersion("v1").
		WithModel("model").
		WithVerdict(verdict).
		WithRawPayload("{}").
		WithCreatedAt(at).
		Bare()
}
