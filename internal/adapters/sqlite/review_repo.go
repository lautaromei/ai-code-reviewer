package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"sort"
	"strings"
	"time"
)

type ReviewStore struct{ s *Database }

func (r *ReviewStore) CapturePrompt(ctx context.Context, job int64, p domain.ReviewPrompt) error {
	var revision any
	if p.Revision.ID > 0 {
		revision = p.Revision.ID
	}
	_, e := r.s.conn().ExecContext(ctx, `UPDATE review_jobs SET prompt_revision_id=?,prompt_hash=?,updated_at=? WHERE id=? AND prompt_hash=''`, revision, p.Revision.Hash, nowRFC3339(), job)
	return e
}

func (r *ReviewStore) AcquireMaintenance(ctx context.Context, name, owner string, d time.Duration) (bool, error) {
	now := time.Now().UTC()
	res, e := r.s.conn().ExecContext(ctx, `INSERT INTO maintenance_leases(name,owner,lease_until,updated_at) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET owner=excluded.owner,lease_until=excluded.lease_until,updated_at=excluded.updated_at WHERE maintenance_leases.lease_until<?`, name, owner, now.Add(d).Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
	if e != nil {
		return false, e
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
func (r *ReviewStore) ReleaseMaintenance(ctx context.Context, name, owner string) error {
	_, e := r.s.conn().ExecContext(ctx, `DELETE FROM maintenance_leases WHERE name=? AND owner=?`, name, owner)
	return e
}
func (r *ReviewStore) MaintenanceDue(ctx context.Context, interval time.Duration) (bool, error) {
	var at string
	e := r.s.conn().QueryRowContext(ctx, `SELECT finished_at FROM runs WHERE command='sync' AND status='succeeded' ORDER BY id DESC LIMIT 1`).Scan(&at)
	if errors.Is(e, sql.ErrNoRows) {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	return time.Since(parseTime(at)) >= interval, nil
}

func (r *ReviewStore) RecordDelivery(ctx context.Context, id, event, action, result string, at time.Time) (bool, error) {
	res, e := r.s.conn().ExecContext(ctx, `INSERT OR IGNORE INTO webhook_deliveries(delivery_id,event_type,action,received_at,result) VALUES(?,?,?,?,?)`, id, event, action, at.Format(time.RFC3339), result)
	if e != nil {
		return false, e
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
func (r *ReviewStore) EnqueueDelivery(ctx context.Context, id, event, action, result, repo string, pr int64, sha string, at time.Time) (bool, error) {
	fresh := false
	e := r.s.withinTx(ctx, func(tx *Database) error {
		res, e := tx.conn().ExecContext(ctx, `INSERT OR IGNORE INTO webhook_deliveries(delivery_id,event_type,action,repository,received_at,result) VALUES(?,?,?,?,?,?)`, id, event, action, repo, at.Format(time.RFC3339), result)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		fresh = n == 1
		if !fresh || result != "queued" {
			return nil
		}
		_, e = tx.conn().ExecContext(ctx, `INSERT INTO review_jobs(repository_id,pull_request_number,initial_head_sha,status,created_at,updated_at)
			SELECT id,?,?,?,?,? FROM repositories WHERE full_name=? AND deleted_at=''
			ON CONFLICT(repository_id,pull_request_number) DO UPDATE SET
			initial_head_sha=excluded.initial_head_sha,status=excluded.status,attempts=0,next_attempt_at='',lease_owner='',lease_until='',last_error='',terminal_reason='',updated_at=excluded.updated_at
			WHERE review_jobs.status=?`, pr, sha, domain.ReviewQueued, at.Format(time.RFC3339), at.Format(time.RFC3339), repo, domain.ReviewSkipped)
		return e
	})
	return fresh, e
}

func (r *ReviewStore) SyncRepositories(ctx context.Context, repos map[string][]string) error {
	return r.s.withinTx(ctx, func(tx *Database) error {
		for name, cats := range repos {
			_, e := tx.conn().ExecContext(ctx, `INSERT INTO repositories(full_name,analyzed,actionable) VALUES(?,1,1) ON CONFLICT(full_name) DO UPDATE SET analyzed=1, actionable=1`, name)
			if e != nil {
				return fmt.Errorf("sync repository %s: %w", name, e)
			}
			var id int64
			e = tx.conn().QueryRowContext(ctx, `SELECT id FROM repositories WHERE full_name=?`, name).Scan(&id)
			if e != nil {
				return fmt.Errorf("resolve repository %s: %w", name, e)
			}
			if _, e = tx.conn().ExecContext(ctx, `DELETE FROM repository_categories WHERE repository_id=?`, id); e != nil {
				return e
			}
			for _, c := range cats {
				if _, e = tx.conn().ExecContext(ctx, `INSERT INTO repository_categories(repository_id,category) VALUES(?,?)`, id, c); e != nil {
					return e
				}
			}
		}
		return nil
	})
}

// SeedRepositorySettings imports configuration for repositories that are not yet
// known. Existing rows are deliberately left untouched so web edits survive restarts.
func (r *ReviewStore) SeedRepositorySettings(ctx context.Context, repos []domain.RepositorySettings) error {
	return r.s.withinTx(ctx, func(tx *Database) error {
		for _, repo := range repos {
			res, e := tx.conn().ExecContext(ctx, `INSERT OR IGNORE INTO repositories(full_name,analyzed,actionable,review_enabled,mining_enabled,settings_updated_at) VALUES(?,1,1,?,?,?)`, repo.FullName, boolToInt(repo.ReviewEnabled), boolToInt(repo.MiningEnabled), nowRFC3339())
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			var id int64
			var updated string
			if e = tx.conn().QueryRowContext(ctx, `SELECT id,settings_updated_at FROM repositories WHERE full_name=?`, repo.FullName).Scan(&id, &updated); e != nil {
				return e
			}
			if n == 0 && updated != "" {
				continue
			}
			if n == 0 {
				if _, e = tx.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=?,mining_enabled=?,settings_updated_at=? WHERE id=?`, boolToInt(repo.ReviewEnabled), boolToInt(repo.MiningEnabled), nowRFC3339(), id); e != nil {
					return e
				}
				if _, e = tx.conn().ExecContext(ctx, `DELETE FROM repository_categories WHERE repository_id=?`, id); e != nil {
					return e
				}
			}
			for _, category := range normalizedCategories(repo.Categories) {
				if _, e = tx.conn().ExecContext(ctx, `INSERT INTO repository_categories(repository_id,category) VALUES(?,?)`, id, category); e != nil {
					return e
				}
			}
		}
		return nil
	})
}

func (r *ReviewStore) SeedReviewModel(ctx context.Context, model string) error {
	model, e := normalizeReviewModel(model, false)
	if e != nil {
		return e
	}
	_, e = r.s.conn().ExecContext(ctx, `INSERT OR IGNORE INTO review_model_settings(singleton,global_model,version,updated_at) VALUES(1,?,1,?)`, model, nowRFC3339())
	return e
}

func (r *ReviewStore) ReviewModelSettings(ctx context.Context) (domain.ReviewModelSettings, error) {
	var x domain.ReviewModelSettings
	var updated, generationModels string
	e := r.s.conn().QueryRowContext(ctx, `SELECT global_model,verification_model,generation_agents,generation_models,version,actor,updated_at FROM review_model_settings WHERE singleton=1`).Scan(&x.GlobalModel, &x.VerificationModel, &x.GenerationAgents, &generationModels, &x.Version, &x.Actor, &updated)
	if e == nil && strings.TrimSpace(generationModels) != "" {
		e = json.Unmarshal([]byte(generationModels), &x.GenerationModels)
	}
	if e == nil && len(x.GenerationModels) == 0 {
		x.GenerationModels = make([]string, x.GenerationAgents)
		for i := range x.GenerationModels {
			x.GenerationModels[i] = x.GlobalModel
		}
	}
	x.UpdatedAt = parseTime(updated)
	return x, e
}

func (r *ReviewStore) SaveGlobalReviewModels(ctx context.Context, model, verificationModel string, generationModels []string, expected int64, actor string) (domain.ReviewModelSettings, error) {
	model, e := normalizeReviewModel(model, false)
	if e != nil {
		return domain.ReviewModelSettings{}, e
	}
	verificationModel, e = normalizeVerificationModel(verificationModel)
	if e != nil {
		return domain.ReviewModelSettings{}, e
	}
	if len(generationModels) < 1 || len(generationModels) > 10 {
		return domain.ReviewModelSettings{}, errors.New("generation agents must be between 1 and 10")
	}
	for i := range generationModels {
		generationModels[i], e = normalizeReviewModel(generationModels[i], false)
		if e != nil {
			return domain.ReviewModelSettings{}, fmt.Errorf("generation agent %d: %w", i+1, e)
		}
	}
	encoded, _ := json.Marshal(generationModels)
	res, e := r.s.conn().ExecContext(ctx, `UPDATE review_model_settings SET global_model=?,verification_model=?,generation_agents=?,generation_models=?,version=version+1,actor=?,updated_at=? WHERE singleton=1 AND version=?`, model, verificationModel, len(generationModels), string(encoded), strings.TrimSpace(actor), nowRFC3339(), expected)
	if e != nil {
		return domain.ReviewModelSettings{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return domain.ReviewModelSettings{}, errors.New("model settings version conflict")
	}
	return r.ReviewModelSettings(ctx)
}

func (r *ReviewStore) GenerationModels(ctx context.Context, repository string) ([]string, error) {
	settings, e := r.ReviewModelSettings(ctx)
	if e != nil {
		return nil, e
	}
	var override string
	e = r.s.conn().QueryRowContext(ctx, `SELECT review_model FROM repositories WHERE full_name=? AND deleted_at=''`, repository).Scan(&override)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(override) != "" {
		for i := range settings.GenerationModels {
			settings.GenerationModels[i] = override
		}
	}
	return settings.GenerationModels, nil
}

func (r *ReviewStore) EffectiveVerificationModel(ctx context.Context) (string, error) {
	var model string
	e := r.s.conn().QueryRowContext(ctx, `SELECT verification_model FROM review_model_settings WHERE singleton=1`).Scan(&model)
	return model, e
}

func (r *ReviewStore) EffectiveReviewModel(ctx context.Context, repository string) (string, error) {
	var model string
	e := r.s.conn().QueryRowContext(ctx, `SELECT CASE WHEN trim(r.review_model)<>'' THEN r.review_model ELSE s.global_model END FROM repositories r CROSS JOIN review_model_settings s WHERE r.full_name=? AND s.singleton=1`, repository).Scan(&model)
	return model, e
}

func (r *ReviewStore) ListRepositorySettings(ctx context.Context) ([]domain.RepositorySettings, error) {
	rows, e := r.s.conn().QueryContext(ctx, `SELECT id,full_name,review_enabled,mining_enabled,review_model,settings_version,settings_updated_at FROM repositories WHERE deleted_at='' ORDER BY full_name`)
	if e != nil {
		return nil, e
	}
	var out []domain.RepositorySettings
	for rows.Next() {
		var x domain.RepositorySettings
		var review, mining int
		var updated string
		if e = rows.Scan(&x.ID, &x.FullName, &review, &mining, &x.ReviewModel, &x.Version, &updated); e != nil {
			return nil, e
		}
		x.ReviewEnabled, x.MiningEnabled, x.UpdatedAt = review != 0, mining != 0, parseTime(updated)
		out = append(out, x)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return nil, e
	}
	if e = rows.Close(); e != nil {
		return nil, e
	}
	for i := range out {
		out[i].Categories, e = r.repositoryCategories(ctx, out[i].ID)
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

func (r *ReviewStore) RepositorySettings(ctx context.Context, name string) (domain.RepositorySettings, bool, error) {
	var x domain.RepositorySettings
	var review, mining int
	var updated string
	e := r.s.conn().QueryRowContext(ctx, `SELECT id,full_name,review_enabled,mining_enabled,review_model,settings_version,settings_updated_at FROM repositories WHERE full_name=? AND deleted_at=''`, name).Scan(&x.ID, &x.FullName, &review, &mining, &x.ReviewModel, &x.Version, &updated)
	if errors.Is(e, sql.ErrNoRows) {
		return x, false, nil
	}
	if e != nil {
		return x, false, e
	}
	x.ReviewEnabled, x.MiningEnabled, x.UpdatedAt = review != 0, mining != 0, parseTime(updated)
	x.Categories, e = r.repositoryCategories(ctx, x.ID)
	return x, true, e
}

func (r *ReviewStore) ReviewEnabled(ctx context.Context, name string) (bool, error) {
	x, ok, e := r.RepositorySettings(ctx, name)
	return ok && x.ReviewEnabled, e
}

// RegisterInstallationRepositories makes GitHub App installation the zero-touch
// onboarding path. Existing dashboard choices are preserved; newly discovered
// repositories start review-enabled with the safe, language-agnostic category.
func (r *ReviewStore) RegisterInstallationRepositories(ctx context.Context, installationID int64, names []string) error {
	if installationID <= 0 {
		return errors.New("github installation id must be positive")
	}
	return r.s.withinTx(ctx, func(tx *Database) error {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" || !strings.Contains(name, "/") {
				return fmt.Errorf("invalid installed repository %q", name)
			}
			_, e := tx.conn().ExecContext(ctx, `INSERT OR IGNORE INTO repositories(full_name,analyzed,actionable,review_enabled,mining_enabled,settings_updated_at,github_installation_id) VALUES(?,1,1,1,0,?,?)`, name, nowRFC3339(), installationID)
			if e != nil {
				return e
			}
			if _, e = tx.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=CASE WHEN github_installation_id=0 THEN 1 ELSE review_enabled END,github_installation_id=? WHERE full_name=? AND deleted_at=''`, installationID, name); e != nil {
				return e
			}
			var id int64
			if e = tx.conn().QueryRowContext(ctx, `SELECT id FROM repositories WHERE full_name=? AND deleted_at=''`, name).Scan(&id); errors.Is(e, sql.ErrNoRows) {
				continue
			} else if e != nil {
				return e
			}
			if _, e = tx.conn().ExecContext(ctx, `INSERT INTO repository_categories(repository_id,category) SELECT ?, 'COMMON' WHERE NOT EXISTS(SELECT 1 FROM repository_categories WHERE repository_id=?)`, id, id); e != nil {
				return e
			}
		}
		return nil
	})
}

func (r *ReviewStore) UnregisterInstallationRepositories(ctx context.Context, installationID int64, names []string) error {
	if len(names) == 0 {
		_, e := r.s.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=0,mining_enabled=0,github_installation_id=0,settings_version=settings_version+1,settings_updated_at=? WHERE github_installation_id=?`, nowRFC3339(), installationID)
		return e
	}
	for _, name := range names {
		if _, e := r.s.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=0,mining_enabled=0,github_installation_id=0,settings_version=settings_version+1,settings_updated_at=? WHERE full_name=? AND github_installation_id=?`, nowRFC3339(), strings.TrimSpace(name), installationID); e != nil {
			return e
		}
	}
	return nil
}

func (r *ReviewStore) InstallationID(ctx context.Context, name string) (int64, error) {
	var id int64
	e := r.s.conn().QueryRowContext(ctx, `SELECT github_installation_id FROM repositories WHERE full_name=? AND deleted_at=''`, name).Scan(&id)
	if e != nil {
		return 0, e
	}
	if id <= 0 {
		return 0, fmt.Errorf("repository %s has no active GitHub App installation", name)
	}
	return id, nil
}

func (r *ReviewStore) RepositoryInstalled(ctx context.Context, name string) (bool, error) {
	var id int64
	e := r.s.conn().QueryRowContext(ctx, `SELECT github_installation_id FROM repositories WHERE full_name=? AND deleted_at=''`, name).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return id > 0, e
}

func (r *ReviewStore) SaveRepositorySettings(ctx context.Context, x domain.RepositorySettings, expected int64, actor string) (domain.RepositorySettings, error) {
	x.FullName = strings.TrimSpace(x.FullName)
	if x.FullName == "" || !strings.Contains(x.FullName, "/") {
		return x, errors.New("repository must use owner/name")
	}
	x.Categories = normalizedCategories(x.Categories)
	model, e := normalizeReviewModel(x.ReviewModel, true)
	if e != nil {
		return x, e
	}
	x.ReviewModel = model
	if (x.ReviewEnabled || x.MiningEnabled) && len(x.Categories) == 0 {
		return x, errors.New("an enabled repository needs at least one category")
	}
	e = r.s.withinTx(ctx, func(tx *Database) error {
		var id, version int64
		var deletedAt string
		e := tx.conn().QueryRowContext(ctx, `SELECT id,settings_version,deleted_at FROM repositories WHERE full_name=?`, x.FullName).Scan(&id, &version, &deletedAt)
		if errors.Is(e, sql.ErrNoRows) {
			if expected != 0 {
				return errors.New("repository version conflict")
			}
			res, ie := tx.conn().ExecContext(ctx, `INSERT INTO repositories(full_name,analyzed,actionable,review_enabled,mining_enabled,review_model,settings_version,settings_updated_at) VALUES(?,1,1,?,?,?,1,?)`, x.FullName, boolToInt(x.ReviewEnabled), boolToInt(x.MiningEnabled), x.ReviewModel, nowRFC3339())
			if ie != nil {
				return ie
			}
			id, _ = res.LastInsertId()
			version = 1
		} else if e != nil {
			return e
		} else if deletedAt != "" {
			if expected != 0 {
				return errors.New("repository version conflict")
			}
			version++
			if _, e = tx.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=?,mining_enabled=?,review_model=?,settings_version=?,settings_updated_at=?,deleted_at='' WHERE id=?`, boolToInt(x.ReviewEnabled), boolToInt(x.MiningEnabled), x.ReviewModel, version, nowRFC3339(), id); e != nil {
				return e
			}
			if _, e = tx.conn().ExecContext(ctx, `DELETE FROM repository_categories WHERE repository_id=?`, id); e != nil {
				return e
			}
		} else {
			if version != expected {
				return errors.New("repository version conflict")
			}
			res, ue := tx.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=?,mining_enabled=?,review_model=?,settings_version=settings_version+1,settings_updated_at=? WHERE id=? AND settings_version=?`, boolToInt(x.ReviewEnabled), boolToInt(x.MiningEnabled), x.ReviewModel, nowRFC3339(), id, expected)
			if ue != nil {
				return ue
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return errors.New("repository version conflict")
			}
			version++
			if _, e = tx.conn().ExecContext(ctx, `DELETE FROM repository_categories WHERE repository_id=?`, id); e != nil {
				return e
			}
		}
		for _, c := range x.Categories {
			if _, e = tx.conn().ExecContext(ctx, `INSERT INTO repository_categories(repository_id,category) VALUES(?,?)`, id, c); e != nil {
				return e
			}
		}
		_, e = tx.conn().ExecContext(ctx, `INSERT INTO repository_settings_history(repository_id,version,review_enabled,mining_enabled,categories,actor,changed_at,review_model) VALUES(?,?,?,?,?,?,?,?)`, id, version, boolToInt(x.ReviewEnabled), boolToInt(x.MiningEnabled), marshalStrings(x.Categories), actor, nowRFC3339(), x.ReviewModel)
		return e
	})
	if e != nil {
		return x, e
	}
	out, _, e := r.RepositorySettings(ctx, x.FullName)
	return out, e
}

func (r *ReviewStore) DeleteRepository(ctx context.Context, name string, expected int64) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("repository name is required")
	}
	now := nowRFC3339()
	res, err := r.s.conn().ExecContext(ctx, `UPDATE repositories SET review_enabled=0,mining_enabled=0,github_installation_id=0,settings_version=settings_version+1,settings_updated_at=?,deleted_at=? WHERE full_name=? AND deleted_at='' AND settings_version=?`, now, now, name, expected)
	if err != nil {
		return fmt.Errorf("delete repository %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var exists int
	if err = r.s.conn().QueryRowContext(ctx, `SELECT count(*) FROM repositories WHERE full_name=? AND deleted_at=''`, name).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("repository not found")
	}
	return errors.New("repository version conflict")
}

func normalizeReviewModel(model string, allowEmpty bool) (string, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if allowEmpty && model == "" {
		return "", nil
	}
	if model != "sonnet" && model != "opus" && model != "haiku_max" {
		return "", errors.New("review model must be sonnet, opus, or haiku_max")
	}
	return model, nil
}

func normalizeVerificationModel(model string) (string, error) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "jev" {
		return model, nil
	}
	return normalizeReviewModel(model, false)
}

func (r *ReviewStore) AvailableCategories(ctx context.Context) ([]string, error) {
	rows, e := r.s.conn().QueryContext(ctx, `SELECT DISTINCT upper(language) FROM rules WHERE language<>'' UNION SELECT DISTINCT upper(category) FROM repository_categories ORDER BY 1`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	base := map[string]bool{"COMMON": true, "GO": true, "PHP": true, "SQL": true, "JS": true, "TS": true, "CSS": true}
	for rows.Next() {
		var c string
		if e = rows.Scan(&c); e != nil {
			return nil, e
		}
		base[c] = true
	}
	out := make([]string, 0, len(base))
	for c := range base {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func (r *ReviewStore) repositoryCategories(ctx context.Context, id int64) ([]string, error) {
	rows, e := r.s.conn().QueryContext(ctx, `SELECT category FROM repository_categories WHERE repository_id=? ORDER BY category`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if e = rows.Scan(&c); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func normalizedCategories(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}
func (r *ReviewStore) CreateJob(ctx context.Context, repo string, pr int64, sha string) (domain.ReviewJob, bool, error) {
	var out domain.ReviewJob
	now := nowRFC3339()
	res, e := r.s.conn().ExecContext(ctx, `INSERT OR IGNORE INTO review_jobs(repository_id,pull_request_number,initial_head_sha,status,created_at,updated_at) SELECT id,?,?,?, ?,? FROM repositories WHERE full_name=? AND deleted_at=''`, pr, sha, domain.ReviewQueued, now, now, repo)
	if e != nil {
		return out, false, e
	}
	n, _ := res.RowsAffected()
	out, e = r.Job(ctx, repo, pr)
	return out, n == 1, e
}
func (r *ReviewStore) Job(ctx context.Context, repo string, pr int64) (domain.ReviewJob, error) {
	var j domain.ReviewJob
	var next, lease, created, updated string
	e := r.s.conn().QueryRowContext(ctx, `SELECT j.id,j.repository_id,r.full_name,j.pull_request_number,j.initial_head_sha,j.status,j.attempts,j.next_attempt_at,j.lease_owner,j.lease_until,j.last_error,j.terminal_reason,j.prompt_hash,j.created_at,j.updated_at FROM review_jobs j JOIN repositories r ON r.id=j.repository_id WHERE r.full_name=? AND j.pull_request_number=?`, repo, pr).Scan(&j.ID, &j.RepositoryID, &j.Repository, &j.PullRequestNumber, &j.InitialHeadSHA, &j.Status, &j.Attempts, &next, &j.LeaseOwner, &lease, &j.LastError, &j.TerminalReason, &j.PromptHash, &created, &updated)
	if e != nil {
		return j, e
	}
	j.NextAttemptAt = parseTime(next)
	j.LeaseUntil = parseTime(lease)
	j.CreatedAt = parseTime(created)
	j.UpdatedAt = parseTime(updated)
	return j, nil
}
func (r *ReviewStore) Claim(ctx context.Context, owner string, lease time.Duration) (domain.ReviewJob, bool, error) {
	var j domain.ReviewJob
	e := r.s.withinTx(ctx, func(tx *Database) error {
		now := time.Now().UTC()
		row := tx.conn().QueryRowContext(ctx, `SELECT j.id,r.full_name,j.pull_request_number FROM review_jobs j JOIN repositories r ON r.id=j.repository_id WHERE (j.status=? OR (j.status=? AND (j.next_attempt_at='' OR j.next_attempt_at<=?)) OR (j.status=? AND j.lease_until<?)) ORDER BY j.id LIMIT 1`, domain.ReviewQueued, domain.ReviewRetryWait, now.Format(time.RFC3339), domain.ReviewRunning, now.Format(time.RFC3339))
		if e := row.Scan(&j.ID, &j.Repository, &j.PullRequestNumber); e != nil {
			return e
		}
		res, e := tx.conn().ExecContext(ctx, `UPDATE review_jobs SET status=?,lease_owner=?,lease_until=?,attempts=attempts+1,updated_at=? WHERE id=?`, domain.ReviewRunning, owner, now.Add(lease).Format(time.RFC3339), now.Format(time.RFC3339), j.ID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(e, sql.ErrNoRows) {
		return j, false, nil
	}
	if e != nil {
		return j, false, e
	}
	j, e = r.Job(ctx, j.Repository, j.PullRequestNumber)
	return j, true, e
}
func (r *ReviewStore) SetStatus(ctx context.Context, id int64, status domain.ReviewJobStatus, reason, errText string) error {
	_, e := r.s.conn().ExecContext(ctx, `UPDATE review_jobs SET status=?,terminal_reason=?,last_error=?,lease_owner='',lease_until='',updated_at=? WHERE id=?`, status, reason, errText, nowRFC3339(), id)
	return e
}
func (r *ReviewStore) Retry(ctx context.Context, id int64, errText string, at time.Time) error {
	_, e := r.s.conn().ExecContext(ctx, `UPDATE review_jobs SET status=?,last_error=?,next_attempt_at=?,lease_owner='',lease_until='',updated_at=? WHERE id=?`, domain.ReviewRetryWait, errText, at.Format(time.RFC3339), nowRFC3339(), id)
	return e
}
func (r *ReviewStore) StartAttempt(ctx context.Context, job int64, provider, model, pv string) (int64, error) {
	res, e := r.s.conn().ExecContext(ctx, `INSERT INTO review_attempts(job_id,provider,model,prompt_version,started_at) VALUES(?,?,?,?,?)`, job, provider, model, pv, nowRFC3339())
	if e != nil {
		return 0, e
	}
	return res.LastInsertId()
}
func (r *ReviewStore) FinishAttempt(ctx context.Context, id int64, result, errText string, input, output, cacheCreation, cacheRead, costMicroUSD int64) error {
	_, e := r.s.conn().ExecContext(ctx, `UPDATE review_attempts SET finished_at=?,result=?,error=?,input_tokens=?,output_tokens=?,cache_creation_tokens=?,cache_read_tokens=?,cost_microusd=? WHERE id=?`, nowRFC3339(), result, errText, input, output, cacheCreation, cacheRead, costMicroUSD, id)
	return e
}

func (r *ReviewStore) ListReviewHistory(ctx context.Context, limit int) ([]domain.ReviewHistoryEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, e := r.s.conn().QueryContext(ctx, `SELECT a.id,r.full_name,j.pull_request_number,a.provider,a.model,a.result,a.error,a.started_at,a.finished_at,a.input_tokens,a.output_tokens,a.cache_creation_tokens,a.cache_read_tokens,a.cost_microusd,COUNT(f.id) FROM review_attempts a JOIN review_jobs j ON j.id=a.job_id JOIN repositories r ON r.id=j.repository_id LEFT JOIN review_findings f ON f.attempt_id=a.id GROUP BY a.id ORDER BY a.id DESC LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []domain.ReviewHistoryEntry
	for rows.Next() {
		var x domain.ReviewHistoryEntry
		var started, finished string
		if e = rows.Scan(&x.AttemptID, &x.Repository, &x.PullRequestNumber, &x.Provider, &x.Model, &x.Result, &x.Error, &started, &finished, &x.InputTokens, &x.OutputTokens, &x.CacheCreationTokens, &x.CacheReadTokens, &x.CostMicroUSD, &x.FindingCount); e != nil {
			return nil, e
		}
		x.StartedAt = parseTime(started)
		if t := parseTime(finished); !t.IsZero() {
			x.FinishedAt = &t
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *ReviewStore) DashboardMetrics(ctx context.Context) (domain.DashboardMetrics, error) {
	var out domain.DashboardMetrics
	if e := r.s.conn().QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='active' THEN 1 ELSE 0 END), 0)
		FROM rules`).Scan(&out.TotalRules, &out.ActiveRules); e != nil {
		return out, e
	}

	rows, e := r.s.conn().QueryContext(ctx, `
		WITH RECURSIVE days(day) AS (
			SELECT date('now', '-6 days')
			UNION ALL SELECT date(day, '+1 day') FROM days WHERE day < date('now')
		), completed AS (
			SELECT date(a.finished_at) AS day,
			       COUNT(DISTINCT j.repository_id || ':' || j.pull_request_number) AS reviews,
			       COALESCE(SUM(a.cost_microusd), 0) AS cost
			FROM review_attempts a
			JOIN review_jobs j ON j.id=a.job_id
			WHERE a.result='completed' AND date(a.finished_at) >= date('now', '-6 days')
			GROUP BY date(a.finished_at)
		)
		SELECT days.day, COALESCE(completed.reviews, 0), COALESCE(completed.cost, 0)
		FROM days LEFT JOIN completed ON completed.day=days.day ORDER BY days.day`)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var x domain.DailyReviewMetric
		if e = rows.Scan(&x.Date, &x.ReviewCount, &x.CostMicroUSD); e != nil {
			return out, e
		}
		out.Daily = append(out.Daily, x)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Daily) > 0 {
		last := out.Daily[len(out.Daily)-1]
		out.ReviewsToday, out.CostTodayMicroUSD = last.ReviewCount, last.CostMicroUSD
		var total int
		for _, day := range out.Daily {
			total += day.ReviewCount
		}
		out.AverageReviewsPerDay = float64(total) / float64(len(out.Daily))
	}
	if e = r.s.conn().QueryRowContext(ctx, `
		SELECT COALESCE(CAST(AVG(cost_microusd) AS INTEGER), 0)
		FROM review_attempts
		WHERE result='completed'
		  AND input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens > 0`).Scan(&out.AverageCostPerReviewMicroUSD); e != nil {
		return out, e
	}
	return out, nil
}
func (r *ReviewStore) Rules(ctx context.Context, repo string) ([]ports.ReviewRule, error) {
	rows, e := r.s.conn().QueryContext(ctx, `SELECT ru.rule_id,ru.language,ru.title,ru.body,ru.scope,ru.content_hash,ru.rule_text,ru.exception_text,ru.bad_example,ru.good_example FROM rules ru JOIN repositories rp ON rp.full_name=? AND rp.deleted_at='' JOIN repository_categories rc ON rc.repository_id=rp.id AND rc.category=upper(ru.language) WHERE ru.status='active' AND (NOT EXISTS(SELECT 1 FROM rule_repository_restrictions x WHERE x.rule_id=ru.rule_id) OR EXISTS(SELECT 1 FROM rule_repository_restrictions x WHERE x.rule_id=ru.rule_id AND x.repository_id=rp.id)) ORDER BY ru.rule_id`, repo)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ports.ReviewRule
	for rows.Next() {
		var x ports.ReviewRule
		var scope string
		if e = rows.Scan(&x.RuleID, &x.Category, &x.Title, &x.Body, &scope, &x.ContentHash, &x.Rule, &x.Exception, &x.BadExample, &x.GoodExample); e != nil {
			return nil, e
		}
		if scope != "" {
			x.Scopes = []string{scope}
		}
		x.Body = ""
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *ReviewStore) SaveRuleInputs(ctx context.Context, attempt int64, rules []ports.ReviewRule) error {
	for _, x := range rules {
		sc, _ := json.Marshal(x.Scopes)
		if _, e := r.s.conn().ExecContext(ctx, `INSERT OR IGNORE INTO review_rule_inputs(attempt_id,rule_id,category,title,body,scopes,content_hash,rule_text,exception_text,bad_example,good_example) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, attempt, x.RuleID, x.Category, x.Title, x.Body, string(sc), x.ContentHash, x.Rule, x.Exception, x.BadExample, x.GoodExample); e != nil {
			return e
		}
	}
	return nil
}
func (r *ReviewStore) SaveFinding(ctx context.Context, attempt int64, f domain.ReviewFinding) (int64, error) {
	res, e := r.s.conn().ExecContext(ctx, `INSERT OR IGNORE INTO review_findings(attempt_id,rule_id,kind,path,side,line,confidence,severity,body,suggestion,fingerprint) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, attempt, f.RuleID, f.Kind, f.Path, f.Side, f.Line, f.Confidence, f.Severity, f.Body, f.Suggestion, f.Fingerprint)
	if e != nil {
		return 0, e
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		e = r.s.conn().QueryRowContext(ctx, `SELECT id FROM review_findings WHERE attempt_id=? AND fingerprint=?`, attempt, f.Fingerprint).Scan(&id)
	}
	return id, e
}

func (r *ReviewStore) SaveVerificationDecisions(ctx context.Context, attempt int64, decisions []domain.VerificationDecision) error {
	return r.s.withinTx(ctx, func(tx *Database) error {
		for _, decision := range decisions {
			_, err := tx.conn().ExecContext(ctx, `INSERT INTO review_verification_decisions(attempt_id,candidate_id,verdict,reason,duplicate_of,kind,rule_id,path,line,confidence,severity,body,suggestion) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				attempt, decision.CandidateID, decision.Verdict, decision.Reason, decision.DuplicateOf, decision.Kind, decision.RuleID,
				decision.Path, decision.Line, decision.Confidence, decision.Severity, decision.Body, decision.Suggestion)
			if err != nil {
				return fmt.Errorf("sqlite: save verification decision %q: %w", decision.CandidateID, err)
			}
		}
		return nil
	})
}
func (r *ReviewStore) Published(ctx context.Context, id, githubID int64) error {
	_, e := r.s.conn().ExecContext(ctx, `UPDATE review_findings SET publication_status='published',github_comment_id=? WHERE id=?`, githubID, id)
	return e
}
func (r *ReviewStore) RecordOwnComment(ctx context.Context, job domain.ReviewJob, f domain.ReviewFinding, githubID int64) error {
	return r.s.withinTx(ctx, func(tx *Database) error {
		now := nowRFC3339()
		ids := []string{}
		if f.RuleID != nil {
			ids = append(ids, *f.RuleID)
		}
		raw, _ := json.Marshal(ids)
		res, e := tx.conn().ExecContext(ctx, `INSERT OR IGNORE INTO comments(github_comment_id,repository_id,pull_request_number,author_kind,origin,body,path,rule_ids,created_at,updated_at,ingested_at,input_hash) VALUES(?,?,?,'ai','ai_rule_usage',?,?,?,?,?,?,?)`, githubID, job.RepositoryID, job.PullRequestNumber, f.Body, f.Path, string(raw), now, now, now, f.Fingerprint)
		if e != nil {
			return e
		}
		id, _ := res.LastInsertId()
		if id == 0 {
			e = tx.conn().QueryRowContext(ctx, `SELECT id FROM comments WHERE repository_id=? AND github_comment_id=?`, job.RepositoryID, githubID).Scan(&id)
			if e != nil {
				return e
			}
		}
		if f.RuleID != nil {
			_, e = tx.conn().ExecContext(ctx, `INSERT OR IGNORE INTO rule_usages(rule_id,comment_id,used_at) VALUES(?,?,?)`, *f.RuleID, id, now)
		}
		return e
	})
}

var _ = fmt.Sprintf
