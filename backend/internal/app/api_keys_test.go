package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

type apiKeyCreateResponse struct {
	APIKey     string `json:"api_key"`
	APIKeyHash string `json:"api_key_hash"`
	UserID     *int   `json:"user_id"`
}

type fakeCPAAPIKeys struct {
	mu             sync.Mutex
	keys           []string
	getCalls       int
	patchCalls     int
	failPatch      bool
	failAfterPatch bool
}

func (fake *fakeCPAAPIKeys) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v0/management/api-keys" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		fake.getCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": fake.keys})
	case http.MethodPatch:
		fake.patchCalls++
		if fake.failPatch {
			http.Error(w, "remote write failed", http.StatusInternalServerError)
			return
		}
		var payload struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, key := range fake.keys {
			if key == payload.New {
				if fake.failAfterPatch {
					http.Error(w, "remote response failed after write", http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": fake.keys})
				return
			}
		}
		fake.keys = append(fake.keys, payload.New)
		if fake.failAfterPatch {
			http.Error(w, "remote response failed after write", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": fake.keys})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (fake *fakeCPAAPIKeys) snapshot() ([]string, int, int) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.keys...), fake.getCalls, fake.patchCalls
}

func newConfiguredAPIKeyTestHandler(t *testing.T, fake *fakeCPAAPIKeys) (http.Handler, []*http.Cookie) {
	t.Helper()
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(fake)
	t.Cleanup(cpa.Close)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	t.Cleanup(app.Close)

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	return handler, cookies
}

func TestListAPIKeysReturnsEmptyArrayForFreshAccount(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	var keys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &keys)
	if keys == nil {
		t.Fatal("fresh API key list decoded as nil; want empty JSON array")
	}
	if len(keys) != 0 {
		t.Fatalf("fresh API key list length = %d, want 0", len(keys))
	}
}

func TestAccountModelRequestGuideUsesConfiguredURL(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:8317",
		"model_request_url": "http://models.example.local/proxy",
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	var guide struct {
		ModelRequestURL    string `json:"model_request_url"`
		OpenAIBaseURL      string `json:"openai_base_url"`
		ChatCompletionsURL string `json:"chat_completions_url"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/account/model-request", nil, cookies, &guide)
	if guide.ModelRequestURL != "http://models.example.local/proxy" {
		t.Fatalf("model_request_url = %q", guide.ModelRequestURL)
	}
	if guide.OpenAIBaseURL != "http://models.example.local/proxy/v1" {
		t.Fatalf("openai_base_url = %q", guide.OpenAIBaseURL)
	}
	if guide.ChatCompletionsURL != "http://models.example.local/proxy/v1/chat/completions" {
		t.Fatalf("chat_completions_url = %q", guide.ChatCompletionsURL)
	}

	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"model_request_url": "http://models.example.local/v1",
	}, cookies, nil)
	requestJSON(t, handler, http.MethodGet, "/api/account/model-request", nil, cookies, &guide)
	if guide.OpenAIBaseURL != "http://models.example.local/v1" {
		t.Fatalf("openai_base_url with existing /v1 = %q", guide.OpenAIBaseURL)
	}
	if guide.ChatCompletionsURL != "http://models.example.local/v1/chat/completions" {
		t.Fatalf("chat_completions_url with existing /v1 = %q", guide.ChatCompletionsURL)
	}
}

func TestAccountModelRequestTestUsesCurrentUserAPIKey(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var expectedAuth string
	remoteKeys := []string{}
	chatCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/api-keys" && r.Method == http.MethodPatch:
			var payload struct {
				Old string `json:"old"`
				New string `json:"new"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			remoteKeys = append(remoteKeys, payload.New)
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			chatCalls++
			if got := r.Header.Get("Authorization"); got != expectedAuth {
				t.Fatalf("Authorization = %q, want %q", got, expectedAuth)
			}
			var payload struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "gpt-test" {
				t.Fatalf("model = %q, want gpt-test", payload.Model)
			}
			if payload.Stream {
				t.Fatal("stream = true, want false")
			}
			if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "ping" {
				t.Fatalf("messages = %#v, want one user ping message", payload.Messages)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"role": "assistant", "content": "pong"}},
				},
				"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	expectedAuth = "Bearer " + created.APIKey

	var result struct {
		Endpoint   string         `json:"endpoint"`
		Model      string         `json:"model"`
		Reply      string         `json:"reply"`
		StatusCode int            `json:"status_code"`
		DurationMS int64          `json:"duration_ms"`
		Usage      map[string]any `json:"usage"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"model":        "gpt-test",
		"message":      "ping",
	}, cookies, &result)

	if result.Endpoint != "chat_completions" || result.Model != "gpt-test" || result.Reply != "pong" || result.StatusCode != http.StatusOK {
		t.Fatalf("test response = %#v, want model/reply/status", result)
	}
	if result.DurationMS < 0 {
		t.Fatalf("duration_ms = %d, want non-negative", result.DurationMS)
	}
	if got := result.Usage["total_tokens"]; got != float64(3) {
		t.Fatalf("usage total_tokens = %#v, want 3", got)
	}
	if chatCalls != 1 {
		t.Fatalf("chat calls = %d, want 1", chatCalls)
	}
}

func TestAccountModelRequestTestSupportsResponsesEndpoint(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var expectedAuth string
	remoteKeys := []string{}
	responsesCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/api-keys" && r.Method == http.MethodPatch:
			var payload struct {
				Old string `json:"old"`
				New string `json:"new"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			remoteKeys = append(remoteKeys, payload.New)
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case r.URL.Path == "/v1/responses" && r.Method == http.MethodPost:
			responsesCalls++
			if got := r.Header.Get("Authorization"); got != expectedAuth {
				t.Fatalf("Authorization = %q, want %q", got, expectedAuth)
			}
			var payload struct {
				Model  string `json:"model"`
				Input  string `json:"input"`
				Stream bool   `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "gpt-response-test" {
				t.Fatalf("model = %q, want gpt-response-test", payload.Model)
			}
			if payload.Input != "ping responses" {
				t.Fatalf("input = %q, want ping responses", payload.Input)
			}
			if payload.Stream {
				t.Fatal("stream = true, want false")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"output_text": "responses pong",
				"usage":       map[string]any{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	expectedAuth = "Bearer " + created.APIKey

	var result struct {
		Endpoint   string         `json:"endpoint"`
		Model      string         `json:"model"`
		Reply      string         `json:"reply"`
		StatusCode int            `json:"status_code"`
		Usage      map[string]any `json:"usage"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"endpoint":     "responses",
		"model":        "gpt-response-test",
		"message":      "ping responses",
	}, cookies, &result)

	if result.Endpoint != "responses" || result.Model != "gpt-response-test" || result.Reply != "responses pong" || result.StatusCode != http.StatusOK {
		t.Fatalf("test response = %#v, want responses endpoint/model/reply/status", result)
	}
	if got := result.Usage["total_tokens"]; got != float64(3) {
		t.Fatalf("usage total_tokens = %#v, want 3", got)
	}
	if responsesCalls != 1 {
		t.Fatalf("responses calls = %d, want 1", responsesCalls)
	}
}

func TestAccountModelRequestTestSupportsClaudeMessagesEndpoint(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	var expectedAPIKey string
	remoteKeys := []string{}
	claudeCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v0/management/api-keys" && r.Method == http.MethodPatch:
			var payload struct {
				Old string `json:"old"`
				New string `json:"new"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			remoteKeys = append(remoteKeys, payload.New)
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
			claudeCalls++
			if got := r.Header.Get("x-api-key"); got != expectedAPIKey {
				t.Fatalf("x-api-key = %q, want %q", got, expectedAPIKey)
			}
			if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
				t.Fatalf("anthropic-version = %q, want 2023-06-01", got)
			}
			var payload struct {
				Model    string `json:"model"`
				MaxToken int    `json:"max_tokens"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Model != "claude-test" {
				t.Fatalf("model = %q, want claude-test", payload.Model)
			}
			if payload.MaxToken != 1024 {
				t.Fatalf("max_tokens = %d, want 1024", payload.MaxToken)
			}
			if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "ping claude" {
				t.Fatalf("messages = %#v, want one user ping claude message", payload.Messages)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "claude pong"},
				},
				"usage": map[string]any{"input_tokens": 2, "output_tokens": 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)
	expectedAPIKey = created.APIKey

	var result struct {
		Endpoint   string         `json:"endpoint"`
		Model      string         `json:"model"`
		Reply      string         `json:"reply"`
		StatusCode int            `json:"status_code"`
		Usage      map[string]any `json:"usage"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"endpoint":     "claude_messages",
		"model":        "claude-test",
		"message":      "ping claude",
	}, cookies, &result)

	if result.Endpoint != "claude_messages" || result.Model != "claude-test" || result.Reply != "claude pong" || result.StatusCode != http.StatusOK {
		t.Fatalf("test response = %#v, want claude endpoint/model/reply/status", result)
	}
	if got := result.Usage["output_tokens"]; got != float64(1) {
		t.Fatalf("usage output_tokens = %#v, want 1", got)
	}
	if claudeCalls != 1 {
		t.Fatalf("claude calls = %d, want 1", claudeCalls)
	}
}

func TestAccountModelRequestTestRejectsOtherUserAPIKey(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v0/management/api-keys" && r.Method == http.MethodPatch {
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": []string{"ok"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	adminCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"model_request_url": cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, adminCookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "Admin",
	}, adminCookies, &created)
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, adminCookies, nil)
	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, nil)

	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/account/model-request/test", map[string]any{
		"api_key_hash": created.APIKeyHash,
		"model":        "gpt-test",
		"message":      "ping",
	}, memberCookies, http.StatusNotFound)
}

func TestCreateAPIKeyWithoutCPAConfigGuidesToSettings(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)

	body, err := json.Marshal(map[string]any{"description": "VSCode"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/api-keys", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /api/api-keys returned %d: %s", recorder.Code, recorder.Body.String())
	}
	message := recorder.Body.String()
	if !strings.Contains(message, "CPA 配置未完成") ||
		!strings.Contains(message, "系统设置") ||
		!strings.Contains(message, "CLIProxyAPI 地址和管理密钥") {
		t.Fatalf("missing actionable CPA config guidance in response: %s", message)
	}
	var keys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &keys)
	if len(keys) != 0 {
		t.Fatalf("missing CPA config left a local API key placeholder: %#v", keys)
	}
}

func TestCreateAPIKeyUsesPatchAppendWhenRemoteListIsEmpty(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	remoteKeys := []string{}
	getCalls := 0
	putCalls := 0
	patchCalls := 0
	badPatchPayload := ""
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/api-keys" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPatch:
			patchCalls++
			var payload struct {
				Old string `json:"old"`
				New string `json:"new"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Old == "" || payload.New == "" || payload.Old != payload.New {
				badPatchPayload = "old/new key must be the same non-empty value"
				http.Error(w, badPatchPayload, http.StatusBadRequest)
				return
			}
			replaced := false
			for index, key := range remoteKeys {
				if key == payload.Old {
					remoteKeys[index] = payload.New
					replaced = true
					break
				}
			}
			if !replaced {
				remoteKeys = append(remoteKeys, payload.New)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case http.MethodGet:
			getCalls++
			http.Error(w, "GET should not be needed to create the first API key", http.StatusInternalServerError)
		case http.MethodPut:
			putCalls++
			http.Error(w, "PUT should not be needed to create the first API key", http.StatusInternalServerError)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer cpa.Close()

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     cpa.URL,
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)

	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "VSCode",
	}, cookies, &created)

	if created.APIKey == "" || created.APIKeyHash == "" {
		t.Fatalf("created API key response is missing key fields: %#v", created)
	}
	if len(remoteKeys) != 1 || remoteKeys[0] != created.APIKey {
		t.Fatalf("remote keys = %#v, want the created key %#v", remoteKeys, created.APIKey)
	}
	if badPatchPayload != "" {
		t.Fatal(badPatchPayload)
	}
	if patchCalls != 1 || getCalls != 0 || putCalls != 0 {
		t.Fatalf("remote call counts patch/get/put = %d/%d/%d, want 1/0/0", patchCalls, getCalls, putCalls)
	}
}

func TestCurrentUserCanCreateGeneratedAndCustomAPIKeys(t *testing.T) {
	fake := &fakeCPAAPIKeys{}
	handler, cookies := newConfiguredAPIKeyTestHandler(t, fake)

	customKey := "legacy-Team_KEY.123"
	custom := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "旧网关迁移",
		"api_key":     customKey,
	}, cookies, &custom)
	if custom.APIKey != customKey || custom.APIKeyHash == "" {
		t.Fatalf("custom key response = %#v, want full custom key", custom)
	}

	generated := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "自动生成",
	}, cookies, &generated)
	if !strings.HasPrefix(generated.APIKey, "sk-") || generated.APIKeyHash == "" {
		t.Fatalf("generated key response = %#v, want generated full key", generated)
	}

	remoteKeys, getCalls, patchCalls := fake.snapshot()
	if len(remoteKeys) != 2 || remoteKeys[0] != customKey || remoteKeys[1] != generated.APIKey {
		t.Fatalf("remote keys = %#v, want custom and generated keys", remoteKeys)
	}
	if getCalls != 1 || patchCalls != 2 {
		t.Fatalf("remote GET/PATCH calls = %d/%d, want 1/2", getCalls, patchCalls)
	}
}

func TestAdminCanCreateGeneratedAndCustomAPIKeysForUser(t *testing.T) {
	fake := &fakeCPAAPIKeys{}
	handler, adminCookies := newConfiguredAPIKeyTestHandler(t, fake)

	var member struct {
		ID int `json:"id"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, adminCookies, &member)
	path := "/api/users/" + strconv.Itoa(member.ID) + "/api-keys/create"

	customKey := "member-existing-key"
	custom := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, path, map[string]any{
		"description": "成员迁移 Key",
		"api_key":     customKey,
	}, adminCookies, &custom)
	if custom.APIKey != customKey || custom.UserID == nil || *custom.UserID != member.ID {
		t.Fatalf("admin custom key response = %#v, want member ownership", custom)
	}

	generated := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, path, map[string]any{
		"description": "成员自动 Key",
	}, adminCookies, &generated)
	if !strings.HasPrefix(generated.APIKey, "sk-") || generated.UserID == nil || *generated.UserID != member.ID {
		t.Fatalf("admin generated key response = %#v, want member ownership", generated)
	}

	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, nil)
	var memberKeys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, memberCookies, &memberKeys)
	if len(memberKeys) != 2 {
		t.Fatalf("member keys = %#v, want 2 admin-created keys", memberKeys)
	}

	requestJSONExpectStatus(t, handler, http.MethodPost, path, map[string]any{
		"description": "越权创建",
	}, memberCookies, http.StatusForbidden)
}

func TestCreateCustomAPIKeyValidation(t *testing.T) {
	fake := &fakeCPAAPIKeys{}
	handler, cookies := newConfiguredAPIKeyTestHandler(t, fake)

	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "empty", body: map[string]any{"description": "Key", "api_key": ""}},
		{name: "null", body: map[string]any{"description": "Key", "api_key": nil}},
		{name: "blank", body: map[string]any{"description": "Key", "api_key": " \t "}},
		{name: "leading whitespace", body: map[string]any{"description": "Key", "api_key": " key"}},
		{name: "trailing whitespace", body: map[string]any{"description": "Key", "api_key": "key "}},
		{name: "internal whitespace", body: map[string]any{"description": "Key", "api_key": "key value"}},
		{name: "control character", body: map[string]any{"description": "Key", "api_key": "key\nvalue"}},
		{name: "key too long", body: map[string]any{"description": "Key", "api_key": strings.Repeat("x", 401)}},
		{name: "description too long", body: map[string]any{"description": strings.Repeat("d", 241), "api_key": "valid-key"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestJSONExpectStatus(t, handler, http.MethodPost, "/api/api-keys", test.body, cookies, http.StatusUnprocessableEntity)
		})
	}

	remoteKeys, getCalls, patchCalls := fake.snapshot()
	if len(remoteKeys) != 0 || getCalls != 0 || patchCalls != 0 {
		t.Fatalf("invalid input reached CPA: keys=%#v GET/PATCH=%d/%d", remoteKeys, getCalls, patchCalls)
	}
}

func TestCreateCustomAPIKeyRejectsLocalAndRemoteDuplicates(t *testing.T) {
	remoteOnlyKey := "remote-existing-key"
	fake := &fakeCPAAPIKeys{keys: []string{remoteOnlyKey}}
	handler, adminCookies := newConfiguredAPIKeyTestHandler(t, fake)

	localKey := "locally-owned-key"
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "管理员 Key",
		"api_key":     localKey,
	}, adminCookies, nil)

	var member struct {
		ID int `json:"id"`
	}
	requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"username": "member",
		"password": "member-password",
		"nickname": "Member",
		"is_admin": false,
	}, adminCookies, &member)
	path := "/api/users/" + strconv.Itoa(member.ID) + "/api-keys/create"
	_, getCallsBeforeDuplicate, patchCallsBeforeDuplicate := fake.snapshot()
	requestJSONExpectStatus(t, handler, http.MethodPost, path, map[string]any{
		"description": "不能转移归属",
		"api_key":     localKey,
	}, adminCookies, http.StatusConflict)
	_, getCallsAfterDuplicate, patchCallsAfterDuplicate := fake.snapshot()
	if getCallsAfterDuplicate != getCallsBeforeDuplicate || patchCallsAfterDuplicate != patchCallsBeforeDuplicate {
		t.Fatalf("local duplicate called CPA: GET %d->%d, PATCH %d->%d", getCallsBeforeDuplicate, getCallsAfterDuplicate, patchCallsBeforeDuplicate, patchCallsAfterDuplicate)
	}

	requestJSONExpectStatus(t, handler, http.MethodPost, path, map[string]any{
		"description": "远端已有 Key",
		"api_key":     remoteOnlyKey,
	}, adminCookies, http.StatusConflict)
	_, getCallsAfterRemoteDuplicate, patchCallsAfterRemoteDuplicate := fake.snapshot()
	if getCallsAfterRemoteDuplicate != getCallsAfterDuplicate+1 || patchCallsAfterRemoteDuplicate != patchCallsAfterDuplicate {
		t.Fatalf("remote duplicate calls GET/PATCH = %d/%d, want %d/%d", getCallsAfterRemoteDuplicate, patchCallsAfterRemoteDuplicate, getCallsAfterDuplicate+1, patchCallsAfterDuplicate)
	}

	memberCookies := requestJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]any{
		"username": "member",
		"password": "member-password",
	}, nil, nil)
	var memberKeys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, memberCookies, &memberKeys)
	if len(memberKeys) != 0 {
		t.Fatalf("duplicate creation changed member ownership: %#v", memberKeys)
	}
	var adminKeys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, adminCookies, &adminKeys)
	if len(adminKeys) != 1 || adminKeys[0].APIKey != localKey {
		t.Fatalf("original admin ownership changed: %#v", adminKeys)
	}
}

func TestCreateAPIKeyRemoteFailureRemovesLocalPlaceholder(t *testing.T) {
	fake := &fakeCPAAPIKeys{failPatch: true}
	handler, cookies := newConfiguredAPIKeyTestHandler(t, fake)

	apiKey := "retry-after-remote-failure"
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "首次失败",
		"api_key":     apiKey,
	}, cookies, http.StatusUnprocessableEntity)
	var keys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &keys)
	if len(keys) != 0 {
		t.Fatalf("remote failure left local API key placeholder: %#v", keys)
	}

	fake.mu.Lock()
	fake.failPatch = false
	fake.mu.Unlock()
	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "重试成功",
		"api_key":     apiKey,
	}, cookies, &created)
	if created.APIKey != apiKey {
		t.Fatalf("retry response = %#v, want original custom key", created)
	}
}

func TestCreateAPIKeyKeepsBindingWhenRemoteWriteSucceededBeforeError(t *testing.T) {
	fake := &fakeCPAAPIKeys{failAfterPatch: true}
	handler, cookies := newConfiguredAPIKeyTestHandler(t, fake)

	apiKey := "written-before-error"
	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
		"description": "响应失败前已写入",
		"api_key":     apiKey,
	}, cookies, &created)
	if created.APIKey != apiKey || created.APIKeyHash == "" {
		t.Fatalf("create response = %#v, want reconciled custom key", created)
	}

	remoteKeys, getCalls, patchCalls := fake.snapshot()
	if len(remoteKeys) != 1 || remoteKeys[0] != apiKey {
		t.Fatalf("remote keys = %#v, want reconciled key", remoteKeys)
	}
	if getCalls != 2 || patchCalls != 1 {
		t.Fatalf("remote GET/PATCH calls = %d/%d, want precheck plus reconciliation and one write", getCalls, patchCalls)
	}

	var keys []apiKeyCreateResponse
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &keys)
	if len(keys) != 1 || keys[0].APIKey != apiKey {
		t.Fatalf("local API keys = %#v, want reconciled binding", keys)
	}
}

func TestAdminCreateAPIKeyRequiresActiveUserWithQuota(t *testing.T) {
	fake := &fakeCPAAPIKeys{}
	handler, adminCookies := newConfiguredAPIKeyTestHandler(t, fake)

	createMember := func(username string) int {
		t.Helper()
		var member struct {
			ID int `json:"id"`
		}
		requestJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
			"username": username,
			"password": "member-password",
			"nickname": username,
			"is_admin": false,
		}, adminCookies, &member)
		return member.ID
	}

	disabledID := createMember("disabled-member")
	requestJSON(t, handler, http.MethodPost, "/api/users/"+strconv.Itoa(disabledID)+"/disable", nil, adminCookies, nil)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/users/"+strconv.Itoa(disabledID)+"/api-keys/create", map[string]any{
		"description": "禁用用户",
	}, adminCookies, http.StatusConflict)

	exhaustedID := createMember("exhausted-member")
	zero := 0
	requestJSON(t, handler, http.MethodPut, "/api/users/"+strconv.Itoa(exhaustedID)+"/quota", map[string]any{
		"lifetime_quota_usd": zero,
	}, adminCookies, nil)
	requestJSONExpectStatus(t, handler, http.MethodPost, "/api/users/"+strconv.Itoa(exhaustedID)+"/api-keys/create", map[string]any{
		"description": "额度耗尽",
	}, adminCookies, http.StatusConflict)

	remoteKeys, getCalls, patchCalls := fake.snapshot()
	if len(remoteKeys) != 0 || getCalls != 0 || patchCalls != 0 {
		t.Fatalf("invalid target creation reached CPA: keys=%#v GET/PATCH=%d/%d", remoteKeys, getCalls, patchCalls)
	}
}
