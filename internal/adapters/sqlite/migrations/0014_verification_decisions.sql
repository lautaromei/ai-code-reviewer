CREATE TABLE review_verification_decisions (
 attempt_id INTEGER NOT NULL REFERENCES review_attempts(id), candidate_id TEXT NOT NULL,
 verdict TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', duplicate_of TEXT NOT NULL DEFAULT '',
 kind TEXT NOT NULL, rule_id TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', line INTEGER NOT NULL DEFAULT 0,
 confidence INTEGER NOT NULL DEFAULT 0, severity TEXT NOT NULL DEFAULT '', body TEXT NOT NULL, suggestion TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(attempt_id, candidate_id)
);
