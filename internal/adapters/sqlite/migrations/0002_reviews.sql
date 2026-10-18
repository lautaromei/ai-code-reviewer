CREATE TABLE repository_categories (
 repository_id INTEGER NOT NULL REFERENCES repositories(id), category TEXT NOT NULL,
 PRIMARY KEY(repository_id, category)
);
CREATE TABLE rule_sources (
 id INTEGER PRIMARY KEY AUTOINCREMENT, rule_id TEXT NOT NULL REFERENCES rules(rule_id),
 repository_id INTEGER REFERENCES repositories(id), source_path TEXT NOT NULL DEFAULT '',
 source_ref TEXT NOT NULL DEFAULT '', source_rule_id TEXT NOT NULL DEFAULT '', source_hash TEXT NOT NULL DEFAULT '',
 UNIQUE(repository_id, source_path, source_ref, source_rule_id)
);
CREATE TABLE rule_repository_restrictions (
 rule_id TEXT NOT NULL REFERENCES rules(rule_id), repository_id INTEGER NOT NULL REFERENCES repositories(id),
 PRIMARY KEY(rule_id, repository_id)
);
CREATE TABLE rule_scopes (
 rule_id TEXT NOT NULL REFERENCES rules(rule_id), glob TEXT NOT NULL, PRIMARY KEY(rule_id, glob)
);
CREATE TABLE webhook_deliveries (
 delivery_id TEXT PRIMARY KEY, event_type TEXT NOT NULL, action TEXT NOT NULL DEFAULT '', repository TEXT NOT NULL DEFAULT '',
 received_at TEXT NOT NULL, processed_at TEXT NOT NULL DEFAULT '', result TEXT NOT NULL DEFAULT ''
);
CREATE TABLE review_prompt_revisions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, scope TEXT NOT NULL, repository TEXT NOT NULL DEFAULT '', text TEXT NOT NULL,
 hash TEXT NOT NULL, origin TEXT NOT NULL DEFAULT '', author TEXT NOT NULL DEFAULT '', previous_revision_id INTEGER REFERENCES review_prompt_revisions(id),
 created_at TEXT NOT NULL
);
CREATE TABLE review_prompt_defaults (
 repository TEXT PRIMARY KEY, adapted_revision_id INTEGER NOT NULL REFERENCES review_prompt_revisions(id),
 source_path TEXT NOT NULL DEFAULT '', source_commit TEXT NOT NULL DEFAULT '', source_hash TEXT NOT NULL DEFAULT '', original_text TEXT NOT NULL DEFAULT ''
);
CREATE TABLE review_prompt_bindings (
 scope TEXT NOT NULL, repository TEXT NOT NULL DEFAULT '', revision_id INTEGER REFERENCES review_prompt_revisions(id),
 use_global INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1, PRIMARY KEY(scope, repository)
);
CREATE TABLE review_jobs (
 id INTEGER PRIMARY KEY AUTOINCREMENT, repository_id INTEGER NOT NULL REFERENCES repositories(id), pull_request_number INTEGER NOT NULL,
 initial_head_sha TEXT NOT NULL, status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL DEFAULT '',
 lease_owner TEXT NOT NULL DEFAULT '', lease_until TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '', terminal_reason TEXT NOT NULL DEFAULT '',
 prompt_revision_id INTEGER REFERENCES review_prompt_revisions(id), prompt_hash TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(repository_id, pull_request_number)
);
CREATE INDEX idx_review_jobs_claim ON review_jobs(status, next_attempt_at, lease_until);
CREATE TABLE review_attempts (
 id INTEGER PRIMARY KEY AUTOINCREMENT, job_id INTEGER NOT NULL REFERENCES review_jobs(id), provider TEXT NOT NULL, model TEXT NOT NULL,
 prompt_version TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '', input_tokens INTEGER NOT NULL DEFAULT 0,
 output_tokens INTEGER NOT NULL DEFAULT 0, result TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE review_rule_inputs (
 attempt_id INTEGER NOT NULL REFERENCES review_attempts(id), rule_id TEXT NOT NULL, category TEXT NOT NULL, title TEXT NOT NULL,
 body TEXT NOT NULL, scopes TEXT NOT NULL DEFAULT '[]', content_hash TEXT NOT NULL, PRIMARY KEY(attempt_id, rule_id)
);
CREATE TABLE review_findings (
 id INTEGER PRIMARY KEY AUTOINCREMENT, attempt_id INTEGER NOT NULL REFERENCES review_attempts(id), rule_id TEXT REFERENCES rules(rule_id),
 kind TEXT NOT NULL, path TEXT NOT NULL DEFAULT '', side TEXT NOT NULL DEFAULT '', line INTEGER NOT NULL DEFAULT 0,
 confidence INTEGER NOT NULL DEFAULT 0, severity TEXT NOT NULL DEFAULT '', body TEXT NOT NULL, suggestion TEXT NOT NULL DEFAULT '',
 fingerprint TEXT NOT NULL, publication_status TEXT NOT NULL DEFAULT 'pending', github_comment_id INTEGER NOT NULL DEFAULT 0,
 UNIQUE(attempt_id, fingerprint)
);
CREATE TABLE maintenance_leases (
 name TEXT PRIMARY KEY, owner TEXT NOT NULL, lease_until TEXT NOT NULL, updated_at TEXT NOT NULL
);
