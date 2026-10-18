package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type classificationRepo struct{ s *Database }

func (c *classificationRepo) Get(ctx context.Context, commentID int64, inputHash string, promptVersion string) (domain.Classification, bool, error) {
	const q = `
SELECT comment_id, input_hash, prompt_version, model, verdict, raw_payload, created_at
FROM classifications
WHERE comment_id = ? AND input_hash = ? AND prompt_version = ?;`

	row := c.s.conn().QueryRowContext(ctx, q, commentID, inputHash, promptVersion)
	var (
		classification domain.Classification
		verdict        string
		createdAt      string
	)
	err := row.Scan(&classification.CommentID, &classification.InputHash, &classification.PromptVersion,
		&classification.Model, &verdict, &classification.RawPayload, &createdAt)
	if err == sql.ErrNoRows {
		return domain.Classification{}, false, nil
	}
	if err != nil {
		return domain.Classification{}, false, fmt.Errorf("sqlite: get classification (comment=%d): %w", commentID, err)
	}
	classification.Verdict = domain.ClassificationVerdict(verdict)
	classification.CreatedAt = parseTime(createdAt)
	return classification, true, nil
}

func (c *classificationRepo) Save(ctx context.Context, classification domain.Classification) error {
	const q = `
INSERT INTO classifications (comment_id, input_hash, prompt_version, model, verdict, raw_payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(comment_id, input_hash, prompt_version) DO UPDATE SET
    model = excluded.model,
    verdict = excluded.verdict,
    raw_payload = excluded.raw_payload,
    created_at = excluded.created_at;`

	_, err := c.s.conn().ExecContext(ctx, q,
		classification.CommentID, classification.InputHash, classification.PromptVersion,
		classification.Model, string(classification.Verdict), classification.RawPayload,
		formatTime(classification.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("sqlite: save classification (comment=%d): %w", classification.CommentID, err)
	}
	return nil
}
