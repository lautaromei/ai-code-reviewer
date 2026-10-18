package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type commentRepo struct{ s *Database }

func (c *commentRepo) Upsert(ctx context.Context, comment domain.Comment) (domain.Comment, error) {
	const q = `
INSERT INTO comments (
    github_comment_id, repository_id, pull_request_number, review_id, parent_comment_id,
    author, author_kind, origin, body, path, diff_hunk, language, url, rule_ids,
    created_at, updated_at, ingested_at, input_hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(repository_id, github_comment_id) DO UPDATE SET
    pull_request_number = excluded.pull_request_number,
    review_id = excluded.review_id,
    parent_comment_id = excluded.parent_comment_id,
    author = excluded.author,
    author_kind = excluded.author_kind,
    origin = excluded.origin,
    body = excluded.body,
    path = excluded.path,
    diff_hunk = excluded.diff_hunk,
    language = excluded.language,
    url = excluded.url,
    rule_ids = excluded.rule_ids,
    created_at = excluded.created_at,
    updated_at = excluded.updated_at,
    ingested_at = excluded.ingested_at,
    input_hash = excluded.input_hash
RETURNING id;`

	row := c.s.conn().QueryRowContext(ctx, q,
		comment.GitHubCommentID, comment.RepositoryID, comment.PullRequestNumber, comment.ReviewID,
		formatInt64Ptr(comment.ParentCommentID),
		comment.Author, string(comment.AuthorKind), string(comment.Origin), comment.Body, comment.Path,
		comment.DiffHunk, comment.Language, comment.URL, marshalStrings(comment.RuleIDs),
		formatTime(comment.CreatedAt), formatTime(comment.UpdatedAt), formatTime(comment.IngestedAt),
		comment.InputHash,
	)
	if err := row.Scan(&comment.ID); err != nil {
		return domain.Comment{}, fmt.Errorf("sqlite: upsert comment (%d, %d): %w", comment.RepositoryID, comment.GitHubCommentID, err)
	}
	return comment, nil
}

func (c *commentRepo) GetByID(ctx context.Context, id int64) (domain.Comment, bool, error) {
	row := c.s.conn().QueryRowContext(ctx, commentSelectColumns+` WHERE id = ?;`, id)
	comment, err := scanComment(row)
	if err == sql.ErrNoRows {
		return domain.Comment{}, false, nil
	}
	if err != nil {
		return domain.Comment{}, false, fmt.Errorf("sqlite: get comment %d: %w", id, err)
	}
	return comment, true, nil
}

func (c *commentRepo) GetByGitHubID(ctx context.Context, repositoryID int64, githubCommentID int64) (domain.Comment, bool, error) {
	row := c.s.conn().QueryRowContext(ctx, commentSelectColumns+` WHERE repository_id = ? AND github_comment_id = ?;`,
		repositoryID, githubCommentID)
	comment, err := scanComment(row)
	if err == sql.ErrNoRows {
		return domain.Comment{}, false, nil
	}
	if err != nil {
		return domain.Comment{}, false, fmt.Errorf("sqlite: get comment (%d, %d): %w", repositoryID, githubCommentID, err)
	}
	return comment, true, nil
}

func (c *commentRepo) ListPendingClassification(ctx context.Context, promptVersion string) ([]domain.Comment, error) {
	const q = commentSelectColumns + `
WHERE NOT EXISTS (
    SELECT 1 FROM classifications cl
    WHERE cl.comment_id = comments.id
      AND cl.input_hash = comments.input_hash
      AND cl.prompt_version = ?
)
ORDER BY comments.id;`

	rows, err := c.s.conn().QueryContext(ctx, q, promptVersion)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list pending classification: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

func (c *commentRepo) ListTrackedAI(ctx context.Context) ([]domain.Comment, error) {
	const q = commentSelectColumns + `
WHERE origin IN (?, ?)
ORDER BY comments.id;`

	rows, err := c.s.conn().QueryContext(ctx, q, string(domain.OriginAIRuleUsage), string(domain.OriginAICandidate))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tracked ai comments: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

func (c *commentRepo) ListForRule(ctx context.Context, ruleID string) ([]domain.Comment, error) {
	const q = `
SELECT comments.id, comments.github_comment_id, comments.repository_id, comments.pull_request_number,
       comments.review_id, comments.parent_comment_id, comments.author, comments.author_kind,
       comments.origin, comments.body, comments.path, comments.diff_hunk, comments.language,
       comments.url, comments.rule_ids, comments.created_at, comments.updated_at, comments.ingested_at,
       comments.input_hash
FROM comments
JOIN rule_usages ON rule_usages.comment_id = comments.id
WHERE rule_usages.rule_id = ?
ORDER BY comments.id;`

	rows, err := c.s.conn().QueryContext(ctx, q, ruleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list comments for rule %q: %w", ruleID, err)
	}
	defer rows.Close()
	return scanComments(rows)
}

func (c *commentRepo) ListAllOrdered(ctx context.Context) ([]domain.Comment, error) {
	const q = commentSelectColumns + `
ORDER BY comments.repository_id, comments.created_at, comments.github_comment_id;`

	rows, err := c.s.conn().QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list all comments ordered: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

const commentSelectColumns = `
SELECT id, github_comment_id, repository_id, pull_request_number, review_id, parent_comment_id,
       author, author_kind, origin, body, path, diff_hunk, language, url, rule_ids,
       created_at, updated_at, ingested_at, input_hash
FROM comments`

func scanComment(row rowScanner) (domain.Comment, error) {
	var (
		comment         domain.Comment
		parentCommentID sql.NullInt64
		authorKind      string
		origin          string
		ruleIDs         string
		createdAt       string
		updatedAt       string
		ingestedAt      string
	)
	err := row.Scan(
		&comment.ID, &comment.GitHubCommentID, &comment.RepositoryID, &comment.PullRequestNumber,
		&comment.ReviewID, &parentCommentID, &comment.Author, &authorKind, &origin, &comment.Body,
		&comment.Path, &comment.DiffHunk, &comment.Language, &comment.URL, &ruleIDs,
		&createdAt, &updatedAt, &ingestedAt, &comment.InputHash,
	)
	if err != nil {
		return domain.Comment{}, err
	}
	comment.ParentCommentID = parseInt64Ptr(parentCommentID)
	comment.AuthorKind = domain.AuthorKind(authorKind)
	comment.Origin = domain.CommentOrigin(origin)
	comment.RuleIDs = unmarshalStrings(ruleIDs)
	comment.CreatedAt = parseTime(createdAt)
	comment.UpdatedAt = parseTime(updatedAt)
	comment.IngestedAt = parseTime(ingestedAt)
	return comment, nil
}

func scanComments(rows *sql.Rows) ([]domain.Comment, error) {
	var out []domain.Comment
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan comment: %w", err)
		}
		out = append(out, comment)
	}
	return out, rows.Err()
}
