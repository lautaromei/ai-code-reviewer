package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type candidateRepo struct{ s *Database }

const candidateSelectColumns = `
SELECT id, language, label, status, merged_into, promoted_rule_id, classifier_version, created_at, updated_at
FROM candidate_clusters`

func (c *candidateRepo) Upsert(ctx context.Context, candidate domain.CandidateCluster) (domain.CandidateCluster, error) {
	const q = `
INSERT INTO candidate_clusters (id, language, label, status, merged_into, promoted_rule_id, classifier_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    language = excluded.language,
    label = excluded.label,
    status = excluded.status,
    merged_into = excluded.merged_into,
    promoted_rule_id = excluded.promoted_rule_id,
    classifier_version = excluded.classifier_version,
    updated_at = excluded.updated_at;`

	_, err := c.s.conn().ExecContext(ctx, q,
		candidate.ID, candidate.Language, candidate.Label, string(candidate.Status),
		candidate.MergedInto, candidate.PromotedRuleID, candidate.ClassifierVersion,
		formatTime(candidate.CreatedAt), formatTime(candidate.UpdatedAt),
	)
	if err != nil {
		return domain.CandidateCluster{}, fmt.Errorf("sqlite: upsert candidate %q: %w", candidate.ID, err)
	}
	return candidate, nil
}

func (c *candidateRepo) Get(ctx context.Context, id string) (domain.CandidateCluster, bool, error) {
	row := c.s.conn().QueryRowContext(ctx, candidateSelectColumns+` WHERE id = ?;`, id)
	cand, err := scanCandidate(row)
	if err == sql.ErrNoRows {
		return domain.CandidateCluster{}, false, nil
	}
	if err != nil {
		return domain.CandidateCluster{}, false, fmt.Errorf("sqlite: get candidate %q: %w", id, err)
	}
	return cand, true, nil
}

func (c *candidateRepo) ListMergedInto(ctx context.Context, survivingID string) ([]domain.CandidateCluster, error) {
	const q = candidateSelectColumns + ` WHERE status = ? AND merged_into = ? ORDER BY id;`

	rows, err := c.s.conn().QueryContext(ctx, q, string(domain.CandidateMerged), survivingID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list candidates merged into %q: %w", survivingID, err)
	}
	defer rows.Close()

	var out []domain.CandidateCluster
	for rows.Next() {
		cand, err := scanCandidate(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan candidate: %w", err)
		}
		out = append(out, cand)
	}
	return out, rows.Err()
}

func (c *candidateRepo) ListEvidence(ctx context.Context, candidateID string) ([]domain.CandidateEvidence, error) {
	const q = `
SELECT candidate_id, comment_id, kind, thumbs_up, thumbs_down, evaluated_at
FROM candidate_evidence
WHERE candidate_id = ?
ORDER BY comment_id;`

	rows, err := c.s.conn().QueryContext(ctx, q, candidateID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list evidence for candidate %q: %w", candidateID, err)
	}
	defer rows.Close()

	var out []domain.CandidateEvidence
	for rows.Next() {
		var (
			ev          domain.CandidateEvidence
			kind        string
			evaluatedAt string
		)
		if err := rows.Scan(&ev.CandidateID, &ev.CommentID, &kind, &ev.ThumbsUp, &ev.ThumbsDown, &evaluatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan evidence for candidate %q: %w", candidateID, err)
		}
		ev.Kind = domain.EvidenceKind(kind)
		ev.EvaluatedAt = parseTime(evaluatedAt)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (c *candidateRepo) ListOpen(ctx context.Context, language string) ([]domain.CandidateCluster, error) {
	const q = candidateSelectColumns + ` WHERE status = ? AND language = ? ORDER BY id;`

	rows, err := c.s.conn().QueryContext(ctx, q, string(domain.CandidateOpen), language)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list open candidates for %q: %w", language, err)
	}
	defer rows.Close()

	var out []domain.CandidateCluster
	for rows.Next() {
		cand, err := scanCandidate(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan candidate: %w", err)
		}
		out = append(out, cand)
	}
	return out, rows.Err()
}

func (c *candidateRepo) AttachEvidence(ctx context.Context, evidence domain.CandidateEvidence) error {
	const q = `
INSERT INTO candidate_evidence (candidate_id, comment_id, kind, thumbs_up, thumbs_down, evaluated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(candidate_id, comment_id) DO UPDATE SET
    kind = excluded.kind,
    thumbs_up = excluded.thumbs_up,
    thumbs_down = excluded.thumbs_down,
    evaluated_at = excluded.evaluated_at;`

	_, err := c.s.conn().ExecContext(ctx, q,
		evidence.CandidateID, evidence.CommentID, string(evidence.Kind),
		evidence.ThumbsUp, evidence.ThumbsDown, formatTime(evidence.EvaluatedAt),
	)
	if err != nil {
		return fmt.Errorf("sqlite: attach evidence candidate=%q comment=%d: %w", evidence.CandidateID, evidence.CommentID, err)
	}
	return nil
}

func (c *candidateRepo) Merge(ctx context.Context, survivingID string, mergedIDs []string) error {
	const q = `
UPDATE candidate_clusters SET status = ?, merged_into = ?, updated_at = ?
WHERE id = ?;`

	conn := c.s.conn()
	for _, id := range mergedIDs {
		if _, err := conn.ExecContext(ctx, q, string(domain.CandidateMerged), survivingID, nowRFC3339(), id); err != nil {
			return fmt.Errorf("sqlite: merging candidate %q into %q: %w", id, survivingID, err)
		}
	}
	return nil
}

func (c *candidateRepo) Promote(ctx context.Context, candidateID string, ruleID string) error {
	const q = `
UPDATE candidate_clusters SET status = ?, promoted_rule_id = ?, updated_at = ?
WHERE id = ?;`

	_, err := c.s.conn().ExecContext(ctx, q, string(domain.CandidatePromoted), ruleID, nowRFC3339(), candidateID)
	if err != nil {
		return fmt.Errorf("sqlite: promoting candidate %q to rule %q: %w", candidateID, ruleID, err)
	}
	return nil
}

func scanCandidate(row rowScanner) (domain.CandidateCluster, error) {
	var (
		cand      domain.CandidateCluster
		status    string
		createdAt string
		updatedAt string
	)
	err := row.Scan(&cand.ID, &cand.Language, &cand.Label, &status, &cand.MergedInto,
		&cand.PromotedRuleID, &cand.ClassifierVersion, &createdAt, &updatedAt)
	if err != nil {
		return domain.CandidateCluster{}, err
	}
	cand.Status = domain.CandidateStatus(status)
	cand.CreatedAt = parseTime(createdAt)
	cand.UpdatedAt = parseTime(updatedAt)
	return cand, nil
}
