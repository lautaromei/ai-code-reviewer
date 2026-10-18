ALTER TABLE review_attempts ADD COLUMN verification_model TEXT NOT NULL DEFAULT '';
ALTER TABLE review_attempts ADD COLUMN verification_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_attempts ADD COLUMN verification_output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_attempts ADD COLUMN verification_cache_creation_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_attempts ADD COLUMN verification_cache_read_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_attempts ADD COLUMN verification_cost_microusd INTEGER NOT NULL DEFAULT 0;
