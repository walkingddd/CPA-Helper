package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type apiKeyTestCPA struct {
	mu                  sync.Mutex
	keys                []string
	methods             []string
	getStatus           int
	patchStatus         int
	putStatus           int
	failPatchAfterWrite bool
	afterPatch          func()
}

func (c *apiKeyTestCPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.URL.Path != "/v0/management/api-keys" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("X-Management-Key") != "test-management-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c.methods = append(c.methods, r.Method)
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		if c.getStatus != 0 {
			w.WriteHeader(c.getStatus)
			return
		}
	case http.MethodPatch:
		if c.patchStatus != 0 {
			w.WriteHeader(c.patchStatus)
			return
		}
		var payload struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Old == "" || payload.New == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if index := slices.Index(c.keys, payload.Old); index >= 0 {
			c.keys[index] = payload.New
		} else {
			c.keys = append(c.keys, payload.New)
		}
		if c.afterPatch != nil {
			c.afterPatch()
		}
		if c.failPatchAfterWrite {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case http.MethodPut:
		if c.putStatus != 0 {
			w.WriteHeader(c.putStatus)
			return
		}
		var keys []string
		if err := json.NewDecoder(r.Body).Decode(&keys); err != nil || keys == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.keys = keys
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"api-keys": c.keys})
}

func (c *apiKeyTestCPA) snapshot() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.keys), slices.Clone(c.methods)
}

func newCustomAPIKeyTestApp(t *testing.T, remote *apiKeyTestCPA) *App {
	t.Helper()
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	cpa := httptest.NewServer(remote)
	t.Cleanup(cpa.Close)
	app, err := NewWithOptions(context.Background(), NewOptions{Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	cfg, err := app.loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Collector.CLIProxyURL = cpa.URL
	cfg.Collector.ManagementKey = "test-management-key"
	cfg.Collector.Enabled = false
	if err := app.saveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	return app
}

func requireAPIKeyErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != status {
		t.Fatalf("error = %v, want application status %d", err, status)
	}
}

func TestCustomAPIKeyCreateAndRotatePreserveBindingAndUsage(t *testing.T) {
	for _, patchStatus := range []int{0, http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(patchStatus), func(t *testing.T) {
			remote := &apiKeyTestCPA{keys: []string{"unrelated-key"}, patchStatus: patchStatus}
			app := newCustomAPIKeyTestApp(t, remote)
			handler := app.Routes()
			cookies := requestJSONForPricingTest(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
				"username": "admin", "password": "test-password", "nickname": "Display name",
			}, nil, nil)
			var created UserApiKeySummary
			requestJSONForPricingTest(t, handler, http.MethodPost, "/api/api-keys", map[string]any{
				"api_key": "  custom-client.key_123  ", "description": "  VSCode  ",
			}, cookies, &created)
			if created.APIKey == nil || *created.APIKey != "custom-client.key_123" || created.APIKeyHash != hashAPIKey(*created.APIKey) || created.Description != "VSCode" {
				t.Fatalf("unexpected custom key: %#v", created)
			}
			if created.UserName == nil || *created.UserName != "admin" {
				t.Fatalf("account key user name = %v, want login username", created.UserName)
			}
			oldRaw := `{"api_key":"custom-client.key_123","request_id":"before-rotation","input_tokens":10}`
			if _, inserted, err := app.saveUsageMessage(context.Background(), []byte(oldRaw)); err != nil || !inserted {
				t.Fatalf("old usage: inserted=%v err=%v", inserted, err)
			}
			var originalStoredRaw string
			if err := app.db.QueryRow(`SELECT raw_json FROM usage_records WHERE request_id = 'before-rotation'`).Scan(&originalStoredRaw); err != nil {
				t.Fatal(err)
			}
			var updated UserApiKeySummary
			requestJSONForPricingTest(t, handler, http.MethodPut, "/api/api-keys/"+created.APIKeyHash, map[string]any{
				"api_key": "replacement-key",
			}, cookies, &updated)
			if updated.APIKey == nil || *updated.APIKey != "replacement-key" || updated.APIKeyHash != hashAPIKey("replacement-key") || updated.Description != "VSCode" {
				t.Fatalf("unexpected replacement: %#v", updated)
			}
			if updated.UserID == nil || created.UserID == nil || *updated.UserID != *created.UserID || updated.CreatedAt == nil || created.CreatedAt == nil || !updated.CreatedAt.Equal(*created.CreatedAt) {
				t.Fatal("rotation changed owner or creation time")
			}
			if _, err := app.getAPIKey(context.Background(), created.APIKeyHash); err == nil {
				t.Fatal("old key binding still exists")
			}
			newRaw := `{"api_key":"replacement-key","request_id":"after-rotation","input_tokens":20}`
			if _, inserted, err := app.saveUsageMessage(context.Background(), []byte(newRaw)); err != nil || !inserted {
				t.Fatalf("new usage: inserted=%v err=%v", inserted, err)
			}
			keys, methodsBeforeRename := remote.snapshot()
			if !slices.Equal(keys, []string{"unrelated-key", "replacement-key"}) {
				t.Fatalf("remote keys = %#v", keys)
			}
			if patchStatus != 0 && !slices.Contains(methodsBeforeRename, http.MethodPut) {
				t.Fatal("unsupported PATCH did not fall back to PUT")
			}
			requestJSONForPricingTest(t, handler, http.MethodPut, "/api/api-keys/"+updated.APIKeyHash, map[string]any{
				"description": "Renamed", "api_key": "   ",
			}, cookies, &updated)
			if *updated.APIKey != "replacement-key" || updated.Description != "Renamed" {
				t.Fatal("description-only update changed the key")
			}
			_, methodsAfterRename := remote.snapshot()
			if !slices.Equal(methodsBeforeRename, methodsAfterRename) {
				t.Fatal("description-only update contacted CPA")
			}
			var snapshots int
			if err := app.db.QueryRow(`SELECT COUNT(*) FROM usage_records WHERE usage_username = 'admin' AND api_key_description = 'VSCode'`).Scan(&snapshots); err != nil || snapshots != 2 {
				t.Fatalf("usage ownership snapshots = %d, err=%v", snapshots, err)
			}
			var storedRaw string
			if err := app.db.QueryRow(`SELECT raw_json FROM usage_records WHERE request_id = 'before-rotation'`).Scan(&storedRaw); err != nil || storedRaw != originalStoredRaw {
				t.Fatalf("historical raw usage changed: %q, err=%v", storedRaw, err)
			}
			var listed []UserApiKeySummary
			requestJSONForPricingTest(t, handler, http.MethodGet, "/api/api-keys", nil, cookies, &listed)
			if len(listed) != 1 || listed[0].APIKeyHash != updated.APIKeyHash {
				t.Fatalf("list after rotation = %#v", listed)
			}
		})
	}
}

func TestCustomAPIKeyConflictsAndOwnership(t *testing.T) {
	remote := &apiKeyTestCPA{keys: []string{"alice-key", "bob-key", "remote-only-key"}}
	app := newCustomAPIKeyTestApp(t, remote)
	ctx := context.Background()
	alice := &AuthUser{ID: seedQuotaTestUser(t, app, "alice"), Username: "alice"}
	bob := seedQuotaTestUser(t, app, "bob")
	seedQuotaTestAPIKey(t, app, alice.ID, "alice-key")
	seedQuotaTestAPIKey(t, app, bob, "bob-key")
	for _, key := range []string{"alice-key", "bob-key", "remote-only-key"} {
		_, err := app.createAPIKeyForUser(ctx, alice.ID, alice.Username, apiKeyPayload{APIKey: key, Description: stringPtr("New description")})
		requireAPIKeyErrorStatus(t, err, http.StatusConflict)
	}
	for _, key := range []string{"bob-key", "remote-only-key"} {
		_, err := app.updateCurrentUserAPIKey(ctx, alice, hashAPIKey("alice-key"), apiKeyPayload{APIKey: key, Description: stringPtr("Changed")})
		requireAPIKeyErrorStatus(t, err, http.StatusConflict)
	}
	_, err := app.updateCurrentUserAPIKey(ctx, alice, hashAPIKey("bob-key"), apiKeyPayload{APIKey: "stolen-key"})
	requireAPIKeyErrorStatus(t, err, http.StatusNotFound)
	keys, methods := remote.snapshot()
	if !slices.Equal(keys, []string{"alice-key", "bob-key", "remote-only-key"}) {
		t.Fatalf("conflict changed remote keys: %#v", keys)
	}
	for _, method := range methods {
		if method != http.MethodGet {
			t.Fatalf("rejected operation wrote remote keys: %s", method)
		}
	}
	for key, owner := range map[string]int{"alice-key": alice.ID, "bob-key": bob} {
		stored, err := app.getAPIKey(ctx, hashAPIKey(key))
		if err != nil || stored.UserID != owner || stored.Description != "VSCode" {
			t.Fatalf("conflict changed local binding: %#v, err=%v", stored, err)
		}
	}
}

func TestCustomAPIKeyConcurrentCreateDoesNotStealBinding(t *testing.T) {
	remote := &apiKeyTestCPA{}
	app := newCustomAPIKeyTestApp(t, remote)
	first := seedQuotaTestUser(t, app, "first")
	second := seedQuotaTestUser(t, app, "second")
	results := make(chan error, 2)
	for _, userID := range []int{first, second} {
		go func(id int) {
			_, err := app.createAPIKeyForUser(context.Background(), id, "test", apiKeyPayload{APIKey: "shared-custom-key", Description: stringPtr("VSCode")})
			results <- err
		}(userID)
	}
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else {
			requireAPIKeyErrorStatus(t, err, http.StatusConflict)
		}
	}
	keys, _ := remote.snapshot()
	if successes != 1 || !slices.Equal(keys, []string{"shared-custom-key"}) {
		t.Fatalf("concurrent creates: successes=%d keys=%#v", successes, keys)
	}
}

func TestCustomAPIKeyInputValidationAndBlankCompatibility(t *testing.T) {
	remote := &apiKeyTestCPA{keys: []string{"existing-key"}}
	app := newCustomAPIKeyTestApp(t, remote)
	ctx := context.Background()
	user := &AuthUser{ID: seedQuotaTestUser(t, app, "member"), Username: "member"}
	seedQuotaTestAPIKey(t, app, user.ID, "existing-key")
	for _, value := range []string{"has space", "has\ttab", "has\nnewline", "has\x00null", "has\x7fdelete", "密钥", strings.Repeat("a", 401)} {
		_, err := app.createAPIKeyForUser(ctx, user.ID, user.Username, apiKeyPayload{APIKey: value, Description: stringPtr("VSCode")})
		requireAPIKeyErrorStatus(t, err, http.StatusUnprocessableEntity)
		_, err = app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("existing-key"), apiKeyPayload{APIKey: value})
		requireAPIKeyErrorStatus(t, err, http.StatusUnprocessableEntity)
	}
	for _, description := range []*string{nil, stringPtr(" ")} {
		_, err := app.createAPIKeyForUser(ctx, user.ID, user.Username, apiKeyPayload{APIKey: "new-key", Description: description})
		requireAPIKeyErrorStatus(t, err, http.StatusUnprocessableEntity)
	}
	_, err := app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("existing-key"), apiKeyPayload{Description: stringPtr(" ")})
	requireAPIKeyErrorStatus(t, err, http.StatusUnprocessableEntity)
	_, methods := remote.snapshot()
	if len(methods) != 0 {
		t.Fatal("invalid input contacted CPA")
	}
	for _, value := range []string{"", " \t ", "existing-key"} {
		updated, err := app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("existing-key"), apiKeyPayload{APIKey: value})
		if err != nil || updated.APIKey == nil || *updated.APIKey != "existing-key" || updated.Description != "VSCode" {
			t.Fatalf("blank/unchanged key lost binding: %#v, err=%v", updated, err)
		}
	}
	for _, value := range []string{"", " \t "} {
		created, err := app.createAPIKeyForUser(ctx, user.ID, user.Username, apiKeyPayload{APIKey: value, Description: stringPtr("Generated")})
		if err != nil || created.APIKey == nil || !strings.HasPrefix(*created.APIKey, generatedAPIKeyPrefix) || len(*created.APIKey) != len(generatedAPIKeyPrefix)+generatedAPIKeyLength {
			t.Fatalf("blank create did not generate: %#v, err=%v", created, err)
		}
	}
	maxKey := strings.Repeat("a", 400)
	created, err := app.createAPIKeyForUser(ctx, user.ID, user.Username, apiKeyPayload{APIKey: maxKey, Description: stringPtr("Maximum length")})
	if err != nil || created.APIKey == nil || *created.APIKey != maxKey {
		t.Fatalf("400-character custom key rejected: %v", err)
	}
}

func TestCustomAPIKeyPausedQuotaAllowsOnlyDescriptionChanges(t *testing.T) {
	remote := &apiKeyTestCPA{}
	app := newCustomAPIKeyTestApp(t, remote)
	ctx := context.Background()
	user := &AuthUser{ID: seedQuotaTestUser(t, app, "member"), Username: "member"}
	seedQuotaTestAPIKey(t, app, user.ID, "paused-key")
	if _, err := app.db.Exec(`UPDATE users SET quota_lifetime_usd = 0, quota_paused_at = ? WHERE id = ?`, dbTime(time.Now()), user.ID); err != nil {
		t.Fatal(err)
	}
	_, err := app.createAPIKeyForUser(ctx, user.ID, user.Username, apiKeyPayload{APIKey: "bypass-key", Description: stringPtr("VSCode")})
	requireAPIKeyErrorStatus(t, err, http.StatusConflict)
	_, err = app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("paused-key"), apiKeyPayload{APIKey: "bypass-key"})
	requireAPIKeyErrorStatus(t, err, http.StatusConflict)
	updated, err := app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("paused-key"), apiKeyPayload{Description: stringPtr("Allowed")})
	if err != nil || updated.Description != "Allowed" || updated.APIKey == nil || *updated.APIKey != "paused-key" {
		t.Fatalf("description-only update failed: %#v, err=%v", updated, err)
	}
	keys, methods := remote.snapshot()
	if len(keys) != 0 || len(methods) != 0 {
		t.Fatal("quota-paused operations contacted CPA")
	}
}

func TestCustomAPIKeySyncFailuresPreserveOriginalBinding(t *testing.T) {
	for _, test := range []struct {
		name                string
		remote              *apiKeyTestCPA
		wantRollbackFailure bool
	}{
		{name: "read failure", remote: &apiKeyTestCPA{getStatus: http.StatusInternalServerError}},
		{name: "patch failure", remote: &apiKeyTestCPA{patchStatus: http.StatusInternalServerError}},
		{name: "response failure after write", remote: &apiKeyTestCPA{failPatchAfterWrite: true}},
		{name: "put fallback failure", remote: &apiKeyTestCPA{patchStatus: http.StatusMethodNotAllowed, putStatus: http.StatusInternalServerError}, wantRollbackFailure: true},
		{name: "rollback failure", remote: &apiKeyTestCPA{failPatchAfterWrite: true, putStatus: http.StatusInternalServerError}, wantRollbackFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := test.remote
			remote.keys = []string{"original-key", "unrelated-key"}
			app := newCustomAPIKeyTestApp(t, remote)
			ctx := context.Background()
			user := &AuthUser{ID: seedQuotaTestUser(t, app, "member"), Username: "member"}
			seedQuotaTestAPIKey(t, app, user.ID, "original-key")
			original, err := app.getAPIKey(ctx, hashAPIKey("original-key"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.updateCurrentUserAPIKey(ctx, user, original.APIKeyHash, apiKeyPayload{APIKey: "replacement-key", Description: stringPtr("Changed")})
			requireAPIKeyErrorStatus(t, err, http.StatusUnprocessableEntity)
			if test.wantRollbackFailure != strings.Contains(err.Error(), "回滚失败") {
				t.Fatalf("rollback failure visibility: %v", err)
			}
			stored, err := app.getAPIKey(ctx, original.APIKeyHash)
			if err != nil || stored.Description != original.Description || *stored.APIKey != *original.APIKey || !stored.UpdatedAt.Equal(*original.UpdatedAt) {
				t.Fatalf("failed sync changed original binding: %#v, err=%v", stored, err)
			}
			if _, err := app.getAPIKey(ctx, hashAPIKey("replacement-key")); err == nil {
				t.Fatal("failed sync retained new binding")
			}
			keys, _ := remote.snapshot()
			if !test.wantRollbackFailure && !slices.Equal(keys, []string{"original-key", "unrelated-key"}) {
				t.Fatalf("failed sync did not restore CPA: %#v", keys)
			}
		})
	}
}

func TestCustomAPIKeyCreationFailureRestoresEmptyRemoteList(t *testing.T) {
	remote := &apiKeyTestCPA{failPatchAfterWrite: true}
	app := newCustomAPIKeyTestApp(t, remote)
	userID := seedQuotaTestUser(t, app, "member")
	_, err := app.createAPIKeyForUser(context.Background(), userID, "member", apiKeyPayload{APIKey: "new-key", Description: stringPtr("VSCode")})
	requireAPIKeyErrorStatus(t, err, http.StatusUnprocessableEntity)
	if strings.Contains(err.Error(), "回滚失败") {
		t.Fatalf("empty-list rollback failed: %v", err)
	}
	keys, _ := remote.snapshot()
	if len(keys) != 0 {
		t.Fatalf("failed creation left remote key: %#v", keys)
	}
	var count int
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM user_api_keys`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed creation left local binding: count=%d err=%v", count, err)
	}
}

func TestCustomAPIKeyCancelledRequestStillRollsBackCPA(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remote := &apiKeyTestCPA{keys: []string{"original-key"}, afterPatch: cancel}
	app := newCustomAPIKeyTestApp(t, remote)
	user := &AuthUser{ID: seedQuotaTestUser(t, app, "member"), Username: "member"}
	seedQuotaTestAPIKey(t, app, user.ID, "original-key")
	_, err := app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("original-key"), apiKeyPayload{APIKey: "replacement-key"})
	if err == nil {
		t.Fatal("cancelled request succeeded")
	}
	keys, _ := remote.snapshot()
	if !slices.Equal(keys, []string{"original-key"}) {
		t.Fatalf("cancelled request did not restore CPA: %#v", keys)
	}
	if _, err := app.getAPIKey(context.Background(), hashAPIKey("original-key")); err != nil {
		t.Fatalf("cancelled request lost original binding: %v", err)
	}
}

func TestCustomAPIKeyDatabaseFailureDoesNotChangeCPA(t *testing.T) {
	remote := &apiKeyTestCPA{keys: []string{"original-key"}}
	app := newCustomAPIKeyTestApp(t, remote)
	ctx := context.Background()
	user := &AuthUser{ID: seedQuotaTestUser(t, app, "member"), Username: "member"}
	seedQuotaTestAPIKey(t, app, user.ID, "original-key")
	if _, err := app.db.Exec(`CREATE TRIGGER fail_key_user_update BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("original-key"), apiKeyPayload{APIKey: "replacement-key", Description: stringPtr("Changed")})
	if err == nil {
		t.Fatal("database failure was swallowed")
	}
	stored, err := app.getAPIKey(ctx, hashAPIKey("original-key"))
	if err != nil || stored.Description != "VSCode" {
		t.Fatalf("database failure changed original binding: %#v, err=%v", stored, err)
	}
	keys, methods := remote.snapshot()
	if len(methods) != 0 || !slices.Equal(keys, []string{"original-key"}) {
		t.Fatal("database failure wrote to CPA")
	}
}

func TestCustomAPIKeyCommitFailureRollsBackLocalAndRemoteChanges(t *testing.T) {
	remote := &apiKeyTestCPA{keys: []string{"original-key", "unrelated-key"}}
	app := newCustomAPIKeyTestApp(t, remote)
	ctx := context.Background()
	user := &AuthUser{ID: seedQuotaTestUser(t, app, "member"), Username: "member"}
	seedQuotaTestAPIKey(t, app, user.ID, "original-key")
	// The deferred foreign-key error happens only at COMMIT, after CPA accepted
	// the replacement. This also verifies that the failed SQLite connection is
	// not returned to the pool with its uncommitted changes still visible.
	for _, statement := range []string{
		`CREATE TABLE key_commit_failure (user_id INTEGER REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER fail_key_commit AFTER UPDATE ON users BEGIN INSERT INTO key_commit_failure (user_id) VALUES (-1); END`,
	} {
		if _, err := app.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_, err := app.updateCurrentUserAPIKey(ctx, user, hashAPIKey("original-key"), apiKeyPayload{APIKey: "replacement-key", Description: stringPtr("Changed")})
	if err == nil {
		t.Fatal("commit failure was swallowed")
	}
	stored, err := app.getAPIKey(ctx, hashAPIKey("original-key"))
	if err != nil || stored.Description != "VSCode" {
		t.Fatalf("commit failure lost original binding: %#v, err=%v", stored, err)
	}
	if _, err := app.getAPIKey(ctx, hashAPIKey("replacement-key")); err == nil {
		t.Fatal("commit failure retained new binding")
	}
	keys, methods := remote.snapshot()
	if !slices.Equal(keys, []string{"original-key", "unrelated-key"}) || !slices.Equal(methods, []string{http.MethodGet, http.MethodPatch, http.MethodPut}) {
		t.Fatalf("commit failure did not compensate CPA: keys=%#v methods=%#v", keys, methods)
	}
}
