package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log"
	"strings"
	"time"
)

const maxAPIKeyLength = 400

func normalizeCustomAPIKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > maxAPIKeyLength {
		return "", validationError("API KEY 不能超过 400 个字符")
	}
	for _, char := range value {
		if char < '!' || char > '~' {
			return "", validationError("API KEY 只能包含可见 ASCII 字符，不能包含空格或控制字符")
		}
	}
	return value, nil
}

func (a *App) createAPIKeyForUser(ctx context.Context, userID int, username string, payload apiKeyPayload) (UserApiKeySummary, error) {
	if payload.Description == nil || strings.TrimSpace(*payload.Description) == "" {
		return UserApiKeySummary{}, validationError("API KEY 描述不能为空")
	}
	description := strings.TrimSpace(*payload.Description)
	apiKey, err := normalizeCustomAPIKey(payload.APIKey)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	custom := apiKey != ""
	if _, err := a.getActiveUser(ctx, userID); err != nil {
		return UserApiKeySummary{}, err
	}
	if err := a.ensureUserQuotaReadyForKeys(ctx, userID); err != nil {
		return UserApiKeySummary{}, err
	}
	if !custom {
		apiKey, err = a.generateUniqueAPIKey(ctx)
		if err != nil {
			return UserApiKeySummary{}, err
		}
	}
	cfg, err := a.apiKeySyncConfig(ctx)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	apiKeyHash := hashAPIKey(apiKey)
	err = a.withAPIKeyTransaction(ctx, func(tx *sql.Tx) (func(context.Context) error, error) {
		now := dbTime(time.Now())
		result, err := tx.ExecContext(ctx, `
			INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(api_key_hash) DO NOTHING
		`, apiKeyHash, userID, apiKey, description, now, now)
		if err != nil {
			return nil, err
		}
		if affected, err := result.RowsAffected(); err != nil {
			return nil, err
		} else if affected == 0 {
			return nil, conflictError("API KEY 已存在，请使用其他值")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET updated_at = ? WHERE id = ?`, now, userID); err != nil {
			return nil, err
		}
		if custom {
			return a.replaceRemoteAPIKey(ctx, cfg, "", apiKey)
		}
		if err := a.addRemoteAPIKeyWithConfig(ctx, cfg, apiKey); err != nil {
			return nil, err
		}
		return func(rollbackCtx context.Context) error {
			return a.removeRemoteAPIKeyHashWithConfig(rollbackCtx, cfg, apiKeyHash)
		}, nil
	})
	if err != nil {
		return UserApiKeySummary{}, err
	}
	summary, err := a.keySummaryByHash(ctx, apiKeyHash, &apiKey)
	summary.UserName = &username
	return summary, err
}

func (a *App) updateCurrentUserAPIKey(ctx context.Context, user *AuthUser, apiKeyHash string, payload apiKeyPayload) (UserApiKeySummary, error) {
	existing, err := a.getAPIKey(ctx, apiKeyHash)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	if existing.UserID != user.ID {
		return UserApiKeySummary{}, notFoundError("API KEY 不存在")
	}
	if payload.Description != nil && strings.TrimSpace(*payload.Description) == "" {
		return UserApiKeySummary{}, validationError("API KEY 描述不能为空")
	}
	apiKey, err := normalizeCustomAPIKey(payload.APIKey)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	nextHash := apiKeyHash
	if apiKey != "" {
		nextHash = hashAPIKey(apiKey)
	}
	changingKey := nextHash != apiKeyHash
	var cfg AppConfig
	if changingKey {
		if err := a.ensureUserQuotaReadyForKeys(ctx, user.ID); err != nil {
			return UserApiKeySummary{}, err
		}
		cfg, err = a.apiKeySyncConfig(ctx)
		if err != nil {
			return UserApiKeySummary{}, err
		}
	}
	err = a.withAPIKeyTransaction(ctx, func(tx *sql.Tx) (func(context.Context) error, error) {
		var description string
		err := tx.QueryRowContext(ctx, `SELECT description FROM user_api_keys WHERE user_id = ? AND api_key_hash = ?`, user.ID, apiKeyHash).Scan(&description)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, notFoundError("API KEY 不存在")
		}
		if err != nil {
			return nil, err
		}
		if payload.Description != nil {
			description = strings.TrimSpace(*payload.Description)
		}
		now := dbTime(time.Now())
		if changingKey {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE api_key_hash = ?`, nextHash).Scan(&count); err != nil {
				return nil, err
			}
			if count != 0 {
				return nil, conflictError("API KEY 已存在，请使用其他值")
			}
			_, err = tx.ExecContext(ctx, `UPDATE user_api_keys SET api_key_hash = ?, api_key = ?, description = ?, updated_at = ? WHERE user_id = ? AND api_key_hash = ?`, nextHash, apiKey, description, now, user.ID, apiKeyHash)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE user_api_keys SET description = ?, updated_at = ? WHERE user_id = ? AND api_key_hash = ?`, description, now, user.ID, apiKeyHash)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET updated_at = ? WHERE id = ?`, now, user.ID); err != nil {
			return nil, err
		}
		if changingKey {
			return a.replaceRemoteAPIKey(ctx, cfg, apiKeyHash, apiKey)
		}
		return nil, nil
	})
	if err != nil {
		return UserApiKeySummary{}, err
	}
	summary, err := a.keySummaryByHash(ctx, nextHash, nil)
	name := user.Username
	summary.UserName = &name
	return summary, err
}

// Hold the single SQLite connection through sync and compensation so another
// local mutation cannot race a rollback. Callbacks must use tx, not a.db.
func (a *App) withAPIKeyTransaction(ctx context.Context, mutate func(*sql.Tx) (func(context.Context) error, error)) error {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	undoRemote, err := mutate(tx)
	if err == nil {
		err = tx.Commit()
		if err != nil {
			// SQLite can leave a transaction open after a failed COMMIT, while
			// sql.Tx is already done. Discard that connection after compensation.
			defer conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}
	if err == nil {
		return nil
	}
	_ = tx.Rollback()
	if undoRemote != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiKeySyncTimeout)
		defer cancel()
		if rollbackErr := undoRemote(rollbackCtx); rollbackErr != nil {
			log.Printf("API key sync rollback failed: %v", rollbackErr)
			return validationError("API KEY 保存失败，且 CPA 回滚失败，请联系管理员检查密钥配置")
		}
	}
	return err
}
