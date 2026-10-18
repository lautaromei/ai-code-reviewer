package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"time"
)

type PromptStore struct{ s *Database }

const GenericReviewPrompt = `Review added and modified lines for concrete correctness, security, reliability, and maintainability problems. Avoid formatter or linter noise. Be specific, respectful, and solution oriented. Use only catalog rules supplied by the service. Return structured findings and do not publish or change the pull request.`

const GenericValidationPrompt = `High-signal validation

Act as an independent verifier. Evaluate every candidate finding against the supplied diff and applicable rules.

Use an asymmetric standard: reject a candidate only when the available evidence positively disproves it, proves it is pre-existing, proves an explicit rule exception applies, or proves it duplicates another candidate. Missing context, incomplete evidence, or reasonable doubt must produce uncertain, not reject. Do not reject based only on the candidate's confidence; deterministic validation applies that policy separately.

Reject cosmetic observations that do not identify a concrete correctness, security, reliability, performance, or maintainability impact. This includes formatting preferences, naming preferences, stylistic nits, and requests to extract constants that do not prevent a demonstrated defect.

Do not create, rewrite, reformat, or expand findings. Return exactly one verdict for every candidate ID: accept, reject, or uncertain, with a concise evidence-based reason.`

func promptHash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func (p *PromptStore) Save(ctx context.Context, scope, repo, text, origin, author string, expected int64) (domain.ReviewPrompt, error) {
	var out domain.ReviewPrompt
	if text == "" {
		return out, errors.New("prompt cannot be empty")
	}
	e := p.s.withinTx(ctx, func(tx *Database) error {
		var current sql.NullInt64
		var version int64
		e := tx.conn().QueryRowContext(ctx, `SELECT revision_id,version FROM review_prompt_bindings WHERE scope=? AND repository=?`, scope, repo).Scan(&current, &version)
		if errors.Is(e, sql.ErrNoRows) {
			version = 0
		} else if e != nil {
			return e
		}
		if version != expected {
			return fmt.Errorf("prompt version conflict: expected %d, current %d", expected, version)
		}
		res, e := tx.conn().ExecContext(ctx, `INSERT INTO review_prompt_revisions(scope,repository,text,hash,origin,author,previous_revision_id,created_at) VALUES(?,?,?,?,?,?,?,?)`, scope, repo, text, promptHash(text), origin, author, current, time.Now().UTC().Format(time.RFC3339))
		if e != nil {
			return e
		}
		id, _ := res.LastInsertId()
		_, e = tx.conn().ExecContext(ctx, `INSERT INTO review_prompt_bindings(scope,repository,revision_id,use_global,version) VALUES(?,?,?,0,1) ON CONFLICT(scope,repository) DO UPDATE SET revision_id=excluded.revision_id,use_global=0,version=review_prompt_bindings.version+1`, scope, repo, id)
		return e
	})
	if e != nil {
		return out, e
	}
	if scope == "validation" {
		return p.Validation(ctx)
	}
	return p.Effective(ctx, repo)
}
func (p *PromptStore) Validation(ctx context.Context) (domain.ReviewPrompt, error) {
	var out domain.ReviewPrompt
	var created string
	e := p.s.conn().QueryRowContext(ctx, `SELECT r.id,r.scope,r.repository,r.text,r.hash,r.origin,r.author,r.created_at,b.version FROM review_prompt_bindings b JOIN review_prompt_revisions r ON r.id=b.revision_id WHERE b.scope='validation' AND b.repository=''`).Scan(&out.Revision.ID, &out.Revision.Scope, &out.Revision.Repository, &out.Revision.Text, &out.Revision.Hash, &out.Revision.Origin, &out.Revision.Author, &created, &out.BindingVersion)
	if e == nil {
		out.Revision.CreatedAt = parseTime(created)
		out.EffectiveOrigin = "validation"
		return out, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	out.Revision = domain.ReviewPromptRevision{Scope: "validation", Text: GenericValidationPrompt, Hash: promptHash(GenericValidationPrompt), Origin: "product", CreatedAt: time.Now().UTC()}
	out.EffectiveOrigin = "product default"
	return out, nil
}
func (p *PromptStore) UseGlobal(ctx context.Context, repo string, expected int64) error {
	if expected == 0 {
		res, e := p.s.conn().ExecContext(ctx, `INSERT OR IGNORE INTO review_prompt_bindings(scope,repository,revision_id,use_global,version) VALUES('repo',?,NULL,1,1)`, repo)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 1 {
			return nil
		}
	}
	res, e := p.s.conn().ExecContext(ctx, `UPDATE review_prompt_bindings SET use_global=1,version=version+1 WHERE scope='repo' AND repository=? AND version=?`, repo, expected)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("prompt version conflict")
	}
	return nil
}
func (p *PromptStore) Effective(ctx context.Context, repo string) (domain.ReviewPrompt, error) {
	var out domain.ReviewPrompt
	queries := []struct {
		q      string
		a      []any
		origin string
	}{{`SELECT r.id,r.scope,r.repository,r.text,r.hash,r.origin,r.author,r.created_at,b.version FROM review_prompt_bindings b JOIN review_prompt_revisions r ON r.id=b.revision_id WHERE b.scope='repo' AND b.repository=? AND b.use_global=0`, []any{repo}, "repo override"}, {`SELECT r.id,r.scope,r.repository,r.text,r.hash,r.origin,r.author,r.created_at,0 FROM review_prompt_defaults d JOIN review_prompt_revisions r ON r.id=d.adapted_revision_id WHERE d.repository=?`, []any{repo}, "repository default"}, {`SELECT r.id,r.scope,r.repository,r.text,r.hash,r.origin,r.author,r.created_at,b.version FROM review_prompt_bindings b JOIN review_prompt_revisions r ON r.id=b.revision_id WHERE b.scope='global' AND b.repository=''`, nil, "global"}}
	for _, x := range queries {
		var created string
		e := p.s.conn().QueryRowContext(ctx, x.q, x.a...).Scan(&out.Revision.ID, &out.Revision.Scope, &out.Revision.Repository, &out.Revision.Text, &out.Revision.Hash, &out.Revision.Origin, &out.Revision.Author, &created, &out.BindingVersion)
		if e == nil {
			out.Revision.CreatedAt = parseTime(created)
			out.EffectiveOrigin = x.origin
			return out, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
	}
	out.Revision = domain.ReviewPromptRevision{Scope: "product", Text: GenericReviewPrompt, Hash: promptHash(GenericReviewPrompt), Origin: "product", CreatedAt: time.Now().UTC()}
	out.EffectiveOrigin = "product default"
	return out, nil
}
func (p *PromptStore) ByHash(ctx context.Context, repo, hash string) (domain.ReviewPrompt, error) {
	if hash == promptHash(GenericReviewPrompt) {
		return domain.ReviewPrompt{Revision: domain.ReviewPromptRevision{Scope: "product", Text: GenericReviewPrompt, Hash: hash, Origin: "product"}, EffectiveOrigin: "captured product default"}, nil
	}
	var out domain.ReviewPrompt
	var created string
	e := p.s.conn().QueryRowContext(ctx, `SELECT id,scope,repository,text,hash,origin,author,created_at FROM review_prompt_revisions WHERE hash=? AND (repository=? OR repository='') ORDER BY id DESC LIMIT 1`, hash, repo).Scan(&out.Revision.ID, &out.Revision.Scope, &out.Revision.Repository, &out.Revision.Text, &out.Revision.Hash, &out.Revision.Origin, &out.Revision.Author, &created)
	if e != nil {
		return out, e
	}
	out.Revision.CreatedAt = parseTime(created)
	out.EffectiveOrigin = "captured revision"
	return out, nil
}
func (p *PromptStore) List(ctx context.Context, scope, repo string) ([]domain.ReviewPromptRevision, error) {
	rows, e := p.s.conn().QueryContext(ctx, `SELECT id,scope,repository,text,hash,origin,author,created_at FROM review_prompt_revisions WHERE scope=? AND repository=? ORDER BY id DESC`, scope, repo)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []domain.ReviewPromptRevision
	for rows.Next() {
		var x domain.ReviewPromptRevision
		var at string
		if e = rows.Scan(&x.ID, &x.Scope, &x.Repository, &x.Text, &x.Hash, &x.Origin, &x.Author, &at); e != nil {
			return nil, e
		}
		x.CreatedAt = parseTime(at)
		out = append(out, x)
	}
	return out, rows.Err()
}
