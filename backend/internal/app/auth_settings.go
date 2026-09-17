package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

type changeCredentialsRequest struct {
	Password        string  `json:"password"`
	CurrentPassword *string `json:"current_password"`
}

var apiKeySyncTimeout = 8 * time.Second
var modelRequestTestTimeout = 45 * time.Second

const (
	modelRequestEndpointChatCompletions = "chat_completions"
	modelRequestEndpointResponses       = "responses"
	modelRequestEndpointClaudeMessages  = "claude_messages"
)

const apiKeySyncMissingConfigMessage = "CPA 配置未完成：请先到「系统设置」填写 CLIProxyAPI 地址和管理密钥，再返回 API 密钥页操作。"

func (a *App) handleAuth(w http.ResponseWriter, r *http.Request) error {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth")
	switch path {
	case "/login":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.handleLogin(w, r)
	case "/setup":
		if r.Method == http.MethodGet {
			return a.handleSetupState(w, r)
		}
		if r.Method == http.MethodPost {
			return a.handleSetupFirstAdmin(w, r)
		}
		return methodNotAllowed()
	case "/me":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		user, err := a.currentUser(r.Context(), r)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, user)
		return nil
	case "/change-credentials":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.handleChangeCredentials(w, r)
	case "/logout":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		clearSessionCookie(w)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	default:
		return notFoundError("Not Found")
	}
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var payload loginRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	username := strings.TrimSpace(payload.Username)
	if username == "" || strings.TrimSpace(payload.Password) == "" {
		return validationError("账号和密码不能为空")
	}
	count, err := a.userCount(r.Context())
	if err != nil {
		return err
	}
	if count == 0 {
		return conflictError("系统尚未初始化，请先创建第一个管理员账号")
	}
	user, hash, salt, disabled, err := a.userCredentialsByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authenticationError("用户名或密码不正确")
		}
		return err
	}
	if disabled || hash == nil || salt == nil || !verifyPassword(payload.Password, *salt, *hash) {
		return authenticationError("用户名或密码不正确")
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if err := setSessionCookie(w, user.ID, cfg.SessionSecret); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, user)
	return nil
}

func (a *App) handleSetupState(w http.ResponseWriter, r *http.Request) error {
	count, err := a.userCount(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": count == 0})
	return nil
}

func (a *App) handleSetupFirstAdmin(w http.ResponseWriter, r *http.Request) error {
	var payload setupRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	username := strings.TrimSpace(payload.Username)
	nickname := strings.TrimSpace(payload.Nickname)
	if username == "" || nickname == "" {
		return validationError("账号和昵称不能为空")
	}
	if len(payload.Password) < 8 {
		return validationError("密码长度不能少于 8 位")
	}
	count, err := a.userCount(r.Context())
	if err != nil {
		return err
	}
	if count > 0 {
		return conflictError("第一个管理员账号已存在")
	}
	salt, err := createSalt()
	if err != nil {
		return err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(r.Context(), `
		INSERT INTO users (username, password_hash, password_salt, is_admin, nickname, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?, ?)
	`, username, hashPassword(payload.Password, salt), salt, nickname, now, now)
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if err := setSessionCookie(w, int(id), cfg.SessionSecret); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, AuthUser{ID: int(id), Username: username, IsAdmin: true})
	return nil
}

func (a *App) handleChangeCredentials(w http.ResponseWriter, r *http.Request) error {
	current, err := a.currentUser(r.Context(), r)
	if err != nil {
		return err
	}
	var payload changeCredentialsRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	if len(payload.Password) < 8 {
		return validationError("密码长度不能少于 8 位")
	}
	if payload.CurrentPassword == nil {
		return forbiddenError("需要提供当前密码")
	}
	var passwordHash, passwordSalt sql.NullString
	err = a.db.QueryRowContext(r.Context(), `SELECT password_hash, password_salt FROM users WHERE id = ? AND disabled_at IS NULL`, current.ID).Scan(&passwordHash, &passwordSalt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authenticationError("登录会话已失效")
		}
		return err
	}
	if !passwordHash.Valid || !passwordSalt.Valid || !verifyPassword(*payload.CurrentPassword, passwordSalt.String, passwordHash.String) {
		return authenticationError("当前密码不正确")
	}
	salt, err := createSalt()
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(r.Context(), `UPDATE users SET password_hash = ?, password_salt = ?, updated_at = ? WHERE id = ?`, hashPassword(payload.Password, salt), salt, dbTime(time.Now()), current.ID)
	if err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if err := setSessionCookie(w, current.ID, cfg.SessionSecret); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, current)
	return nil
}

func (a *App) userCount(ctx context.Context) (int, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (a *App) firstActiveUserID(ctx context.Context) (*int, error) {
	var id int
	err := a.db.QueryRowContext(ctx, `SELECT id FROM users WHERE disabled_at IS NULL ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (a *App) ensureUsersInitialized(ctx context.Context) error {
	id, err := a.firstActiveUserID(ctx)
	if err != nil {
		return err
	}
	if id == nil {
		return conflictError("请先创建第一个管理员账号")
	}
	return nil
}

func (a *App) userCredentialsByUsername(ctx context.Context, username string) (AuthUser, *string, *string, bool, error) {
	var user AuthUser
	var passwordHash, passwordSalt, disabledAt sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT id, username, is_admin, password_hash, password_salt, disabled_at FROM users WHERE username = ?`, username).Scan(&user.ID, &user.Username, &user.IsAdmin, &passwordHash, &passwordSalt, &disabledAt)
	if err != nil {
		return AuthUser{}, nil, nil, false, err
	}
	return user, nullableString(passwordHash), nullableString(passwordSalt), disabledAt.Valid, nil
}

type settingsUpdateRequest struct {
	CLIProxyURL          *string  `json:"cliaproxy_url"`
	ModelRequestURL      *string  `json:"model_request_url"`
	ManagementKey        *string  `json:"management_key"`
	CollectorEnabled     *bool    `json:"collector_enabled"`
	QueueName            *string  `json:"queue_name"`
	BatchSize            *int     `json:"batch_size"`
	PollIntervalSeconds  *float64 `json:"poll_interval_seconds"`
	RetryIntervalSeconds *float64 `json:"retry_interval_seconds"`
	APIKeyPrefix         *string  `json:"api_key_prefix"`
}

type modelRequestTestPayload struct {
	APIKeyHash string `json:"api_key_hash"`
	Endpoint   string `json:"endpoint"`
	Model      string `json:"model"`
	Message    string `json:"message"`
}

type modelRequestTestResponse struct {
	Endpoint   string         `json:"endpoint"`
	Model      string         `json:"model"`
	Reply      string         `json:"reply"`
	StatusCode int            `json:"status_code"`
	DurationMS int64          `json:"duration_ms"`
	Usage      map[string]any `json:"usage,omitempty"`
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	switch r.Method {
	case http.MethodGet:
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, settingsResponse(cfg))
		return nil
	case http.MethodPut:
		var payload settingsUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		if payload.CLIProxyURL != nil {
			value := strings.TrimRight(strings.TrimSpace(*payload.CLIProxyURL), "/")
			if value == "" {
				return validationError("不能为空")
			}
			cfg.Collector.CLIProxyURL = value
		}
		if payload.ModelRequestURL != nil {
			value, err := normalizeModelRequestURL(*payload.ModelRequestURL)
			if err != nil {
				return err
			}
			cfg.ModelRequestURL = value
		}
		if payload.ManagementKey != nil {
			cfg.Collector.ManagementKey = strings.TrimSpace(*payload.ManagementKey)
		}
		if payload.CollectorEnabled != nil {
			cfg.Collector.Enabled = *payload.CollectorEnabled
		}
		if payload.QueueName != nil {
			value := strings.TrimSpace(*payload.QueueName)
			if value == "" {
				return validationError("不能为空")
			}
			cfg.Collector.QueueName = value
		}
		if payload.BatchSize != nil {
			if *payload.BatchSize < 1 || *payload.BatchSize > 1000 {
				return validationError("batch_size 超出范围")
			}
			cfg.Collector.BatchSize = *payload.BatchSize
		}
		if payload.PollIntervalSeconds != nil {
			if *payload.PollIntervalSeconds < 0.2 || *payload.PollIntervalSeconds > 3600 {
				return validationError("poll_interval_seconds 超出范围")
			}
			cfg.Collector.PollIntervalSeconds = *payload.PollIntervalSeconds
		}
		if payload.RetryIntervalSeconds != nil {
			if *payload.RetryIntervalSeconds < 1 || *payload.RetryIntervalSeconds > 3600 {
				return validationError("retry_interval_seconds 超出范围")
			}
			cfg.Collector.RetryIntervalSeconds = *payload.RetryIntervalSeconds
		}
		if payload.APIKeyPrefix != nil {
			if err := validateAPIKeyPrefix(*payload.APIKeyPrefix); err != nil {
				return err
			}
			cfg.APIKeyPrefix = normalizeAPIKeyPrefix(*payload.APIKeyPrefix)
		}
		if err := a.saveConfig(r.Context(), cfg); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, settingsResponse(cfg))
		return nil
	default:
		return methodNotAllowed()
	}
}

func settingsResponse(cfg AppConfig) map[string]any {
	collector := cfg.Collector
	return map[string]any{
		"cliaproxy_url":          collector.CLIProxyURL,
		"model_request_url":      cfg.ModelRequestURL,
		"management_key":         collector.ManagementKey,
		"management_key_set":     strings.TrimSpace(collector.ManagementKey) != "",
		"collector_enabled":      collector.Enabled,
		"queue_name":             collector.QueueName,
		"batch_size":             collector.BatchSize,
		"poll_interval_seconds":  collector.PollIntervalSeconds,
		"retry_interval_seconds": collector.RetryIntervalSeconds,
		"api_key_prefix":         normalizeAPIKeyPrefix(cfg.APIKeyPrefix),
	}
}

func (a *App) handleCurrentModelRequestGuide(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.currentUser(r.Context(), r); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, modelRequestGuideResponse(cfg.ModelRequestURL))
	return nil
}

func (a *App) handleCurrentModelRequestTest(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodPost); err != nil {
		return err
	}
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	var payload modelRequestTestPayload
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	response, err := a.testCurrentUserModelRequest(r.Context(), user, payload)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func (a *App) testCurrentUserModelRequest(ctx context.Context, user *AuthUser, payload modelRequestTestPayload) (modelRequestTestResponse, error) {
	apiKeyHash := strings.TrimSpace(payload.APIKeyHash)
	model := strings.TrimSpace(payload.Model)
	message := strings.TrimSpace(payload.Message)
	if apiKeyHash == "" {
		return modelRequestTestResponse{}, validationError("API KEY 不能为空")
	}
	if model == "" {
		return modelRequestTestResponse{}, validationError("测试模型不能为空")
	}
	if len(model) > 256 {
		return modelRequestTestResponse{}, validationError("测试模型名称过长")
	}
	if message == "" {
		message = "请用一句中文回复：连接测试成功。"
	}
	if len(message) > 4000 {
		return modelRequestTestResponse{}, validationError("测试消息不能超过 4000 个字符")
	}
	endpoint, err := normalizeModelRequestEndpoint(payload.Endpoint)
	if err != nil {
		return modelRequestTestResponse{}, err
	}

	apiKey, err := a.getAPIKey(ctx, apiKeyHash)
	if err != nil {
		return modelRequestTestResponse{}, err
	}
	if apiKey.UserID != user.ID {
		return modelRequestTestResponse{}, notFoundError("API KEY 不存在")
	}
	if apiKey.APIKey == nil || strings.TrimSpace(*apiKey.APIKey) == "" {
		return modelRequestTestResponse{}, conflictError("当前 API KEY 缺少完整密钥，无法发起测试")
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return modelRequestTestResponse{}, err
	}
	target := strings.TrimRight(modelRequestOpenAIBaseURL(cfg.ModelRequestURL), "/") + modelRequestEndpointPath(endpoint)
	headers := modelRequestEndpointHeaders(endpoint, strings.TrimSpace(*apiKey.APIKey))
	requestBody := modelRequestEndpointBody(endpoint, model, message)

	start := time.Now()
	response, responseBody, err := doJSON(ctx, httpClient(modelRequestTestTimeout), http.MethodPost, target, headers, requestBody)
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return modelRequestTestResponse{}, validationError("模型请求超时，请检查模型请求地址或稍后重试")
		}
		return modelRequestTestResponse{}, validationError("模型请求失败：" + err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := compactRemoteResponse(responseBody)
		if detail != "" {
			return modelRequestTestResponse{}, validationError(fmt.Sprintf("模型请求失败：HTTP %d：%s", response.StatusCode, detail))
		}
		return modelRequestTestResponse{}, validationError(fmt.Sprintf("模型请求失败：HTTP %d", response.StatusCode))
	}
	reply, usage, err := parseModelRequestTestResponse(endpoint, responseBody)
	if err != nil {
		return modelRequestTestResponse{}, err
	}
	return modelRequestTestResponse{
		Endpoint:   endpoint,
		Model:      model,
		Reply:      reply,
		StatusCode: response.StatusCode,
		DurationMS: durationMS,
		Usage:      usage,
	}, nil
}

func parseModelRequestTestResponse(endpoint string, payload []byte) (string, map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return "", nil, validationError("模型响应不是有效 JSON")
	}
	var usage map[string]any
	if value, ok := raw["usage"].(map[string]any); ok {
		usage = value
	}
	var reply string
	switch endpoint {
	case modelRequestEndpointResponses:
		reply = extractResponsesReply(raw)
	case modelRequestEndpointClaudeMessages:
		reply = extractClaudeMessagesReply(raw)
	default:
		reply = extractChatCompletionReply(raw)
	}
	return reply, usage, nil
}

func normalizeModelRequestEndpoint(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", modelRequestEndpointChatCompletions:
		return modelRequestEndpointChatCompletions, nil
	case modelRequestEndpointResponses:
		return modelRequestEndpointResponses, nil
	case modelRequestEndpointClaudeMessages:
		return modelRequestEndpointClaudeMessages, nil
	default:
		return "", validationError("请求格式不支持")
	}
}

func modelRequestEndpointPath(endpoint string) string {
	switch endpoint {
	case modelRequestEndpointResponses:
		return "/responses"
	case modelRequestEndpointClaudeMessages:
		return "/messages"
	default:
		return "/chat/completions"
	}
}

func modelRequestEndpointHeaders(endpoint string, apiKey string) http.Header {
	headers := http.Header{}
	if endpoint == modelRequestEndpointClaudeMessages {
		headers.Set("x-api-key", apiKey)
		headers.Set("anthropic-version", "2023-06-01")
		return headers
	}
	headers.Set("Authorization", "Bearer "+apiKey)
	return headers
}

func modelRequestEndpointBody(endpoint string, model string, message string) map[string]any {
	switch endpoint {
	case modelRequestEndpointResponses:
		return map[string]any{
			"model":  model,
			"input":  message,
			"stream": false,
		}
	case modelRequestEndpointClaudeMessages:
		return map[string]any{
			"model":      model,
			"max_tokens": 1024,
			"messages": []map[string]string{
				{"role": "user", "content": message},
			},
		}
	default:
		return map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": message},
			},
			"stream": false,
		}
	}
}

func extractChatCompletionReply(raw map[string]any) string {
	choices, ok := raw["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	for _, item := range choices {
		choice, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if message, ok := choice["message"].(map[string]any); ok {
			if content := chatContentText(message["content"]); content != "" {
				return content
			}
		}
		if text := stringValue(choice["text"]); text != nil {
			return *text
		}
	}
	return ""
}

func extractResponsesReply(raw map[string]any) string {
	if text := stringValue(raw["output_text"]); text != nil {
		return *text
	}
	output, ok := raw["output"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if content := chatContentText(object["content"]); content != "" {
			parts = append(parts, content)
		}
		if text := stringValue(object["text"]); text != nil {
			parts = append(parts, *text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractClaudeMessagesReply(raw map[string]any) string {
	if content := chatContentText(raw["content"]); content != "" {
		return content
	}
	if text := stringValue(raw["completion"]); text != nil {
		return *text
	}
	return ""
}

func chatContentText(value any) string {
	if text := stringValue(value); text != nil {
		return *text
	}
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := stringValue(object["text"]); text != nil {
			parts = append(parts, *text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func compactRemoteResponse(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 800 {
		text = string(runes[:800]) + "..."
	}
	return text
}

func normalizeModelRequestURL(value string) (string, error) {
	normalized := strings.TrimRight(strings.TrimSpace(value), "/")
	if normalized == "" {
		return "", validationError("模型请求地址不能为空")
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", validationError("模型请求地址必须是有效的 http:// 或 https:// 地址")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", validationError("模型请求地址必须使用 http:// 或 https://")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", validationError("模型请求地址不能包含查询参数或锚点")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func modelRequestGuideResponse(modelRequestURL string) map[string]any {
	requestURL := strings.TrimRight(strings.TrimSpace(modelRequestURL), "/")
	if requestURL == "" {
		requestURL = defaultCPAURL
	}
	openAIBaseURL := modelRequestOpenAIBaseURL(requestURL)
	return map[string]any{
		"model_request_url":    requestURL,
		"openai_base_url":      openAIBaseURL,
		"chat_completions_url": strings.TrimRight(openAIBaseURL, "/") + "/chat/completions",
	}
}

func modelRequestOpenAIBaseURL(requestURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(requestURL), "/")
	if normalized == "" {
		normalized = defaultCPAURL
	}
	if strings.HasSuffix(strings.ToLower(normalized), "/v1") {
		return normalized
	}
	return normalized + "/v1"
}

func (a *App) handleCollectorStatus(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	state, err := a.collectorState(r.Context())
	if err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                cfg.Collector.Enabled,
		"running":                state.Running,
		"queue_name":             cfg.Collector.QueueName,
		"batch_size":             cfg.Collector.BatchSize,
		"poll_interval_seconds":  cfg.Collector.PollIntervalSeconds,
		"retry_interval_seconds": cfg.Collector.RetryIntervalSeconds,
		"last_poll_at":           state.LastPollAt,
		"last_success_at":        state.LastSuccessAt,
		"last_error":             state.LastError,
		"remote_enabled":         state.RemoteEnabled,
		"records_collected":      state.RecordsCollected,
	})
	return nil
}

type collectorState struct {
	Running          bool
	LastPollAt       *time.Time
	LastSuccessAt    *time.Time
	LastError        *string
	RemoteEnabled    *bool
	RecordsCollected int
}

func (a *App) collectorState(ctx context.Context) (collectorState, error) {
	_, err := a.db.ExecContext(ctx, `INSERT OR IGNORE INTO collector_state (id, running, records_collected, updated_at) VALUES (1, 0, 0, ?)`, dbTime(time.Now()))
	if err != nil {
		return collectorState{}, err
	}
	var state collectorState
	var lastPoll, lastSuccess, lastError sql.NullString
	var remote sql.NullBool
	err = a.db.QueryRowContext(ctx, `SELECT running, CAST(last_poll_at AS TEXT), CAST(last_success_at AS TEXT), last_error, remote_enabled, records_collected FROM collector_state WHERE id = 1`).Scan(&state.Running, &lastPoll, &lastSuccess, &lastError, &remote, &state.RecordsCollected)
	if err != nil {
		return collectorState{}, err
	}
	state.LastPollAt = timePtr(lastPoll)
	state.LastSuccessAt = timePtr(lastSuccess)
	state.LastError = nullableString(lastError)
	if remote.Valid {
		value := remote.Bool
		state.RemoteEnabled = &value
	}
	return state, nil
}

func (a *App) addRemoteAPIKey(ctx context.Context, apiKey string) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError(apiKeySyncMissingConfigMessage)
	}
	syncCtx, cancel := context.WithTimeout(ctx, apiKeySyncTimeout)
	defer cancel()
	unsupported, err := a.patchRemoteAPIKey(syncCtx, cfg, apiKey)
	if err != nil {
		return err
	}
	if !unsupported {
		return nil
	}
	keys, err := a.remoteAPIKeys(syncCtx, cfg)
	if err != nil {
		return err
	}
	for _, existing := range keys {
		if existing == apiKey {
			return nil
		}
	}
	keys = append(keys, apiKey)
	return a.putRemoteAPIKeys(syncCtx, cfg, keys)
}

func (a *App) removeRemoteAPIKeyHash(ctx context.Context, apiKeyHash string) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError(apiKeySyncMissingConfigMessage)
	}
	syncCtx, cancel := context.WithTimeout(ctx, apiKeySyncTimeout)
	defer cancel()
	keys, err := a.remoteAPIKeys(syncCtx, cfg)
	if err != nil {
		return err
	}
	next := make([]string, 0, len(keys))
	changed := false
	for _, key := range keys {
		if hashAPIKey(key) == apiKeyHash {
			changed = true
			continue
		}
		next = append(next, key)
	}
	if !changed {
		return nil
	}
	return a.putRemoteAPIKeys(syncCtx, cfg, next)
}

func (a *App) remoteAPIKeys(ctx context.Context, cfg AppConfig) ([]string, error) {
	response, payload, err := doJSON(ctx, httpClient(apiKeySyncTimeout), http.MethodGet, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/api-keys", nil), managementHeaders(cfg.Collector.ManagementKey), nil)
	if err != nil {
		return nil, remoteAPIKeyError("读取 CPA API KEY", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, validationError(fmt.Sprintf("读取 CPA API KEY 失败：HTTP %d", response.StatusCode))
	}
	return parseStringList(payload), nil
}

func (a *App) putRemoteAPIKeys(ctx context.Context, cfg AppConfig, keys []string) error {
	response, _, err := doJSON(ctx, httpClient(apiKeySyncTimeout), http.MethodPut, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/api-keys", nil), managementHeaders(cfg.Collector.ManagementKey), keys)
	if err != nil {
		return remoteAPIKeyError("写入 CPA API KEY", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("写入 CPA API KEY 失败：HTTP %d", response.StatusCode))
	}
	return nil
}

func (a *App) patchRemoteAPIKey(ctx context.Context, cfg AppConfig, apiKey string) (bool, error) {
	payload := map[string]string{"old": apiKey, "new": apiKey}
	response, _, err := doJSON(ctx, httpClient(apiKeySyncTimeout), http.MethodPatch, makeURL(cfg.Collector.CLIProxyURL, "/v0/management/api-keys", nil), managementHeaders(cfg.Collector.ManagementKey), payload)
	if err != nil {
		return false, remoteAPIKeyError("写入 CPA API KEY", err)
	}
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return true, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, validationError(fmt.Sprintf("写入 CPA API KEY 失败：HTTP %d", response.StatusCode))
	}
	return false, nil
}

func remoteAPIKeyError(action string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return validationError(action + " 超时，请检查 CLIProxyAPI 地址和管理密钥")
	}
	return validationError(fmt.Sprintf("%s 失败：%s", action, err.Error()))
}

func parseStringList(payload []byte) []string {
	var raw any
	if json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	var result []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					result = append(result, strings.TrimSpace(text))
				}
			}
		case map[string]any:
			for _, key := range []string{"api-keys", "api_keys", "items", "value", "data"} {
				if child, ok := typed[key]; ok {
					walk(child)
					return
				}
			}
		}
	}
	walk(raw)
	return result
}
