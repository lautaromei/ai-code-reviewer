package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type ruleRepo struct{ s *Database }

const ruleSelectColumns = `
SELECT rule_id, language, title, scope, body, status, created_at, activated_at,
       last_used_at, disabled_at, disabled_reason, source, content_hash,
       rule_text, exception_text, bad_example, good_example
FROM rules`

func (r *ruleRepo) Insert(ctx context.Context, rule domain.Rule) error {
	const q = `
INSERT INTO rules (
    rule_id, language, title, scope, body, status, created_at, activated_at,
    last_used_at, disabled_at, disabled_reason, source, content_hash,
    rule_text, exception_text, bad_example, good_example
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`

	_, err := r.s.conn().ExecContext(ctx, q,
		rule.RuleID, rule.Language, rule.Title, rule.Scope, rule.Body, string(rule.Status),
		formatTime(rule.CreatedAt), formatTime(rule.ActivatedAt),
		formatTimePtr(rule.LastUsedAt), formatTimePtr(rule.DisabledAt),
		string(rule.DisabledReason), rule.Source, rule.ContentHash,
		rule.Rule, rule.Exception, rule.BadExample, rule.GoodExample,
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert rule %q: %w", rule.RuleID, err)
	}
	return nil
}

func (r *ruleRepo) Get(ctx context.Context, ruleID string) (domain.Rule, bool, error) {
	row := r.s.conn().QueryRowContext(ctx, ruleSelectColumns+` WHERE rule_id = ?;`, ruleID)
	rule, err := scanRule(row)
	if err == sql.ErrNoRows {
		return domain.Rule{}, false, nil
	}
	if err != nil {
		return domain.Rule{}, false, fmt.Errorf("sqlite: get rule %q: %w", ruleID, err)
	}
	return rule, true, nil
}

func (r *ruleRepo) ListReferences(ctx context.Context, ruleID string) ([]domain.RuleReference, error) {
	const mined = `
SELECT repositories.full_name, comments.pull_request_number, comments.github_comment_id,
       comments.url, comments.path, 0, comments.body, comments.created_at,
       SUM(CASE WHEN reactions.content = '-1' THEN 1 ELSE 0 END)
FROM rule_usages
JOIN comments ON comments.id = rule_usages.comment_id
JOIN repositories ON repositories.id = comments.repository_id
LEFT JOIN reactions ON reactions.comment_id = comments.id
WHERE rule_usages.rule_id = ?
GROUP BY comments.id`
	rows, err := r.s.conn().QueryContext(ctx, mined, ruleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list mined references for rule %q: %w", ruleID, err)
	}
	references, err := scanRuleReferences(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	const published = `
SELECT repositories.full_name, review_jobs.pull_request_number, review_findings.github_comment_id,
       '', review_findings.path, review_findings.line, review_findings.body, review_attempts.finished_at, 0
FROM review_findings
JOIN review_attempts ON review_attempts.id = review_findings.attempt_id
JOIN review_jobs ON review_jobs.id = review_attempts.job_id
JOIN repositories ON repositories.id = review_jobs.repository_id
WHERE review_findings.rule_id = ? AND review_findings.publication_status = 'published'
  AND review_findings.github_comment_id > 0
  AND NOT EXISTS (
      SELECT 1 FROM comments
      WHERE comments.repository_id = repositories.id
        AND comments.github_comment_id = review_findings.github_comment_id
  )`
	rows, err = r.s.conn().QueryContext(ctx, published, ruleID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list published references for rule %q: %w", ruleID, err)
	}
	defer rows.Close()
	publishedReferences, err := scanRuleReferences(rows)
	if err != nil {
		return nil, err
	}
	references = append(references, publishedReferences...)
	sort.Slice(references, func(i, j int) bool { return references[i].CreatedAt.After(references[j].CreatedAt) })
	return references, nil
}

func scanRuleReferences(rows *sql.Rows) ([]domain.RuleReference, error) {
	var out []domain.RuleReference
	for rows.Next() {
		var reference domain.RuleReference
		var createdAt string
		if err := rows.Scan(&reference.Repository, &reference.PullRequestNumber, &reference.GitHubCommentID,
			&reference.URL, &reference.Path, &reference.Line, &reference.Body, &createdAt, &reference.ThumbsDown); err != nil {
			return nil, fmt.Errorf("sqlite: scan rule reference: %w", err)
		}
		reference.CreatedAt = parseTime(createdAt)
		if reference.URL == "" {
			reference.URL = fmt.Sprintf("https://github.com/%s/pull/%d#discussion_r%d", reference.Repository, reference.PullRequestNumber, reference.GitHubCommentID)
		}
		out = append(out, reference)
	}
	return out, rows.Err()
}

func (r *ruleRepo) ListByStatus(ctx context.Context, status domain.RuleStatus) ([]domain.Rule, error) {
	rows, err := r.s.conn().QueryContext(ctx, ruleSelectColumns+` WHERE status = ? ORDER BY rule_id;`, string(status))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list rules by status %q: %w", status, err)
	}
	defer rows.Close()
	return scanRules(rows)
}

func (r *ruleRepo) ListActiveByLanguages(ctx context.Context, languages []string) ([]domain.Rule, error) {
	if len(languages) == 0 {
		return nil, nil
	}
	placeholders := make([]byte, 0, len(languages)*2)
	args := make([]any, 0, len(languages)+1)
	args = append(args, string(domain.RuleActive))
	for i, lang := range languages {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, lang)
	}

	q := ruleSelectColumns + ` WHERE status = ? AND language IN (` + string(placeholders) + `) ORDER BY rule_id;`
	rows, err := r.s.conn().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list active rules by languages: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

func (r *ruleRepo) UpdateStatus(ctx context.Context, event domain.RuleStatusEvent) error {
	conn := r.s.conn()

	const updateRule = `
UPDATE rules SET status = ?, disabled_reason = ?, disabled_at = ?
WHERE rule_id = ?;`
	var disabledAt string
	if event.ToState == domain.RuleDisabled || event.ToState == domain.RuleExpired {
		disabledAt = formatTime(event.OccurredAt)
	}
	if _, err := conn.ExecContext(ctx, updateRule, string(event.ToState), string(event.Reason), disabledAt, event.RuleID); err != nil {
		return fmt.Errorf("sqlite: updating rule status %q: %w", event.RuleID, err)
	}

	const insertHistory = `
INSERT INTO rule_status_history (event_key, rule_id, from_state, to_state, reason, run_id, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_key) DO NOTHING;`
	if _, err := conn.ExecContext(ctx, insertHistory,
		event.EventKey, event.RuleID, string(event.FromState), string(event.ToState),
		string(event.Reason), event.RunID, formatTime(event.OccurredAt),
	); err != nil {
		return fmt.Errorf("sqlite: recording rule status history %q: %w", event.EventKey, err)
	}
	return nil
}

func (r *ruleRepo) Delete(ctx context.Context, ruleID string) error {
	return r.s.withinTx(ctx, func(txStore *Database) error {
		conn := txStore.conn()
		for _, statement := range []string{
			`DELETE FROM snapshot_rules WHERE rule_id = ?`,
			`DELETE FROM rule_status_history WHERE rule_id = ?`,
			`DELETE FROM rule_usages WHERE rule_id = ?`,
			`DELETE FROM rules WHERE rule_id = ?`,
		} {
			if _, err := conn.ExecContext(ctx, statement, ruleID); err != nil {
				return fmt.Errorf("sqlite: delete rule %q: %w", ruleID, err)
			}
		}
		return nil
	})
}

func (r *ruleRepo) RecordUsage(ctx context.Context, usage domain.RuleUsage) error {
	const q = `
INSERT INTO rule_usages (rule_id, comment_id, used_at)
VALUES (?, ?, ?)
ON CONFLICT(rule_id, comment_id) DO UPDATE SET used_at = excluded.used_at;`

	conn := r.s.conn()
	usedAt := formatTime(usage.UsedAt)
	_, err := conn.ExecContext(ctx, q, usage.RuleID, usage.CommentID, usedAt)
	if err != nil {
		return fmt.Errorf("sqlite: recording usage of rule %q on comment %d: %w", usage.RuleID, usage.CommentID, err)
	}

	const updateLastUsedAt = `
UPDATE rules
SET last_used_at = ?
WHERE rule_id = ? AND (last_used_at = '' OR last_used_at < ?);`
	if _, err := conn.ExecContext(ctx, updateLastUsedAt, usedAt, usage.RuleID, usedAt); err != nil {
		return fmt.Errorf("sqlite: updating last_used_at for rule %q: %w", usage.RuleID, err)
	}
	return nil
}

func (r *ruleRepo) NextSequence(ctx context.Context, languageCode string) (int, error) {
	const q = `
INSERT INTO rule_sequences (language, next_value)
VALUES (?, 1)
ON CONFLICT(language) DO UPDATE SET next_value = rule_sequences.next_value + 1
RETURNING next_value;`

	var next int
	if err := r.s.conn().QueryRowContext(ctx, q, languageCode).Scan(&next); err != nil {
		return 0, fmt.Errorf("sqlite: next sequence for language %q: %w", languageCode, err)
	}
	return next, nil
}

func scanRule(row rowScanner) (domain.Rule, error) {
	var (
		rule           domain.Rule
		status         string
		createdAt      string
		activatedAt    string
		lastUsedAt     string
		disabledAt     string
		disabledReason string
	)
	err := row.Scan(
		&rule.RuleID, &rule.Language, &rule.Title, &rule.Scope, &rule.Body, &status,
		&createdAt, &activatedAt, &lastUsedAt, &disabledAt, &disabledReason,
		&rule.Source, &rule.ContentHash,
		&rule.Rule, &rule.Exception, &rule.BadExample, &rule.GoodExample,
	)
	if err != nil {
		return domain.Rule{}, err
	}
	rule.Status = domain.RuleStatus(status)
	rule.CreatedAt = parseTime(createdAt)
	rule.ActivatedAt = parseTime(activatedAt)
	rule.LastUsedAt = parseTimePtr(lastUsedAt)
	rule.DisabledAt = parseTimePtr(disabledAt)
	rule.DisabledReason = domain.DisabledReason(disabledReason)
	return rule, nil
}

func scanRules(rows *sql.Rows) ([]domain.Rule, error) {
	var out []domain.Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan rule: %w", err)
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}
