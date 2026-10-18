package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type cursorRepo struct{ s *Database }

func (c *cursorRepo) Get(ctx context.Context, repositoryID int64, stream string) (domain.Cursor, bool, error) {
	const q = `
SELECT repository_id, stream, high_watermark, last_comment_id, etag, last_success_at
FROM sync_cursors WHERE repository_id = ? AND stream = ?;`

	row := c.s.conn().QueryRowContext(ctx, q, repositoryID, stream)
	var (
		cur           domain.Cursor
		highWatermark string
		lastSuccess   string
	)
	err := row.Scan(&cur.RepositoryID, &cur.Stream, &highWatermark, &cur.LastCommentID, &cur.ETag, &lastSuccess)
	if err == sql.ErrNoRows {
		return domain.Cursor{}, false, nil
	}
	if err != nil {
		return domain.Cursor{}, false, fmt.Errorf("sqlite: get cursor (%d, %s): %w", repositoryID, stream, err)
	}
	cur.HighWatermark = parseTime(highWatermark)
	cur.LastSuccessAt = parseTime(lastSuccess)
	return cur, true, nil
}

func (c *cursorRepo) Advance(ctx context.Context, cursor domain.Cursor) error {
	const q = `
INSERT INTO sync_cursors (repository_id, stream, high_watermark, last_comment_id, etag, last_success_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(repository_id, stream) DO UPDATE SET
    high_watermark = excluded.high_watermark,
    last_comment_id = excluded.last_comment_id,
    etag = excluded.etag,
    last_success_at = excluded.last_success_at;`

	_, err := c.s.conn().ExecContext(ctx, q,
		cursor.RepositoryID, cursor.Stream,
		formatTime(cursor.HighWatermark), cursor.LastCommentID, cursor.ETag,
		formatTime(cursor.LastSuccessAt),
	)
	if err != nil {
		return fmt.Errorf("sqlite: advance cursor (%d, %s): %w", cursor.RepositoryID, cursor.Stream, err)
	}
	return nil
}
