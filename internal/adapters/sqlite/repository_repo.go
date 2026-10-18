package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type repositoryRepo struct{ s *Database }

func (r *repositoryRepo) Upsert(ctx context.Context, repo domain.Repository) (domain.Repository, error) {
	const q = `
INSERT INTO repositories (full_name, analyzed, actionable, rules_path, languages, languages_at, default_branch)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(full_name) DO UPDATE SET
    analyzed = excluded.analyzed,
    actionable = excluded.actionable,
    rules_path = excluded.rules_path,
    languages = excluded.languages,
    languages_at = excluded.languages_at,
    default_branch = excluded.default_branch
RETURNING id;`

	row := r.s.conn().QueryRowContext(ctx, q,
		repo.FullName,
		boolToInt(repo.Analyzed),
		boolToInt(repo.Actionable),
		repo.RulesPath,
		marshalStrings(repo.Languages),
		formatTime(repo.LanguagesAt),
		repo.DefaultBranch,
	)
	if err := row.Scan(&repo.ID); err != nil {
		return domain.Repository{}, fmt.Errorf("sqlite: upsert repository %q: %w", repo.FullName, err)
	}
	return repo, nil
}

func (r *repositoryRepo) GetByFullName(ctx context.Context, fullName string) (domain.Repository, bool, error) {
	const q = `
SELECT id, full_name, analyzed, actionable, rules_path, languages, languages_at, default_branch
FROM repositories WHERE full_name = ?;`

	row := r.s.conn().QueryRowContext(ctx, q, fullName)
	repo, err := scanRepository(row)
	if err == sql.ErrNoRows {
		return domain.Repository{}, false, nil
	}
	if err != nil {
		return domain.Repository{}, false, fmt.Errorf("sqlite: get repository %q: %w", fullName, err)
	}
	return repo, true, nil
}

func (r *repositoryRepo) List(ctx context.Context) ([]domain.Repository, error) {
	const q = `
SELECT id, full_name, analyzed, actionable, rules_path, languages, languages_at, default_branch
FROM repositories ORDER BY id;`

	rows, err := r.s.conn().QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list repositories: %w", err)
	}
	defer rows.Close()

	var out []domain.Repository
	for rows.Next() {
		repo, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan repository: %w", err)
		}
		out = append(out, repo)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row rowScanner) (domain.Repository, error) {
	var (
		repo        domain.Repository
		analyzed    int
		actionable  int
		languages   string
		languagesAt string
	)
	err := row.Scan(&repo.ID, &repo.FullName, &analyzed, &actionable, &repo.RulesPath, &languages, &languagesAt, &repo.DefaultBranch)
	if err != nil {
		return domain.Repository{}, err
	}
	repo.Analyzed = analyzed != 0
	repo.Actionable = actionable != 0
	repo.Languages = unmarshalStrings(languages)
	repo.LanguagesAt = parseTime(languagesAt)
	return repo, nil
}
