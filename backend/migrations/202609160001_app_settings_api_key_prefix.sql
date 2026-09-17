-- +goose Up
-- API keys minted by CPA-Helper carry a configurable prefix (e.g. `sk-myteam-...`). The value
-- stored here is the prefix WITHOUT the joining dash; an empty value means the default `sk`,
-- so every existing deployment keeps generating `sk-...` keys exactly as before. Only NEW keys
-- are affected — existing keys are never rewritten.
ALTER TABLE app_settings ADD COLUMN api_key_prefix TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE app_settings DROP COLUMN api_key_prefix;
