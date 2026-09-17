package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

// generatedAPIKeySecretLength mirrors the backend's random-secret length; a generated key is
// `<prefix>-<secret>`, so its total length is len(prefix)+1+52.
const generatedAPIKeySecretLength = 52

// newAPIKeyPrefixTestApp boots the app with an admin session and a permissive fake CPA that
// accepts key sync, so API keys can actually be created.
func newAPIKeyPrefixTestApp(t *testing.T) (http.Handler, []*http.Cookie, func()) {
	t.Helper()
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	var mu sync.Mutex
	remoteKeys := []string{}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/api-keys" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPatch:
			var payload struct {
				New string `json:"new"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			remoteKeys = append(remoteKeys, payload.New)
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		case http.MethodPut:
			var keys []string
			_ = json.NewDecoder(r.Body).Decode(&keys)
			remoteKeys = keys
			_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": remoteKeys})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	app, err := backendApp.New()
	if err != nil {
		cpa.Close()
		t.Fatalf("New() failed: %v", err)
	}
	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin", "password": "test-password", "nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url": cpa.URL, "management_key": "test-management-key", "collector_enabled": false,
	}, cookies, nil)
	return handler, cookies, func() { app.Close(); cpa.Close() }
}

func createAPIKeyForTest(t *testing.T, handler http.Handler, cookies []*http.Cookie) apiKeyCreateResponse {
	t.Helper()
	created := apiKeyCreateResponse{}
	requestJSON(t, handler, http.MethodPost, "/api/api-keys", map[string]any{"description": "prefix test"}, cookies, &created)
	if created.APIKey == "" || created.APIKeyHash == "" {
		t.Fatalf("create returned empty key/hash: %+v", created)
	}
	return created
}

func settingsPrefixForTest(t *testing.T, handler http.Handler, cookies []*http.Cookie) string {
	t.Helper()
	var settings struct {
		APIKeyPrefix string `json:"api_key_prefix"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/settings", nil, cookies, &settings)
	return settings.APIKeyPrefix
}

func TestAPIKeyPrefixDefaultsToSkAndKeepsLegacyShape(t *testing.T) {
	handler, cookies, cleanup := newAPIKeyPrefixTestApp(t)
	defer cleanup()
	if got := settingsPrefixForTest(t, handler, cookies); got != "sk" {
		t.Fatalf("default api_key_prefix = %q, want sk", got)
	}
	created := createAPIKeyForTest(t, handler, cookies)
	if !strings.HasPrefix(created.APIKey, "sk-") {
		t.Fatalf("default key %q must start with sk-", created.APIKey)
	}
	if len(created.APIKey) != len("sk-")+generatedAPIKeySecretLength {
		t.Fatalf("default key length = %d, want %d (legacy shape preserved)", len(created.APIKey), len("sk-")+generatedAPIKeySecretLength)
	}
}

func TestAPIKeyPrefixConfiguredIsUsedForNewKeysOnly(t *testing.T) {
	handler, cookies, cleanup := newAPIKeyPrefixTestApp(t)
	defer cleanup()
	legacy := createAPIKeyForTest(t, handler, cookies)

	var saved struct {
		APIKeyPrefix string `json:"api_key_prefix"`
	}
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{"api_key_prefix": " sk-myteam "}, cookies, &saved)
	if saved.APIKeyPrefix != "sk-myteam" {
		t.Fatalf("saved api_key_prefix = %q, want sk-myteam (trimmed)", saved.APIKeyPrefix)
	}
	if got := settingsPrefixForTest(t, handler, cookies); got != "sk-myteam" {
		t.Fatalf("api_key_prefix did not persist: %q", got)
	}

	created := createAPIKeyForTest(t, handler, cookies)
	if !strings.HasPrefix(created.APIKey, "sk-myteam-") || strings.HasPrefix(created.APIKey, "sk-myteam--") {
		t.Fatalf("new key %q must be sk-myteam-<secret> with exactly one joining dash", created.APIKey)
	}
	if len(created.APIKey) != len("sk-myteam-")+generatedAPIKeySecretLength {
		t.Fatalf("new key length = %d, want %d", len(created.APIKey), len("sk-myteam-")+generatedAPIKeySecretLength)
	}

	// Existing keys are never rewritten: the legacy key is still listed with its original value.
	var keys []struct {
		APIKey     string `json:"api_key"`
		APIKeyHash string `json:"api_key_hash"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &keys)
	foundLegacy, foundNew := false, false
	for _, key := range keys {
		if key.APIKeyHash == legacy.APIKeyHash && strings.HasPrefix(key.APIKey, "sk-") && !strings.HasPrefix(key.APIKey, "sk-myteam-") {
			foundLegacy = true
		}
		if key.APIKeyHash == created.APIKeyHash && strings.HasPrefix(key.APIKey, "sk-myteam-") {
			foundNew = true
		}
	}
	if !foundLegacy || !foundNew {
		t.Fatalf("expected both the untouched legacy sk- key and the new sk-myteam- key; got %+v", keys)
	}

	// Blank resets to the default.
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{"api_key_prefix": ""}, cookies, &saved)
	if saved.APIKeyPrefix != "sk" {
		t.Fatalf("blank api_key_prefix should reset to sk, got %q", saved.APIKeyPrefix)
	}
}

func TestAPIKeyPrefixRejectsMalformedValues(t *testing.T) {
	handler, cookies, cleanup := newAPIKeyPrefixTestApp(t)
	defer cleanup()
	for _, bad := range []string{
		"-sk",                           // leading dash
		"sk-",                           // trailing dash (the generator adds the joining dash)
		"sk myteam",                     // whitespace
		"sk/myteam",                     // slash
		"sk.myteam",                     // dot
		"sk：myteam",                     // non-ASCII
		strings.Repeat("a", 33),         // too long
		"sk-" + strings.Repeat("b", 30), // too long (33)
	} {
		requestJSONExpectStatus(t, handler, http.MethodPut, "/api/settings", map[string]any{"api_key_prefix": bad}, cookies, http.StatusUnprocessableEntity)
	}
	// The rejections must not have changed the stored value.
	if got := settingsPrefixForTest(t, handler, cookies); got != "sk" {
		t.Fatalf("rejected values must not persist; api_key_prefix = %q", got)
	}
	// Valid edge cases: single char, underscore, digits, 32 chars.
	for _, ok := range []string{"a", "team_42", "SK-MyTeam_2", strings.Repeat("z", 32)} {
		requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{"api_key_prefix": ok}, cookies, nil)
		if got := settingsPrefixForTest(t, handler, cookies); got != ok {
			t.Fatalf("valid prefix %q not stored, got %q", ok, got)
		}
	}
}
