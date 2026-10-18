package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

const commentsStream = "comments"

type mineCursorRepository interface {
	Get(ctx context.Context, repositoryID int64, stream string) (domain.Cursor, bool, error)
	Advance(ctx context.Context, cursor domain.Cursor) error
}

type mineCommentRepository interface {
	Upsert(ctx context.Context, comment domain.Comment) (domain.Comment, error)
}

type mineRuleRepository interface {
	RecordUsage(ctx context.Context, usage domain.RuleUsage) error
}

type MineRepositoryInput struct {
	Repository domain.Repository

	CursorOverlap time.Duration
	InitialSince  time.Time

	AIAuthors           []string
	ExtensionToLanguage map[string]string
	RulePattern         *regexp.Regexp
	IncludeReplies      bool
}

type MineSummary struct {
	RepositoryFullName string
	CommentsSeen       int
	CommentsUpserted   int
}

func MineRepository(ctx context.Context, gh ports.CodeReviewGateway, cursors mineCursorRepository, comments mineCommentRepository, rules mineRuleRepository, clock ports.Clock, input MineRepositoryInput) (MineSummary, error) {
	summary := MineSummary{RepositoryFullName: input.Repository.FullName}

	if ctx == nil {
		return summary, fmt.Errorf("application: mine: ctx is nil")
	}
	if gh == nil {
		return summary, fmt.Errorf("application: mine: ports.CodeReviewGateway dependency is nil")
	}
	if clock == nil {
		return summary, fmt.Errorf("application: mine: ports.Clock dependency is nil")
	}
	if cursors == nil || comments == nil || rules == nil {
		return summary, fmt.Errorf("application: mine: repository dependencies are incomplete")
	}

	since := input.InitialSince
	existingCursor, hasCursor, err := cursors.Get(ctx, input.Repository.ID, commentsStream)
	if err != nil {
		return summary, fmt.Errorf("application: mine: read cursor for %s: %w", input.Repository.FullName, err)
	}
	if hasCursor {
		since = existingCursor.HighWatermark.Add(-input.CursorOverlap)
		if since.Before(input.InitialSince) {
			since = input.InitialSince
		}
	}

	reviewComments, _, err := gh.ListReviewComments(ctx, input.Repository.FullName, ports.Cursor{Since: since})
	if err != nil {
		return summary, fmt.Errorf("application: mine: list comments from %s: %w", input.Repository.FullName, err)
	}

	highWatermark := since
	if hasCursor && existingCursor.HighWatermark.After(highWatermark) {
		highWatermark = existingCursor.HighWatermark
	}

	for _, rc := range reviewComments {
		if rc.InReplyToID != nil && !input.IncludeReplies {
			continue
		}
		summary.CommentsSeen++

		authorKind, origin, ruleIDs := ClassifyOrigin(OriginInput{
			AuthorLogin: rc.Author,
			AuthorIsBot: rc.AuthorIsBot,
			Body:        rc.Body,
		}, input.AIAuthors, input.RulePattern)

		if origin != domain.OriginAIRuleUsage {
			ruleIDs = nil
		}

		language := DetectLanguage(rc.Path, input.ExtensionToLanguage)

		comment := domain.Comment{
			GitHubCommentID:   rc.GitHubCommentID,
			RepositoryID:      input.Repository.ID,
			PullRequestNumber: rc.PullRequestNumber,
			ReviewID:          rc.ReviewID,
			ParentCommentID:   rc.InReplyToID,
			Author:            rc.Author,
			AuthorKind:        authorKind,
			Origin:            origin,
			Body:              rc.Body,
			Path:              rc.Path,
			DiffHunk:          rc.DiffHunk,
			Language:          language,
			URL:               rc.URL,
			RuleIDs:           ruleIDs,
			CreatedAt:         rc.CreatedAt,
			UpdatedAt:         rc.UpdatedAt,
			IngestedAt:        clock.Now(),
			InputHash:         inputHash(rc.Body, rc.DiffHunk, language),
		}

		storedComment, err := comments.Upsert(ctx, comment)
		if err != nil {
			return summary, fmt.Errorf("application: mine: save comment %d from %s: %w", rc.GitHubCommentID, input.Repository.FullName, err)
		}
		summary.CommentsUpserted++

		for _, ruleID := range storedComment.RuleIDs {
			if err := rules.RecordUsage(ctx, domain.RuleUsage{
				RuleID:    ruleID,
				CommentID: storedComment.ID,
				UsedAt:    storedComment.CreatedAt,
			}); err != nil {
				return summary, fmt.Errorf("application: mine: record rule %s usage in comment %d from %s: %w", ruleID, rc.GitHubCommentID, input.Repository.FullName, err)
			}
		}

		if rc.UpdatedAt.After(highWatermark) {
			highWatermark = rc.UpdatedAt
		}
	}

	cursor := domain.Cursor{
		RepositoryID:  input.Repository.ID,
		Stream:        commentsStream,
		HighWatermark: highWatermark,
		LastSuccessAt: clock.Now(),
	}
	if hasCursor {
		cursor.LastCommentID = existingCursor.LastCommentID
		cursor.ETag = existingCursor.ETag
	}
	if err := cursors.Advance(ctx, cursor); err != nil {
		return summary, fmt.Errorf("application: mine: avanzar cursor de %s: %w", input.Repository.FullName, err)
	}

	return summary, nil
}

func inputHash(body, diffHunk, language string) string {
	sum := sha256.Sum256([]byte(body + "|" + diffHunk + "|" + language))
	return hex.EncodeToString(sum[:])
}
