ALTER TABLE review_attempts ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_attempts ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_attempts ADD COLUMN cost_microusd INTEGER NOT NULL DEFAULT 0;
