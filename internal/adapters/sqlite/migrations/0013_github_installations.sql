ALTER TABLE repositories ADD COLUMN github_installation_id INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_repositories_github_installation_id
ON repositories(github_installation_id);
