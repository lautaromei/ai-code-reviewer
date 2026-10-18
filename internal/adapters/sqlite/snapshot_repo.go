package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type snapshotRepo struct{ s *Database }

func (s *snapshotRepo) Save(ctx context.Context, snapshot domain.Snapshot) error {
	conn := s.s.conn()

	const insertSnapshot = `
INSERT INTO snapshots (hash, run_id, renderer_version, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(hash) DO UPDATE SET
    run_id = excluded.run_id,
    renderer_version = excluded.renderer_version,
    created_at = excluded.created_at
RETURNING id;`

	var id int64
	if err := conn.QueryRowContext(ctx, insertSnapshot,
		snapshot.Hash, snapshot.RunID, snapshot.RendererVersion, formatTime(snapshot.CreatedAt),
	).Scan(&id); err != nil {
		return fmt.Errorf("sqlite: save snapshot %q: %w", snapshot.Hash, err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM snapshot_rules WHERE snapshot_id = ?;`, id); err != nil {
		return fmt.Errorf("sqlite: clearing snapshot_rules for snapshot %d: %w", id, err)
	}

	const insertRule = `INSERT INTO snapshot_rules (snapshot_id, rule_id, position) VALUES (?, ?, ?);`
	for i, ruleID := range snapshot.RuleIDs {
		if _, err := conn.ExecContext(ctx, insertRule, id, ruleID, i); err != nil {
			return fmt.Errorf("sqlite: linking rule %q to snapshot %d: %w", ruleID, id, err)
		}
	}
	return nil
}

func (s *snapshotRepo) Latest(ctx context.Context) (domain.Snapshot, bool, error) {
	conn := s.s.conn()

	const q = `
SELECT id, hash, run_id, renderer_version, created_at
FROM snapshots ORDER BY id DESC LIMIT 1;`

	var (
		snapshot  domain.Snapshot
		createdAt string
	)
	err := conn.QueryRowContext(ctx, q).Scan(&snapshot.ID, &snapshot.Hash, &snapshot.RunID, &snapshot.RendererVersion, &createdAt)
	if err == sql.ErrNoRows {
		return domain.Snapshot{}, false, nil
	}
	if err != nil {
		return domain.Snapshot{}, false, fmt.Errorf("sqlite: latest snapshot: %w", err)
	}
	snapshot.CreatedAt = parseTime(createdAt)

	rows, err := conn.QueryContext(ctx, `SELECT rule_id FROM snapshot_rules WHERE snapshot_id = ? ORDER BY position;`, snapshot.ID)
	if err != nil {
		return domain.Snapshot{}, false, fmt.Errorf("sqlite: loading rules for snapshot %d: %w", snapshot.ID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var ruleID string
		if err := rows.Scan(&ruleID); err != nil {
			return domain.Snapshot{}, false, fmt.Errorf("sqlite: scanning snapshot rule: %w", err)
		}
		snapshot.RuleIDs = append(snapshot.RuleIDs, ruleID)
	}
	if err := rows.Err(); err != nil {
		return domain.Snapshot{}, false, err
	}
	return snapshot, true, nil
}
