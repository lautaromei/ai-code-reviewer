ALTER TABLE review_model_settings ADD COLUMN generation_agents INTEGER NOT NULL DEFAULT 1 CHECK(generation_agents BETWEEN 1 AND 10);
