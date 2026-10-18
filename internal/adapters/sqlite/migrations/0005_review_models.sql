ALTER TABLE repositories ADD COLUMN review_model TEXT NOT NULL DEFAULT '';
ALTER TABLE repository_settings_history ADD COLUMN review_model TEXT NOT NULL DEFAULT '';

CREATE TABLE review_model_settings (
 singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
 global_model TEXT NOT NULL,
 version INTEGER NOT NULL DEFAULT 1,
 actor TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT ''
);
