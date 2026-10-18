ALTER TABLE repositories ADD COLUMN review_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE repositories ADD COLUMN mining_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE repositories ADD COLUMN settings_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE repositories ADD COLUMN settings_updated_at TEXT NOT NULL DEFAULT '';

CREATE TABLE repository_settings_history (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 repository_id INTEGER NOT NULL REFERENCES repositories(id),
 version INTEGER NOT NULL,
 review_enabled INTEGER NOT NULL,
 mining_enabled INTEGER NOT NULL,
 categories TEXT NOT NULL,
 actor TEXT NOT NULL DEFAULT '',
 changed_at TEXT NOT NULL
);
