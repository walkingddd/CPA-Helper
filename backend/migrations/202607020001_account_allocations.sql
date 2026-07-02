-- +goose Up
CREATE TABLE IF NOT EXISTS account_pools (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name VARCHAR(160) NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_pools_name
	ON account_pools(name);

CREATE TABLE IF NOT EXISTS account_pool_members (
	pool_id INTEGER NOT NULL,
	auth_name VARCHAR(500) NOT NULL,
	weight INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME NOT NULL,
	PRIMARY KEY(pool_id, auth_name),
	FOREIGN KEY(pool_id) REFERENCES account_pools(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_account_pool_members_auth_name
	ON account_pool_members(auth_name);

CREATE TABLE IF NOT EXISTS user_account_allocations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	scope_type VARCHAR(20) NOT NULL,
	auth_name VARCHAR(500),
	pool_id INTEGER,
	quota_type VARCHAR(20) NOT NULL,
	quota_limit REAL NOT NULL,
	period VARCHAR(20) NOT NULL,
	hard_limit BOOLEAN NOT NULL DEFAULT 0,
	enabled BOOLEAN NOT NULL DEFAULT 1,
	note TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id),
	FOREIGN KEY(pool_id) REFERENCES account_pools(id) ON DELETE CASCADE,
	CHECK(scope_type IN ('auth', 'pool')),
	CHECK(quota_type IN ('requests', 'tokens', 'usd')),
	CHECK(period IN ('daily', 'monthly', 'all_time')),
	CHECK(quota_limit > 0),
	CHECK(
		(scope_type = 'auth' AND auth_name IS NOT NULL AND TRIM(auth_name) <> '' AND pool_id IS NULL)
		OR
		(scope_type = 'pool' AND pool_id IS NOT NULL AND auth_name IS NULL)
	)
);

CREATE INDEX IF NOT EXISTS ix_user_account_allocations_user_id
	ON user_account_allocations(user_id);

CREATE INDEX IF NOT EXISTS ix_user_account_allocations_auth_name
	ON user_account_allocations(auth_name);

CREATE INDEX IF NOT EXISTS ix_user_account_allocations_pool_id
	ON user_account_allocations(pool_id);

CREATE TABLE IF NOT EXISTS allocation_alert_states (
	allocation_id INTEGER NOT NULL,
	period_key VARCHAR(40) NOT NULL,
	level VARCHAR(20) NOT NULL,
	first_triggered_at DATETIME NOT NULL,
	last_triggered_at DATETIME NOT NULL,
	acknowledged_at DATETIME,
	PRIMARY KEY(allocation_id, period_key, level),
	FOREIGN KEY(allocation_id) REFERENCES user_account_allocations(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_allocation_alert_states_last_triggered_at
	ON allocation_alert_states(last_triggered_at);

-- +goose Down
DROP TABLE IF EXISTS allocation_alert_states;
DROP TABLE IF EXISTS user_account_allocations;
DROP TABLE IF EXISTS account_pool_members;
DROP TABLE IF EXISTS account_pools;
