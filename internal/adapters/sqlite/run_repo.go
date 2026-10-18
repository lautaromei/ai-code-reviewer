package sqlite

import (
	"context"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type runRepo struct{ s *Database }

func (r *runRepo) Start(ctx context.Context, run domain.Run) (domain.Run, error) {
	const q = `
INSERT INTO runs (command, config_hash, status, started_at, finished_at, summary, error)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;`

	err := r.s.conn().QueryRowContext(ctx, q,
		run.Command, run.ConfigHash, string(run.Status), formatTime(run.StartedAt),
		formatTimePtr(run.FinishedAt), run.Summary, run.Error,
	).Scan(&run.ID)
	if err != nil {
		return domain.Run{}, fmt.Errorf("sqlite: start run %q: %w", run.Command, err)
	}
	return run, nil
}

func (r *runRepo) Finish(ctx context.Context, run domain.Run) error {
	const q = `
UPDATE runs SET status = ?, finished_at = ?, summary = ?, error = ?
WHERE id = ?;`

	_, err := r.s.conn().ExecContext(ctx, q,
		string(run.Status), formatTimePtr(run.FinishedAt), run.Summary, run.Error, run.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: finish run %d: %w", run.ID, err)
	}
	return nil
}
