
CREATE TABLE IF NOT EXISTS repositories (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    full_name      TEXT    NOT NULL UNIQUE,
    analyzed       INTEGER NOT NULL DEFAULT 0,
    actionable     INTEGER NOT NULL DEFAULT 0,
    rules_path     TEXT    NOT NULL DEFAULT '',
    languages      TEXT    NOT NULL DEFAULT '[]',
    languages_at   TEXT    NOT NULL DEFAULT '',
    default_branch TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS sync_cursors (
    repository_id   INTEGER NOT NULL REFERENCES repositories(id),
    stream          TEXT    NOT NULL,
    high_watermark  TEXT    NOT NULL DEFAULT '',
    last_comment_id INTEGER NOT NULL DEFAULT 0,
    etag            TEXT    NOT NULL DEFAULT '',
    last_success_at TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (repository_id, stream)
);

CREATE TABLE IF NOT EXISTS runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    command     TEXT    NOT NULL,
    config_hash TEXT    NOT NULL DEFAULT '',
    status      TEXT    NOT NULL,
    started_at  TEXT    NOT NULL,
    finished_at TEXT    NOT NULL DEFAULT '',
    summary     TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS comments (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    github_comment_id   INTEGER NOT NULL,
    repository_id       INTEGER NOT NULL REFERENCES repositories(id),
    pull_request_number INTEGER NOT NULL DEFAULT 0,
    review_id           INTEGER NOT NULL DEFAULT 0,
    parent_comment_id   INTEGER,
    author               TEXT   NOT NULL DEFAULT '',
    author_kind          TEXT   NOT NULL DEFAULT '',
    origin               TEXT   NOT NULL DEFAULT '',
    body                 TEXT   NOT NULL DEFAULT '',
    path                 TEXT   NOT NULL DEFAULT '',
    diff_hunk            TEXT   NOT NULL DEFAULT '',
    language             TEXT   NOT NULL DEFAULT '',
    url                  TEXT   NOT NULL DEFAULT '',
    rule_ids             TEXT   NOT NULL DEFAULT '[]',
    created_at           TEXT   NOT NULL DEFAULT '',
    updated_at           TEXT   NOT NULL DEFAULT '',
    ingested_at          TEXT   NOT NULL DEFAULT '',
    input_hash           TEXT   NOT NULL DEFAULT '',
    UNIQUE (repository_id, github_comment_id)
);

CREATE INDEX IF NOT EXISTS idx_comments_repository_id ON comments(repository_id);

CREATE TABLE IF NOT EXISTS reactions (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    github_reaction_id INTEGER NOT NULL UNIQUE,
    comment_id         INTEGER NOT NULL REFERENCES comments(id),
    user               TEXT    NOT NULL DEFAULT '',
    content             TEXT   NOT NULL DEFAULT '',
    created_at          TEXT   NOT NULL DEFAULT '',
    last_seen_at        TEXT   NOT NULL DEFAULT '',
    UNIQUE (comment_id, user, content)
);

CREATE INDEX IF NOT EXISTS idx_reactions_comment_id ON reactions(comment_id);

CREATE TABLE IF NOT EXISTS rules (
    rule_id         TEXT PRIMARY KEY,
    language        TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    scope           TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT '',
    activated_at    TEXT NOT NULL DEFAULT '',
    last_used_at    TEXT NOT NULL DEFAULT '',
    disabled_at     TEXT NOT NULL DEFAULT '',
    disabled_reason TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT '',
    content_hash    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_rules_status ON rules(status);
CREATE INDEX IF NOT EXISTS idx_rules_language ON rules(language);

CREATE TABLE IF NOT EXISTS rule_usages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id    TEXT    NOT NULL REFERENCES rules(rule_id),
    comment_id INTEGER NOT NULL REFERENCES comments(id),
    used_at    TEXT    NOT NULL DEFAULT '',
    UNIQUE (rule_id, comment_id)
);

CREATE TABLE IF NOT EXISTS rule_status_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_key   TEXT    NOT NULL UNIQUE,
    rule_id     TEXT    NOT NULL REFERENCES rules(rule_id),
    from_state  TEXT    NOT NULL DEFAULT '',
    to_state    TEXT    NOT NULL,
    reason      TEXT    NOT NULL DEFAULT '',
    run_id      INTEGER NOT NULL DEFAULT 0,
    occurred_at TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS candidate_clusters (
    id                 TEXT PRIMARY KEY,
    language           TEXT NOT NULL,
    label              TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL,
    merged_into        TEXT NOT NULL DEFAULT '',
    promoted_rule_id   TEXT NOT NULL DEFAULT '',
    classifier_version TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL DEFAULT '',
    updated_at         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_candidate_clusters_status_language ON candidate_clusters(status, language);

CREATE TABLE IF NOT EXISTS candidate_evidence (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id TEXT    NOT NULL REFERENCES candidate_clusters(id),
    comment_id   INTEGER NOT NULL REFERENCES comments(id),
    kind         TEXT    NOT NULL,
    thumbs_up    INTEGER NOT NULL DEFAULT 0,
    thumbs_down  INTEGER NOT NULL DEFAULT 0,
    evaluated_at TEXT    NOT NULL DEFAULT '',
    UNIQUE (candidate_id, comment_id)
);

CREATE TABLE IF NOT EXISTS classifications (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    comment_id     INTEGER NOT NULL REFERENCES comments(id),
    input_hash     TEXT    NOT NULL,
    prompt_version TEXT    NOT NULL,
    model          TEXT    NOT NULL DEFAULT '',
    verdict        TEXT    NOT NULL,
    raw_payload    TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL DEFAULT '',
    UNIQUE (comment_id, input_hash, prompt_version)
);

CREATE TABLE IF NOT EXISTS snapshots (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    hash             TEXT    NOT NULL UNIQUE,
    run_id           INTEGER NOT NULL DEFAULT 0,
    renderer_version TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS snapshot_rules (
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id),
    rule_id     TEXT    NOT NULL REFERENCES rules(rule_id),
    position    INTEGER NOT NULL,
    PRIMARY KEY (snapshot_id, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_snapshot_rules_snapshot_position ON snapshot_rules(snapshot_id, position);

CREATE TABLE IF NOT EXISTS publications (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id            INTEGER NOT NULL REFERENCES snapshots(id),
    repository_id          INTEGER NOT NULL REFERENCES repositories(id),
    filtered_snapshot_hash TEXT    NOT NULL,
    branch                 TEXT    NOT NULL DEFAULT '',
    commit_sha             TEXT    NOT NULL DEFAULT '',
    pull_request_url       TEXT    NOT NULL DEFAULT '',
    status                 TEXT    NOT NULL,
    last_error             TEXT    NOT NULL DEFAULT '',
    created_at             TEXT    NOT NULL DEFAULT '',
    UNIQUE (repository_id, filtered_snapshot_hash)
);

CREATE TABLE IF NOT EXISTS rule_sequences (
    language   TEXT PRIMARY KEY,
    next_value INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
