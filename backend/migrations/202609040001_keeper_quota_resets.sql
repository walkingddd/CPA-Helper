-- +goose Up
CREATE TABLE IF NOT EXISTS codex_keeper_quota_resets (
	auth_name VARCHAR(500) PRIMARY KEY,
	reset_count INTEGER NOT NULL DEFAULT 0,
	last_reset_at TIMESTAMP NULL
);

-- +goose Down
DROP TABLE IF EXISTS codex_keeper_quota_resets;
