package application

import (
	"context"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type ReconcileReactionsSummary struct {
	CommentsProcessed int
	ReactionsReplaced int
}

// ReactionSourceRepository is the repository dependency consumed by ReconcileReactions.
type ReactionSourceRepository interface {
	List(context.Context) ([]domain.Repository, error)
}

// TrackedCommentsRepository is the comment dependency consumed by ReconcileReactions.
type TrackedCommentsRepository interface {
	ListTrackedAI(context.Context) ([]domain.Comment, error)
}

// ReactionSinkRepository is the persistence dependency consumed by ReconcileReactions.
type ReactionSinkRepository interface {
	ReplaceForComment(context.Context, int64, []domain.Reaction) error
}

func ReconcileReactions(ctx context.Context, gh ports.CodeReviewGateway, repositories ReactionSourceRepository, commentsRepository TrackedCommentsRepository, reactionsRepository ReactionSinkRepository, clock ports.Clock) (ReconcileReactionsSummary, error) {
	comments, err := commentsRepository.ListTrackedAI(ctx)
	if err != nil {
		return ReconcileReactionsSummary{}, err
	}

	repositoryList, err := repositories.List(ctx)
	if err != nil {
		return ReconcileReactionsSummary{}, err
	}
	repoFullNameByID := make(map[int64]string, len(repositoryList))
	for _, repo := range repositoryList {
		repoFullNameByID[repo.ID] = repo.FullName
	}

	summary := ReconcileReactionsSummary{}
	now := clock.Now()

	for _, comment := range comments {
		summary.CommentsProcessed++

		page, err := gh.ListReactions(ctx, repoFullNameByID[comment.RepositoryID], comment.GitHubCommentID, "")
		if err != nil {
			return ReconcileReactionsSummary{}, err
		}
		if page.NotModified {
			continue
		}

		reactions := make([]domain.Reaction, 0, len(page.Reactions))
		for _, r := range page.Reactions {
			if r.Content != "-1" {
				continue
			}
			reactions = append(reactions, domain.Reaction{
				GitHubReactionID: r.GitHubReactionID,
				CommentID:        comment.ID,
				User:             r.User,
				Content:          r.Content,
				CreatedAt:        r.CreatedAt,
				LastSeenAt:       now,
			})
		}

		if err := reactionsRepository.ReplaceForComment(ctx, comment.ID, reactions); err != nil {
			return ReconcileReactionsSummary{}, err
		}
		summary.ReactionsReplaced += len(reactions)
	}

	return summary, nil
}
