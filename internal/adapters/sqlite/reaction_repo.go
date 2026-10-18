package sqlite

import (
	"context"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type reactionRepo struct{ s *Database }

func (r *reactionRepo) ReplaceForComment(ctx context.Context, commentID int64, reactions []domain.Reaction) error {
	conn := r.s.conn()

	if _, err := conn.ExecContext(ctx, `DELETE FROM reactions WHERE comment_id = ?;`, commentID); err != nil {
		return fmt.Errorf("sqlite: borrando reacciones de comment %d: %w", commentID, err)
	}

	const q = `
INSERT INTO reactions (github_reaction_id, comment_id, user, content, created_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(github_reaction_id) DO UPDATE SET
    comment_id = excluded.comment_id,
    user = excluded.user,
    content = excluded.content,
    created_at = excluded.created_at,
    last_seen_at = excluded.last_seen_at;`

	for _, reaction := range reactions {
		reaction.CommentID = commentID
		if _, err := conn.ExecContext(ctx, q,
			reaction.GitHubReactionID, reaction.CommentID, reaction.User, reaction.Content,
			formatTime(reaction.CreatedAt), formatTime(reaction.LastSeenAt),
		); err != nil {
			return fmt.Errorf("sqlite: insert reaction %d for comment %d: %w", reaction.GitHubReactionID, commentID, err)
		}
	}
	return nil
}

func (r *reactionRepo) CountByRule(ctx context.Context, ruleID string, content string) (int, error) {
	const q = `
SELECT COUNT(*)
FROM reactions
JOIN rule_usages ON rule_usages.comment_id = reactions.comment_id
WHERE rule_usages.rule_id = ? AND reactions.content = ?;`

	var count int
	if err := r.s.conn().QueryRowContext(ctx, q, ruleID, content).Scan(&count); err != nil {
		return 0, fmt.Errorf("sqlite: count reactions for rule %q content %q: %w", ruleID, content, err)
	}
	return count, nil
}

func (r *reactionRepo) CountByComment(ctx context.Context, commentID int64, content string) (int, error) {
	const q = `SELECT COUNT(*) FROM reactions WHERE comment_id = ? AND content = ?;`

	var count int
	if err := r.s.conn().QueryRowContext(ctx, q, commentID, content).Scan(&count); err != nil {
		return 0, fmt.Errorf("sqlite: count reactions for comment %d content %q: %w", commentID, content, err)
	}
	return count, nil
}
