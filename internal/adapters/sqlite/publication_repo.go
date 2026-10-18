package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type publicationRepo struct{ s *Database }

func (p *publicationRepo) Save(ctx context.Context, publication domain.Publication) error {
	const q = `
INSERT INTO publications (snapshot_id, repository_id, filtered_snapshot_hash, branch, commit_sha, pull_request_url, status, last_error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(repository_id, filtered_snapshot_hash) DO UPDATE SET
    snapshot_id = excluded.snapshot_id,
    branch = excluded.branch,
    commit_sha = excluded.commit_sha,
    pull_request_url = excluded.pull_request_url,
    status = excluded.status,
    last_error = excluded.last_error
RETURNING id;`

	var id int64
	err := p.s.conn().QueryRowContext(ctx, q,
		publication.SnapshotID, publication.RepositoryID, publication.FilteredSnapshotHash,
		publication.Branch, publication.CommitSHA, publication.PullRequestURL,
		string(publication.Status), publication.LastError, formatTime(publication.CreatedAt),
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("sqlite: save publication (repo=%d): %w", publication.RepositoryID, err)
	}
	return nil
}

func (p *publicationRepo) LastForRepository(ctx context.Context, repositoryID int64) (domain.Publication, bool, error) {
	const q = `
SELECT id, snapshot_id, repository_id, filtered_snapshot_hash, branch, commit_sha, pull_request_url, status, last_error, created_at
FROM publications
WHERE repository_id = ?
ORDER BY id DESC LIMIT 1;`

	var (
		pub       domain.Publication
		status    string
		createdAt string
	)
	err := p.s.conn().QueryRowContext(ctx, q, repositoryID).Scan(
		&pub.ID, &pub.SnapshotID, &pub.RepositoryID, &pub.FilteredSnapshotHash,
		&pub.Branch, &pub.CommitSHA, &pub.PullRequestURL, &status, &pub.LastError, &createdAt,
	)
	if err == sql.ErrNoRows {
		return domain.Publication{}, false, nil
	}
	if err != nil {
		return domain.Publication{}, false, fmt.Errorf("sqlite: last publication for repo %d: %w", repositoryID, err)
	}
	pub.Status = domain.PublicationStatus(status)
	pub.CreatedAt = parseTime(createdAt)
	return pub, true, nil
}
