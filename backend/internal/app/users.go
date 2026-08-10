package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	generatedAPIKeyPrefix     = "sk-"
	generatedAPIKeyLength     = 52
	generatedAPIKeyAlphabet   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	maxAPIKeyLengthBytes      = 400
	maxAPIKeyDescriptionRunes = 240
)

type userPayload struct {
	Username string  `json:"username"`
	Password *string `json:"password"`
	IsAdmin  bool    `json:"is_admin"`
	Nickname string  `json:"nickname"`
}

type userAPIKeyBindPayload struct {
	APIKey      *string `json:"api_key"`
	APIKeyHash  *string `json:"api_key_hash"`
	Description string  `json:"description"`
}

type optionalAPIKeyValue struct {
	Value string
	Set   bool
}

func (value *optionalAPIKeyValue) UnmarshalJSON(data []byte) error {
	value.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		value.Value = ""
		return nil
	}
	return json.Unmarshal(data, &value.Value)
}

type apiKeyPayload struct {
	Description string `json:"description"`
}

type apiKeyCreatePayload struct {
	Description string              `json:"description"`
	APIKey      optionalAPIKeyValue `json:"api_key"`
}

type UserRecord struct {
	ID                   int
	Username             string
	IsAdmin              bool
	Nickname             string
	DisabledAt           *time.Time
	PasswordHash         *string
	PasswordSalt         *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	QuotaLifetimeUSD     *float64
	QuotaMonthlyUSD      *float64
	QuotaStartedAt       *time.Time
	QuotaMonth           string
	QuotaMonthUsedUSD    float64
	QuotaPausedAt        *time.Time
	QuotaPauseReason     *string
	QuotaSyncError       *string
	QuotaUnpricedRecords int
}

type UserAPIKey struct {
	APIKeyHash  string
	UserID      int
	APIKey      *string
	Description string
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

type UserApiKeySummary struct {
	APIKeyHash            string     `json:"api_key_hash"`
	APIKey                *string    `json:"api_key"`
	Description           string     `json:"description"`
	UserID                *int       `json:"user_id"`
	UserName              *string    `json:"user_name"`
	CreatedAt             *time.Time `json:"created_at"`
	UpdatedAt             *time.Time `json:"updated_at"`
	Records               int        `json:"records"`
	SuccessRecords        int        `json:"success_records"`
	FailedRecords         int        `json:"failed_records"`
	TotalTokens           int        `json:"total_tokens"`
	TodayRecords          int        `json:"today_records"`
	TodaySuccessRecords   int        `json:"today_success_records"`
	TodayFailedRecords    int        `json:"today_failed_records"`
	TodayInputTokens      int        `json:"today_input_tokens"`
	TodayOutputTokens     int        `json:"today_output_tokens"`
	TodayCachedTokens     int        `json:"today_cached_tokens"`
	TodayReasoningTokens  int        `json:"today_reasoning_tokens"`
	TodayTotalTokens      int        `json:"today_total_tokens"`
	TodayEstimatedCostUSD float64    `json:"today_estimated_cost_usd"`
	TodayUnpricedRecords  int        `json:"today_unpriced_records"`
	FirstSeenAt           *time.Time `json:"first_seen_at"`
	LastSeenAt            *time.Time `json:"last_seen_at"`
	LastProvider          *string    `json:"last_provider"`
	LastModel             *string    `json:"last_model"`
	Providers             []string   `json:"providers"`
	Models                []string   `json:"models"`
}

type UserSummaryResponse struct {
	ID                    int                     `json:"id"`
	Username              string                  `json:"username"`
	IsAdmin               bool                    `json:"is_admin"`
	Nickname              string                  `json:"nickname"`
	DisabledAt            *time.Time              `json:"disabled_at"`
	PasswordSet           bool                    `json:"password_set"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
	APIKeys               []UserApiKeySummary     `json:"api_keys"`
	KeyCount              int                     `json:"key_count"`
	Records               int                     `json:"records"`
	SuccessRecords        int                     `json:"success_records"`
	FailedRecords         int                     `json:"failed_records"`
	TotalTokens           int                     `json:"total_tokens"`
	TodayRecords          int                     `json:"today_records"`
	TodaySuccessRecords   int                     `json:"today_success_records"`
	TodayFailedRecords    int                     `json:"today_failed_records"`
	TodayInputTokens      int                     `json:"today_input_tokens"`
	TodayOutputTokens     int                     `json:"today_output_tokens"`
	TodayCachedTokens     int                     `json:"today_cached_tokens"`
	TodayReasoningTokens  int                     `json:"today_reasoning_tokens"`
	TodayTotalTokens      int                     `json:"today_total_tokens"`
	TodayEstimatedCostUSD float64                 `json:"today_estimated_cost_usd"`
	TodayUnpricedRecords  int                     `json:"today_unpriced_records"`
	FirstSeenAt           *time.Time              `json:"first_seen_at"`
	LastSeenAt            *time.Time              `json:"last_seen_at"`
	LastProvider          *string                 `json:"last_provider"`
	LastModel             *string                 `json:"last_model"`
	Providers             []string                `json:"providers"`
	Models                []string                `json:"models"`
	Quota                 UserQuotaStatusResponse `json:"quota"`
}

func (a *App) handleUsers(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	switch r.Method {
	case http.MethodGet:
		users, err := a.listUsers(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, users)
		return nil
	case http.MethodPost:
		var payload userPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		user, err := a.createUser(r.Context(), payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, user)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleUserByPath(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	parts := splitPath(r.URL.Path, "/api/users/")
	if len(parts) == 1 && parts[0] == "observed-api-keys" {
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		keys, err := a.keySummaries(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, keys)
		return nil
	}
	if len(parts) < 1 {
		return notFoundError("Not Found")
	}
	userID, err := parseIntPath(parts[0])
	if err != nil {
		return err
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			var payload userPayload
			if err := decodeJSON(r, &payload); err != nil {
				return err
			}
			user, err := a.updateUser(r.Context(), userID, payload)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, user)
			return nil
		case http.MethodDelete:
			if err := a.disableUser(r.Context(), userID); err != nil {
				return err
			}
			writeNoContent(w)
			return nil
		default:
			return methodNotAllowed()
		}
	}
	if len(parts) == 2 && parts[1] == "disable" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		if err := a.disableUser(r.Context(), userID); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	}
	if len(parts) == 2 && parts[1] == "enable" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		if err := a.enableUser(r.Context(), userID); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	}
	if len(parts) == 2 && parts[1] == "quota" {
		if err := requireMethod(r, http.MethodPut); err != nil {
			return err
		}
		var payload userQuotaPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		status, err := a.updateUserQuota(r.Context(), userID, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, status)
		return nil
	}
	if len(parts) == 2 && parts[1] == "api-keys" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload userAPIKeyBindPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		summary, err := a.bindUserAPIKey(r.Context(), userID, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, summary)
		return nil
	}
	if len(parts) == 3 && parts[1] == "api-keys" && parts[2] == "create" {
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload apiKeyCreatePayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		summary, err := a.createAPIKeyForUser(r.Context(), userID, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, summary)
		return nil
	}
	if len(parts) == 3 && parts[1] == "api-keys" {
		if err := requireMethod(r, http.MethodDelete); err != nil {
			return err
		}
		if err := a.unbindUserAPIKey(r.Context(), userID, parts[2]); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	}
	return notFoundError("Not Found")
}

func (a *App) handleCurrentUserAPIKeys(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := a.currentUserAPIKeys(r.Context(), user)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, keys)
		return nil
	case http.MethodPost:
		var payload apiKeyCreatePayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		summary, err := a.createAPIKeyForUser(r.Context(), user.ID, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, summary)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleCurrentUserAPIKeyByHash(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	apiKeyHash := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/api-keys/"), "/")
	if apiKeyHash == "" {
		return notFoundError("API KEY 不存在")
	}
	switch r.Method {
	case http.MethodPut:
		var payload apiKeyPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		summary, err := a.updateCurrentUserAPIKey(r.Context(), user, apiKeyHash, payload.Description)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, summary)
		return nil
	case http.MethodDelete:
		if err := a.deleteCurrentUserAPIKey(r.Context(), user, apiKeyHash); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) listUsers(ctx context.Context) ([]UserSummaryResponse, error) {
	if err := a.ensureUsersInitialized(ctx); err != nil {
		return nil, err
	}
	users, err := a.allUsers(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := a.keySummaries(ctx)
	if err != nil {
		return nil, err
	}
	usage, err := a.userUsageSummaries(ctx)
	if err != nil {
		return nil, err
	}
	keysByUser := map[int][]UserApiKeySummary{}
	for _, key := range keys {
		if key.UserID != nil {
			keysByUser[*key.UserID] = append(keysByUser[*key.UserID], key)
		}
	}
	responses := make([]UserSummaryResponse, 0, len(users))
	for _, user := range users {
		quota, err := a.userQuotaStatus(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		responses = append(responses, userSummaryResponse(user, keysByUser[user.ID], usage[user.Username], quota))
	}
	sort.Slice(responses, func(i, j int) bool { return responses[i].ID < responses[j].ID })
	return responses, nil
}

func (a *App) createUser(ctx context.Context, payload userPayload) (UserSummaryResponse, error) {
	if err := a.ensureUsersInitialized(ctx); err != nil {
		return UserSummaryResponse{}, err
	}
	username := strings.TrimSpace(payload.Username)
	nickname := strings.TrimSpace(payload.Nickname)
	if username == "" || nickname == "" {
		return UserSummaryResponse{}, validationError("账号和昵称不能为空")
	}
	if payload.Password == nil || strings.TrimSpace(*payload.Password) == "" {
		return UserSummaryResponse{}, validationError("密码不能为空")
	}
	if err := a.ensureUsernameAvailable(ctx, username, nil); err != nil {
		return UserSummaryResponse{}, err
	}
	salt, err := createSalt()
	if err != nil {
		return UserSummaryResponse{}, err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, password_salt, is_admin, nickname, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, username, hashPassword(*payload.Password, salt), salt, payload.IsAdmin, nickname, now, now)
	if err != nil {
		return UserSummaryResponse{}, err
	}
	id, _ := result.LastInsertId()
	user, err := a.getUser(ctx, int(id))
	if err != nil {
		return UserSummaryResponse{}, err
	}
	return userSummaryResponse(user, nil, emptyUserUsageSummary(), quotaStatusFromUser(user)), nil
}

func (a *App) updateUser(ctx context.Context, id int, payload userPayload) (UserSummaryResponse, error) {
	user, err := a.getUser(ctx, id)
	if err != nil {
		return UserSummaryResponse{}, err
	}
	username := strings.TrimSpace(payload.Username)
	if username != user.Username {
		return UserSummaryResponse{}, conflictError("账号不允许修改")
	}
	firstID, err := a.firstActiveUserID(ctx)
	if err != nil {
		return UserSummaryResponse{}, err
	}
	if firstID != nil && user.ID == *firstID && !payload.IsAdmin {
		return UserSummaryResponse{}, conflictError("第一个管理员账号不能取消管理员权限")
	}
	nickname := strings.TrimSpace(payload.Nickname)
	if nickname == "" {
		return UserSummaryResponse{}, validationError("昵称不能为空")
	}
	if payload.Password != nil && strings.TrimSpace(*payload.Password) != "" {
		salt, err := createSalt()
		if err != nil {
			return UserSummaryResponse{}, err
		}
		_, err = a.db.ExecContext(ctx, `UPDATE users SET nickname = ?, is_admin = ?, password_hash = ?, password_salt = ?, updated_at = ? WHERE id = ?`, nickname, payload.IsAdmin, hashPassword(*payload.Password, salt), salt, dbTime(time.Now()), id)
		if err != nil {
			return UserSummaryResponse{}, err
		}
	} else {
		_, err = a.db.ExecContext(ctx, `UPDATE users SET nickname = ?, is_admin = ?, updated_at = ? WHERE id = ?`, nickname, payload.IsAdmin, dbTime(time.Now()), id)
		if err != nil {
			return UserSummaryResponse{}, err
		}
	}
	users, err := a.listUsers(ctx)
	if err != nil {
		return UserSummaryResponse{}, err
	}
	for _, item := range users {
		if item.ID == id {
			return item, nil
		}
	}
	return UserSummaryResponse{}, notFoundError("用户不存在")
}

func (a *App) disableUser(ctx context.Context, id int) error {
	user, err := a.getUser(ctx, id)
	if err != nil {
		return err
	}
	if user.ID == 1 {
		return conflictError("第一个用户不能禁用")
	}
	keys, err := a.userAPIKeys(ctx, id)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := a.removeRemoteAPIKeyHash(ctx, key.APIKeyHash); err != nil {
			return err
		}
	}
	_, err = a.db.ExecContext(ctx, `UPDATE users SET disabled_at = ?, updated_at = ? WHERE id = ?`, dbTime(time.Now()), dbTime(time.Now()), id)
	return err
}

func (a *App) enableUser(ctx context.Context, id int) error {
	user, err := a.getUser(ctx, id)
	if err != nil {
		return err
	}
	if user.DisabledAt == nil {
		return nil
	}
	user, err = a.ensureQuotaMonth(ctx, user)
	if err != nil {
		return err
	}
	if user.QuotaPausedAt != nil && !quotaHasAvailable(user) {
		return conflictError("用户额度已用尽，请补充额度后再恢复 API KEY")
	}
	keys, err := a.userAPIKeys(ctx, id)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key.APIKey == nil {
			return conflictError("存在无法恢复的 API KEY，请重新绑定后再启用")
		}
	}
	restored := []string{}
	for _, key := range keys {
		if key.APIKey == nil {
			continue
		}
		if err := a.addRemoteAPIKey(ctx, *key.APIKey); err != nil {
			for _, hash := range restored {
				_ = a.removeRemoteAPIKeyHash(ctx, hash)
			}
			return err
		}
		restored = append(restored, key.APIKeyHash)
	}
	_, err = a.db.ExecContext(ctx, `UPDATE users SET disabled_at = NULL, quota_paused_at = NULL, quota_pause_reason = NULL, quota_sync_error = NULL, updated_at = ? WHERE id = ?`, dbTime(time.Now()), id)
	return err
}

func (a *App) bindUserAPIKey(ctx context.Context, userID int, payload userAPIKeyBindPayload) (UserApiKeySummary, error) {
	user, err := a.getActiveUser(ctx, userID)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	if err := a.ensureUserQuotaReadyForKeys(ctx, user.ID); err != nil {
		return UserApiKeySummary{}, err
	}
	description := strings.TrimSpace(payload.Description)
	if description == "" {
		return UserApiKeySummary{}, validationError("API KEY 描述不能为空")
	}
	var apiKeyHash string
	if payload.APIKeyHash != nil {
		apiKeyHash = strings.TrimSpace(*payload.APIKeyHash)
	}
	var apiKey *string
	if payload.APIKey != nil {
		normalized := strings.TrimSpace(*payload.APIKey)
		if normalized == "" {
			return UserApiKeySummary{}, validationError("API KEY 不能为空")
		}
		apiKey = &normalized
		calculated := hashAPIKey(normalized)
		if apiKeyHash != "" && calculated != apiKeyHash {
			return UserApiKeySummary{}, conflictError("API KEY 与 API KEY 标识不匹配")
		}
		apiKeyHash = calculated
	}
	if apiKeyHash == "" {
		return UserApiKeySummary{}, validationError("API KEY 或 API KEY 标识不能为空")
	}
	if apiKey == nil {
		existing, err := a.getAPIKey(ctx, apiKeyHash)
		if err != nil {
			return UserApiKeySummary{}, notFoundError("未找到完整 API KEY，请粘贴原始 API KEY")
		}
		apiKey = existing.APIKey
	}
	if apiKey == nil {
		return UserApiKeySummary{}, notFoundError("未找到完整 API KEY，请粘贴原始 API KEY")
	}
	if err := a.upsertUserAPIKey(ctx, user.ID, apiKeyHash, *apiKey, description); err != nil {
		return UserApiKeySummary{}, err
	}
	return a.keySummaryByHash(ctx, apiKeyHash, nil)
}

func (a *App) unbindUserAPIKey(ctx context.Context, userID int, apiKeyHash string) error {
	if _, err := a.getUser(ctx, userID); err != nil {
		return err
	}
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_api_keys WHERE user_id = ? AND api_key_hash = ?`, userID, apiKeyHash)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return notFoundError("API KEY 绑定不存在")
	}
	return nil
}

func (a *App) currentUserAPIKeys(ctx context.Context, user *AuthUser) ([]UserApiKeySummary, error) {
	keys, err := a.keySummaries(ctx)
	if err != nil {
		return nil, err
	}
	var result []UserApiKeySummary
	for _, key := range keys {
		if key.UserID != nil && *key.UserID == user.ID {
			name := user.Username
			key.UserName = &name
			result = append(result, key)
		}
	}
	return result, nil
}

func (a *App) createAPIKeyForUser(ctx context.Context, userID int, payload apiKeyCreatePayload) (UserApiKeySummary, error) {
	description, err := normalizeAPIKeyDescription(payload.Description)
	if err != nil {
		return UserApiKeySummary{}, err
	}

	var apiKey string
	if payload.APIKey.Set {
		apiKey, err = validateCustomAPIKey(payload.APIKey.Value)
		if err != nil {
			return UserApiKeySummary{}, err
		}
	}

	user, err := a.getActiveUser(ctx, userID)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	if err := a.ensureUserQuotaReadyForKeys(ctx, user.ID); err != nil {
		return UserApiKeySummary{}, err
	}

	if payload.APIKey.Set {
		exists, err := a.localAPIKeyExists(ctx, hashAPIKey(apiKey))
		if err != nil {
			return UserApiKeySummary{}, err
		}
		if exists {
			return UserApiKeySummary{}, conflictError("API KEY 已存在，不能重复创建")
		}
		exists, err = a.remoteAPIKeyExists(ctx, apiKey)
		if err != nil {
			return UserApiKeySummary{}, err
		}
		if exists {
			return UserApiKeySummary{}, conflictError("API KEY 已存在于 CPA，请由管理员使用绑定功能关联用户")
		}
	} else {
		if err := a.ensureAPIKeySyncConfigured(ctx); err != nil {
			return UserApiKeySummary{}, err
		}
		apiKey, err = a.generateUniqueAPIKey(ctx)
		if err != nil {
			return UserApiKeySummary{}, err
		}
	}

	apiKeyHash := hashAPIKey(apiKey)
	if err := a.insertUserAPIKey(ctx, user.ID, apiKeyHash, apiKey, description); err != nil {
		return UserApiKeySummary{}, err
	}
	if err := a.addRemoteAPIKey(ctx, apiKey); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiKeySyncTimeout)
		defer cancel()
		remoteExists, verifyErr := a.remoteAPIKeyExists(cleanupCtx, apiKey)
		if verifyErr != nil {
			log.Printf("unable to verify CPA API key after sync failure user_id=%d api_key_hash=%s: sync_error=%v verify_error=%v", user.ID, apiKeyHash, err, verifyErr)
			return UserApiKeySummary{}, appError("upstream_error", http.StatusBadGateway, "CPA API KEY 同步状态无法确认，请刷新后检查")
		}
		if !remoteExists {
			if cleanupErr := a.deleteCreatedAPIKey(cleanupCtx, user.ID, apiKeyHash); cleanupErr != nil {
				log.Printf("unable to clean local API key after sync failure user_id=%d api_key_hash=%s: sync_error=%v cleanup_error=%v", user.ID, apiKeyHash, err, cleanupErr)
				return UserApiKeySummary{}, cleanupErr
			}
			return UserApiKeySummary{}, err
		}
	}
	summary, err := a.keySummaryByHash(ctx, apiKeyHash, &apiKey)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	summary.UserName = &user.Username
	return summary, nil
}

func validateCustomAPIKey(apiKey string) (string, error) {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return "", validationError("API KEY 不能为空")
	}
	if trimmed != apiKey {
		return "", validationError("API KEY 不能包含首尾空白字符")
	}
	if len([]byte(apiKey)) > maxAPIKeyLengthBytes {
		return "", validationError("API KEY 不能超过 400 字节")
	}
	for _, char := range apiKey {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return "", validationError("API KEY 不能包含空白或控制字符")
		}
	}
	return apiKey, nil
}

func normalizeAPIKeyDescription(value string) (string, error) {
	description := strings.TrimSpace(value)
	if description == "" {
		return "", validationError("API KEY 描述不能为空")
	}
	if len([]rune(description)) > maxAPIKeyDescriptionRunes {
		return "", validationError("API KEY 描述不能超过 240 个字符")
	}
	return description, nil
}

func (a *App) remoteAPIKeyExists(ctx context.Context, apiKey string) (bool, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return false, validationError(apiKeySyncMissingConfigMessage)
	}
	syncCtx, cancel := context.WithTimeout(ctx, apiKeySyncTimeout)
	defer cancel()
	keys, err := a.remoteAPIKeys(syncCtx, cfg)
	if err != nil {
		return false, err
	}
	for _, existing := range keys {
		if existing == apiKey {
			return true, nil
		}
	}
	return false, nil
}

func (a *App) ensureAPIKeySyncConfigured(ctx context.Context) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError(apiKeySyncMissingConfigMessage)
	}
	return nil
}

func (a *App) updateCurrentUserAPIKey(ctx context.Context, user *AuthUser, apiKeyHash, description string) (UserApiKeySummary, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return UserApiKeySummary{}, validationError("API KEY 描述不能为空")
	}
	result, err := a.db.ExecContext(ctx, `UPDATE user_api_keys SET description = ?, updated_at = ? WHERE user_id = ? AND api_key_hash = ?`, description, dbTime(time.Now()), user.ID, apiKeyHash)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return UserApiKeySummary{}, notFoundError("API KEY 不存在")
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE users SET updated_at = ? WHERE id = ?`, dbTime(time.Now()), user.ID)
	summary, err := a.keySummaryByHash(ctx, apiKeyHash, nil)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	name := user.Username
	summary.UserName = &name
	return summary, nil
}

func (a *App) deleteCurrentUserAPIKey(ctx context.Context, user *AuthUser, apiKeyHash string) error {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_api_keys WHERE user_id = ? AND api_key_hash = ?`, user.ID, apiKeyHash)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return notFoundError("API KEY 不存在")
	}
	return a.removeRemoteAPIKeyHash(ctx, apiKeyHash)
}

func (a *App) upsertUserAPIKey(ctx context.Context, userID int, apiKeyHash, apiKey, description string) error {
	now := dbTime(time.Now())
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(api_key_hash) DO UPDATE SET user_id = excluded.user_id,
			api_key = excluded.api_key, description = excluded.description, updated_at = excluded.updated_at
	`, apiKeyHash, userID, apiKey, description, now, now)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `UPDATE users SET updated_at = ? WHERE id = ?`, now, userID)
	return err
}

func (a *App) insertUserAPIKey(ctx context.Context, userID int, apiKeyHash, apiKey, description string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := dbTime(time.Now())
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, apiKeyHash, userID, apiKey, description, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return conflictError("API KEY 已存在，不能重复创建")
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET updated_at = ? WHERE id = ?`, now, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) deleteCreatedAPIKey(ctx context.Context, userID int, apiKeyHash string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM user_api_keys WHERE user_id = ? AND api_key_hash = ?`, userID, apiKeyHash)
	return err
}

func (a *App) localAPIKeyExists(ctx context.Context, apiKeyHash string) (bool, error) {
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE api_key_hash = ?`, apiKeyHash).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) generateUniqueAPIKey(ctx context.Context) (string, error) {
	for i := 0; i < 10; i++ {
		var builder strings.Builder
		builder.WriteString(generatedAPIKeyPrefix)
		for j := 0; j < generatedAPIKeyLength; j++ {
			index, err := rand.Int(rand.Reader, big.NewInt(int64(len(generatedAPIKeyAlphabet))))
			if err != nil {
				return "", err
			}
			builder.WriteByte(generatedAPIKeyAlphabet[index.Int64()])
		}
		apiKey := builder.String()
		exists, err := a.localAPIKeyExists(ctx, hashAPIKey(apiKey))
		if err != nil {
			return "", err
		}
		if !exists {
			return apiKey, nil
		}
	}
	return "", conflictError("生成 API KEY 失败，请重试")
}

const userSelectColumns = `id, username, is_admin, nickname, CAST(disabled_at AS TEXT), password_hash, password_salt,
	CAST(created_at AS TEXT), CAST(updated_at AS TEXT), quota_lifetime_usd, quota_monthly_usd,
	CAST(quota_started_at AS TEXT), quota_month, quota_month_used_usd, CAST(quota_paused_at AS TEXT),
	quota_pause_reason, quota_sync_error, quota_unpriced_records`

func (a *App) allUsers(ctx context.Context) ([]UserRecord, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT `+userSelectColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []UserRecord
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(scanner userScanner) (UserRecord, error) {
	var user UserRecord
	var disabledAt, passwordHash, passwordSalt, createdAt, updatedAt, quotaStartedAt, quotaPausedAt, quotaPauseReason, quotaSyncError sql.NullString
	var quotaLifetime, quotaMonthly, quotaMonthUsed sql.NullFloat64
	var quotaUnpriced sql.NullInt64
	err := scanner.Scan(
		&user.ID, &user.Username, &user.IsAdmin, &user.Nickname, &disabledAt,
		&passwordHash, &passwordSalt, &createdAt, &updatedAt, &quotaLifetime,
		&quotaMonthly, &quotaStartedAt, &user.QuotaMonth, &quotaMonthUsed,
		&quotaPausedAt, &quotaPauseReason, &quotaSyncError, &quotaUnpriced,
	)
	if err != nil {
		return UserRecord{}, err
	}
	user.DisabledAt = timePtr(disabledAt)
	user.PasswordHash = nullableString(passwordHash)
	user.PasswordSalt = nullableString(passwordSalt)
	user.QuotaLifetimeUSD = nullableFloat(quotaLifetime)
	user.QuotaMonthlyUSD = nullableFloat(quotaMonthly)
	user.QuotaStartedAt = timePtr(quotaStartedAt)
	if quotaMonthUsed.Valid {
		user.QuotaMonthUsedUSD = quotaMonthUsed.Float64
	}
	user.QuotaPausedAt = timePtr(quotaPausedAt)
	user.QuotaPauseReason = nullableString(quotaPauseReason)
	user.QuotaSyncError = nullableString(quotaSyncError)
	if quotaUnpriced.Valid {
		user.QuotaUnpricedRecords = int(quotaUnpriced.Int64)
	}
	if parsed, ok := parseDBTime(createdAt.String); ok {
		user.CreatedAt = parsed
	}
	if parsed, ok := parseDBTime(updatedAt.String); ok {
		user.UpdatedAt = parsed
	}
	return user, nil
}

func (a *App) getUser(ctx context.Context, id int) (UserRecord, error) {
	row := a.db.QueryRowContext(ctx, `SELECT `+userSelectColumns+` FROM users WHERE id = ?`, id)
	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserRecord{}, notFoundError("用户不存在")
		}
		return UserRecord{}, err
	}
	return user, nil
}

func (a *App) userByUsername(ctx context.Context, username string) (UserRecord, error) {
	row := a.db.QueryRowContext(ctx, `SELECT `+userSelectColumns+` FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func (a *App) getActiveUser(ctx context.Context, id int) (UserRecord, error) {
	user, err := a.getUser(ctx, id)
	if err != nil {
		return UserRecord{}, err
	}
	if user.DisabledAt != nil {
		return UserRecord{}, conflictError("用户已禁用")
	}
	return user, nil
}

func (a *App) ensureUsernameAvailable(ctx context.Context, username string, exceptID *int) error {
	var id int
	err := a.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if exceptID == nil || id != *exceptID {
		return conflictError("账号已存在")
	}
	return nil
}

func (a *App) userAPIKeys(ctx context.Context, userID int) ([]UserAPIKey, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT api_key_hash, user_id, api_key, description, CAST(created_at AS TEXT), CAST(updated_at AS TEXT) FROM user_api_keys WHERE user_id = ? ORDER BY created_at, api_key_hash`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

func (a *App) getAPIKey(ctx context.Context, apiKeyHash string) (UserAPIKey, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT api_key_hash, user_id, api_key, description, CAST(created_at AS TEXT), CAST(updated_at AS TEXT) FROM user_api_keys WHERE api_key_hash = ?`, apiKeyHash)
	if err != nil {
		return UserAPIKey{}, err
	}
	defer rows.Close()
	keys, err := scanAPIKeys(rows)
	if err != nil {
		return UserAPIKey{}, err
	}
	if len(keys) == 0 {
		return UserAPIKey{}, notFoundError("API KEY 不存在")
	}
	return keys[0], nil
}

func scanAPIKeys(rows *sql.Rows) ([]UserAPIKey, error) {
	var keys []UserAPIKey
	for rows.Next() {
		var key UserAPIKey
		var apiKey, createdAt, updatedAt sql.NullString
		if err := rows.Scan(&key.APIKeyHash, &key.UserID, &apiKey, &key.Description, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		key.APIKey = nullableString(apiKey)
		key.CreatedAt = timePtr(createdAt)
		key.UpdatedAt = timePtr(updatedAt)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (a *App) keySummaries(ctx context.Context) ([]UserApiKeySummary, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT k.api_key_hash, k.user_id, k.api_key, k.description, CAST(k.created_at AS TEXT), CAST(k.updated_at AS TEXT),
		       u.nickname, u.username
		FROM user_api_keys k
		LEFT JOIN users u ON u.id = k.user_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var summaries []UserApiKeySummary
	for rows.Next() {
		var summary UserApiKeySummary
		var apiKey, createdAt, updatedAt, nickname, username sql.NullString
		var userID int
		if err := rows.Scan(&summary.APIKeyHash, &userID, &apiKey, &summary.Description, &createdAt, &updatedAt, &nickname, &username); err != nil {
			return nil, err
		}
		summary.APIKey = nullableString(apiKey)
		summary.CreatedAt = timePtr(createdAt)
		summary.UpdatedAt = timePtr(updatedAt)
		summary.UserID = &userID
		if username.Valid {
			label := strings.TrimSpace(nickname.String)
			if label == "" {
				label = username.String
			}
			summary.UserName = &label
		}
		summary.Providers = []string{}
		summary.Models = []string{}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		left, right := time.Time{}, time.Time{}
		if summaries[i].UpdatedAt != nil {
			left = *summaries[i].UpdatedAt
		}
		if summaries[j].UpdatedAt != nil {
			right = *summaries[j].UpdatedAt
		}
		return left.After(right)
	})
	return summaries, rows.Err()
}

func (a *App) keySummaryByHash(ctx context.Context, apiKeyHash string, fullKey *string) (UserApiKeySummary, error) {
	summaries, err := a.keySummaries(ctx)
	if err != nil {
		return UserApiKeySummary{}, err
	}
	for _, summary := range summaries {
		if summary.APIKeyHash == apiKeyHash {
			if fullKey != nil {
				summary.APIKey = fullKey
			}
			return summary, nil
		}
	}
	return UserApiKeySummary{}, notFoundError("API KEY 不存在")
}

type userUsageSummary struct {
	Records               int
	SuccessRecords        int
	FailedRecords         int
	TotalTokens           int
	TodayRecords          int
	TodaySuccessRecords   int
	TodayFailedRecords    int
	TodayInputTokens      int
	TodayOutputTokens     int
	TodayCachedTokens     int
	TodayReasoningTokens  int
	TodayTotalTokens      int
	TodayEstimatedCostUSD float64
	TodayUnpricedRecords  int
	FirstSeenAt           *time.Time
	LastSeenAt            *time.Time
	LastProvider          *string
	LastModel             *string
	Providers             []string
	Models                []string
}

func emptyUserUsageSummary() userUsageSummary {
	return userUsageSummary{Providers: []string{}, Models: []string{}}
}

func (a *App) userUsageSummaries(ctx context.Context) (map[string]userUsageSummary, error) {
	prices, err := a.priceMap(ctx)
	if err != nil {
		return nil, err
	}
	filters := UsageFilters{}
	records, err := a.filteredUsageRecords(ctx, filters, "")
	if err != nil {
		return nil, err
	}
	todayStart, todayEnd := defaultTodayRange()
	result := map[string]userUsageSummary{}
	providerSeen := map[string]map[string]bool{}
	modelSeen := map[string]map[string]bool{}
	for _, record := range records {
		if record.UsageUsername == nil || strings.TrimSpace(*record.UsageUsername) == "" {
			continue
		}
		username := *record.UsageUsername
		summary := result[username]
		if summary.Providers == nil {
			summary = emptyUserUsageSummary()
			providerSeen[username] = map[string]bool{}
			modelSeen[username] = map[string]bool{}
		}
		summary.Records++
		if record.Failed {
			summary.FailedRecords++
		} else {
			summary.SuccessRecords++
		}
		summary.TotalTokens += usageAggregateTotalTokens(record)
		if summary.FirstSeenAt == nil || record.Timestamp.Before(*summary.FirstSeenAt) {
			t := record.Timestamp
			summary.FirstSeenAt = &t
		}
		if summary.LastSeenAt == nil || record.Timestamp.After(*summary.LastSeenAt) {
			t := record.Timestamp
			summary.LastSeenAt = &t
			summary.LastProvider = record.Provider
			summary.LastModel = record.Model
		}
		appendUniqueString(&summary.Providers, providerSeen[username], record.Provider)
		appendUniqueString(&summary.Models, modelSeen[username], record.Model)
		if !record.Timestamp.Before(todayStart) && record.Timestamp.Before(todayEnd) {
			amount, unpriced := recordCost(record, prices)
			summary.TodayRecords++
			if record.Failed {
				summary.TodayFailedRecords++
			} else {
				summary.TodaySuccessRecords++
			}
			summary.TodayInputTokens += usageAggregateInputTokens(record)
			summary.TodayOutputTokens += record.OutputTokens
			summary.TodayCachedTokens += record.CachedTokens
			summary.TodayReasoningTokens += record.ReasoningTokens
			summary.TodayTotalTokens += usageAggregateTotalTokens(record)
			summary.TodayEstimatedCostUSD = mathRound(summary.TodayEstimatedCostUSD+amount, 8)
			if unpriced {
				summary.TodayUnpricedRecords++
			}
		}
		result[username] = summary
	}
	return result, nil
}

func appendUniqueString(items *[]string, seen map[string]bool, value *string) {
	if value == nil {
		return
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" || seen[normalized] {
		return
	}
	seen[normalized] = true
	*items = append(*items, normalized)
}

func displayUserName(user UserRecord) string {
	if strings.TrimSpace(user.Nickname) != "" {
		return strings.TrimSpace(user.Nickname)
	}
	if strings.TrimSpace(user.Username) != "" {
		return strings.TrimSpace(user.Username)
	}
	return "未知用户"
}

func userSummaryResponse(user UserRecord, keys []UserApiKeySummary, usage userUsageSummary, quota UserQuotaStatusResponse) UserSummaryResponse {
	if usage.Providers == nil {
		usage = emptyUserUsageSummary()
	}
	return UserSummaryResponse{
		ID:                    user.ID,
		Username:              user.Username,
		IsAdmin:               user.IsAdmin,
		Nickname:              user.Nickname,
		DisabledAt:            user.DisabledAt,
		PasswordSet:           user.PasswordHash != nil && user.PasswordSalt != nil,
		CreatedAt:             user.CreatedAt,
		UpdatedAt:             user.UpdatedAt,
		APIKeys:               keys,
		KeyCount:              len(keys),
		Records:               usage.Records,
		SuccessRecords:        usage.SuccessRecords,
		FailedRecords:         usage.FailedRecords,
		TotalTokens:           usage.TotalTokens,
		TodayRecords:          usage.TodayRecords,
		TodaySuccessRecords:   usage.TodaySuccessRecords,
		TodayFailedRecords:    usage.TodayFailedRecords,
		TodayInputTokens:      usage.TodayInputTokens,
		TodayOutputTokens:     usage.TodayOutputTokens,
		TodayCachedTokens:     usage.TodayCachedTokens,
		TodayReasoningTokens:  usage.TodayReasoningTokens,
		TodayTotalTokens:      usage.TodayTotalTokens,
		TodayEstimatedCostUSD: usage.TodayEstimatedCostUSD,
		TodayUnpricedRecords:  usage.TodayUnpricedRecords,
		FirstSeenAt:           usage.FirstSeenAt,
		LastSeenAt:            usage.LastSeenAt,
		LastProvider:          usage.LastProvider,
		LastModel:             usage.LastModel,
		Providers:             usage.Providers,
		Models:                usage.Models,
		Quota:                 quota,
	}
}
