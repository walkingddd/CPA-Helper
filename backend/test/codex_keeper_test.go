package test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	backendApp "cpa-helper/backend/internal/app"
)

type keeperStatusResponse struct {
	Running       bool     `json:"running"`
	DaemonRunning bool     `json:"daemon_running"`
	Logs          []string `json:"logs"`
}

type keeperAccountsResponse struct {
	Items []struct {
		Name           string  `json:"name"`
		AccountType    *string `json:"account_type"`
		Disabled       bool    `json:"disabled"`
		Priority       *int    `json:"priority"`
		PrimaryResetAt *string `json:"primary_reset_at"`
		LastStatusCode *int    `json:"last_status_code"`
		LastError      *string `json:"last_error"`
		LatestAction   *string `json:"latest_action"`
		LastCheckedAt  *string `json:"last_checked_at"`
		LastHealthyAt  *string `json:"last_healthy_at"`
	} `json:"items"`
}

type collectorStatusTimeResponse struct {
	LastPollAt    *string `json:"last_poll_at"`
	LastSuccessAt *string `json:"last_success_at"`
}

type userTimesResponse []struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	APIKeys   []struct {
		CreatedAt *string `json:"created_at"`
		UpdatedAt *string `json:"updated_at"`
	} `json:"api_keys"`
}

type modelPriceTimesResponse []struct {
	LastSyncedAt *string `json:"last_synced_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type usageRecordsTimeResponse struct {
	Start *string `json:"start"`
	End   *string `json:"end"`
	Items []struct {
		Timestamp string `json:"timestamp"`
	} `json:"items"`
}

func TestKeeperAutoStartReportsDaemonRunning(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	app, err := backendApp.New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer func() {
		if app != nil {
			app.Close()
		}
	}()

	handler := app.Routes()
	cookies := requestJSON(t, handler, http.MethodPost, "/api/auth/setup", map[string]any{
		"username": "admin",
		"password": "test-password",
		"nickname": "Admin",
	}, nil, nil)
	requestJSON(t, handler, http.MethodPut, "/api/settings", map[string]any{
		"cliaproxy_url":     "http://127.0.0.1:1",
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":     "0 0 29 2 *",
		"auto_start_daemon": true,
	}, cookies, nil)
	app.Close()
	app = nil

	app, err = backendApp.New()
	if err != nil {
		t.Fatalf("New() with auto-start enabled failed: %v", err)
	}
	handler = app.Routes()

	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if !status.DaemonRunning {
		t.Fatal("daemon_running = false, want true after auto-start")
	}
	if status.Running {
		t.Fatal("running = true, want false while daemon is only waiting for the next cron tick")
	}

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/stop", nil, cookies, nil)
	status = keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if status.DaemonRunning {
		t.Fatal("daemon_running = true, want false after stop")
	}
}

func TestKeeperLogsUseStandardFileFormatAndCanBeCleared(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

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
		"cliaproxy_url":     "http://127.0.0.1:1",
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron": "0 0 29 2 *",
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/start", nil, cookies, nil)

	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if len(status.Logs) == 0 {
		t.Fatal("status logs are empty, want daemon start log")
	}
	assertStandardKeeperLogLine(t, status.Logs[len(status.Logs)-1])

	logPath := filepath.Join(dataDir, "logs", "codex-keeper-"+time.Now().Format("2006-01-02")+".log")
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read keeper log file: %v", err)
	}
	fileLines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(fileLines) == 0 {
		t.Fatal("keeper log file is empty")
	}
	assertStandardKeeperLogLine(t, strings.TrimSpace(fileLines[len(fileLines)-1]))

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/logs/clear", nil, cookies, nil)
	matches, err := filepath.Glob(filepath.Join(dataDir, "logs", "codex-keeper-*.log"))
	if err != nil {
		t.Fatalf("glob keeper log files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("keeper log files after clear = %v, want none", matches)
	}
}

func TestKeeperStatusRestoresRecentLogFileLines(t *testing.T) {
	dataDir := t.TempDir()
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	expected := `time=2026-05-16T16:03:05.571+08:00 level=INFO msg="demo.json: 巡检正常，类型 free" component=codex_keeper`
	if err := os.WriteFile(filepath.Join(logDir, "codex-keeper-2026-05-16.log"), []byte(expected+"\n"), 0o644); err != nil {
		t.Fatalf("write keeper log fixture: %v", err)
	}
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

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
	status := keeperStatusResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
	if len(status.Logs) != 1 || status.Logs[0] != expected {
		t.Fatalf("restored logs = %#v, want %#v", status.Logs, []string{expected})
	}
}

func TestKeeperRunMaintainsSystemPriorityRules(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	authNames := []string{
		"free-null.json",
		"free-quota-null.json",
		"plus-wrong.json",
		"manual-quota-high.json",
		"manual-high.json",
		"manual-low.json",
	}
	usagePercents := map[string]int{
		"free-quota-null.json":   100,
		"manual-quota-high.json": 100,
	}
	authDetails := map[string]map[string]any{
		"free-null.json": {
			"name":         "free-null.json",
			"type":         "codex",
			"email":        "user001@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     nil,
			"access_token": "test-token",
		},
		"free-quota-null.json": {
			"name":         "free-quota-null.json",
			"type":         "codex",
			"email":        "user005@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     nil,
			"access_token": "test-token",
		},
		"plus-wrong.json": {
			"name":         "plus-wrong.json",
			"type":         "codex",
			"email":        "user002@example.com",
			"account_type": "plus",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"manual-quota-high.json": {
			"name":         "manual-quota-high.json",
			"type":         "codex",
			"email":        "user006@example.com",
			"account_type": "plus",
			"disabled":     false,
			"priority":     100,
			"access_token": "test-token",
		},
		"manual-high.json": {
			"name":         "manual-high.json",
			"type":         "codex",
			"email":        "user003@example.com",
			"account_type": "plus",
			"disabled":     false,
			"priority":     21,
			"access_token": "test-token",
		},
		"manual-low.json": {
			"name":         "manual-low.json",
			"type":         "codex",
			"email":        "user004@example.com",
			"account_type": "team",
			"disabled":     false,
			"priority":     -2,
			"access_token": "test-token",
		},
	}
	var mu sync.Mutex
	patches := map[string][]int{}

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			files := make([]map[string]any, 0, len(authNames))
			for _, name := range authNames {
				files = append(files, map[string]any{"name": name, "type": "codex"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			detail, ok := authDetails[r.URL.Query().Get("name")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(detail)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string `json:"auth_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			usedPercent := usagePercents[payload.AuthIndex]
			if usedPercent == 0 {
				usedPercent = 10
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        usedPercent,
							"reset_after_seconds": 3600,
						},
					},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			var payload struct {
				Name     string `json:"name"`
				Priority *int   `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Priority == nil {
				http.Error(w, "priority is required", http.StatusBadRequest)
				return
			}
			mu.Lock()
			patches[payload.Name] = append(patches[payload.Name], *payload.Priority)
			if detail, ok := authDetails[payload.Name]; ok {
				detail["priority"] = *payload.Priority
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"quota_threshold":     100,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/run-once", nil, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, len(authNames))

	expectedPriorities := map[string]int{
		"free-null.json":         0,
		"free-quota-null.json":   -1,
		"plus-wrong.json":        4,
		"manual-quota-high.json": -1,
		"manual-high.json":       21,
		"manual-low.json":        -2,
	}
	assertKeeperPriorities(t, response, expectedPriorities)

	mu.Lock()
	expectedPatches := map[string][]int{
		"free-quota-null.json":   {-1},
		"plus-wrong.json":        {4},
		"manual-quota-high.json": {-1},
	}
	assertKeeperPriorityPatchesLocked(t, patches, expectedPatches)
	mu.Unlock()

	usagePercents["manual-quota-high.json"] = 10
	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/run-once", nil, cookies, nil)
	response = waitForKeeperAccounts(t, handler, cookies, len(authNames))
	expectedPriorities["manual-quota-high.json"] = 100
	assertKeeperPriorities(t, response, expectedPriorities)

	mu.Lock()
	expectedPatches["manual-quota-high.json"] = []int{-1, 100}
	assertKeeperPriorityPatchesLocked(t, patches, expectedPatches)
	mu.Unlock()
}

func TestKeeperRefreshAccountsOnlyProcessesSelectedAuths(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	authDetails := map[string]map[string]any{
		"refresh-me.json": {
			"name":         "refresh-me.json",
			"type":         "codex",
			"email":        "refresh@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"skip-me.json": {
			"name":         "skip-me.json",
			"type":         "codex",
			"email":        "skip@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
	}
	var mu sync.Mutex
	usageCalls := map[string]int{}

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"name": "refresh-me.json", "type": "codex"},
					{"name": "skip-me.json", "type": "codex"},
					{"name": "not-codex.json", "type": "openai"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			detail, ok := authDetails[r.URL.Query().Get("name")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(detail)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string `json:"auth_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			usageCalls[payload.AuthIndex]++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"plan_type": "free",
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        10,
							"reset_after_seconds": 3600,
						},
					},
				},
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
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"quota_threshold":     100,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{"refresh-me.json"},
	}, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, 1)
	if response.Items[0].Name != "refresh-me.json" {
		t.Fatalf("refreshed account = %q, want refresh-me.json", response.Items[0].Name)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := usageCalls["refresh-me.json"]; got != 1 {
		t.Fatalf("refresh-me usage calls = %d, want 1", got)
	}
	if got := usageCalls["skip-me.json"]; got != 0 {
		t.Fatalf("skip-me usage calls = %d, want 0", got)
	}
}

func TestKeeperRefreshAccountsUpdatesStateWithoutRemoteMutation(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

	authDetails := map[string]map[string]any{
		"quota-high.json": {
			"name":         "quota-high.json",
			"type":         "codex",
			"email":        "quota@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "test-token",
		},
		"bad-token.json": {
			"name":         "bad-token.json",
			"type":         "codex",
			"email":        "bad@example.com",
			"account_type": "free",
			"disabled":     false,
			"priority":     0,
			"access_token": "bad-token",
		},
	}
	var mu sync.Mutex
	statusPatchCount := 0
	priorityPatchCount := 0

	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"name": "quota-high.json", "type": "codex"},
					{"name": "bad-token.json", "type": "codex"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			detail, ok := authDetails[r.URL.Query().Get("name")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(detail)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			var payload struct {
				AuthIndex string `json:"auth_index"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			statusCode := 200
			usedPercent := 100
			if payload.AuthIndex == "bad-token.json" {
				statusCode = 401
				usedPercent = 0
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": statusCode,
				"body": map[string]any{
					"plan_type": "free",
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        usedPercent,
							"reset_after_seconds": 3600,
						},
					},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/status":
			mu.Lock()
			statusPatchCount++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			mu.Lock()
			priorityPatchCount++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		"management_key":    "test-management-key",
		"collector_enabled": false,
	}, cookies, nil)
	requestJSON(t, handler, http.MethodPut, "/api/codex-keeper/settings", map[string]any{
		"schedule_cron":       "0 0 29 2 *",
		"dry_run":             false,
		"quota_threshold":     50,
		"worker_threads":      1,
		"cpa_timeout_seconds": 1,
	}, cookies, nil)

	requestJSON(t, handler, http.MethodPost, "/api/codex-keeper/accounts/refresh", map[string]any{
		"auth_names": []string{"quota-high.json", "bad-token.json"},
	}, cookies, nil)
	response := waitForKeeperAccounts(t, handler, cookies, 2)

	items := map[string]struct {
		Disabled       bool
		Priority       *int
		LastStatusCode *int
		LastError      *string
	}{}
	for _, item := range response.Items {
		items[item.Name] = struct {
			Disabled       bool
			Priority       *int
			LastStatusCode *int
			LastError      *string
		}{
			Disabled:       item.Disabled,
			Priority:       item.Priority,
			LastStatusCode: item.LastStatusCode,
			LastError:      item.LastError,
		}
	}

	quota := items["quota-high.json"]
	if quota.Priority == nil || *quota.Priority != 0 {
		t.Fatalf("quota-high priority = %v, want unchanged 0", quota.Priority)
	}
	if quota.LastError != nil {
		t.Fatalf("quota-high last_error = %v, want nil", *quota.LastError)
	}
	bad := items["bad-token.json"]
	if bad.Disabled {
		t.Fatal("bad-token disabled = true, want false when refresh only updates state")
	}
	if bad.LastStatusCode == nil || *bad.LastStatusCode != 401 {
		t.Fatalf("bad-token last_status_code = %v, want 401", bad.LastStatusCode)
	}
	if bad.LastError == nil || !strings.Contains(*bad.LastError, "凭证不可用") {
		t.Fatalf("bad-token last_error = %v, want credential error", bad.LastError)
	}

	mu.Lock()
	defer mu.Unlock()
	if statusPatchCount != 0 {
		t.Fatalf("status patch count = %d, want 0", statusPatchCount)
	}
	if priorityPatchCount != 0 {
		t.Fatalf("priority patch count = %d, want 0", priorityPatchCount)
	}
}

func TestKeeperAccountsReturnBeijingOffsetTimeStrings(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

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

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, disabled, primary_reset_at, last_checked_at,
			last_healthy_at, created_at, updated_at
		) VALUES (?, ?, 0, ?, ?, ?, ?, ?)
	`,
		"sample.json",
		"user001@example.com",
		"2026-05-14 01:02:03.654321",
		"2026-05-13 12:00:01.123456",
		"2026-05-13 12:00:02.123456",
		"2026-05-13 11:59:58.000000",
		"2026-05-13 11:59:58.000000",
	)
	if err != nil {
		t.Fatalf("insert keeper account state: %v", err)
	}

	response := keeperAccountsResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
	if len(response.Items) != 1 {
		t.Fatalf("accounts length = %d, want 1", len(response.Items))
	}
	item := response.Items[0]
	if item.Name != "sample.json" {
		t.Fatalf("account name = %q, want sample.json", item.Name)
	}
	if item.Priority == nil || *item.Priority != 0 {
		t.Fatalf("priority = %v, want 0 when stored value is NULL", item.Priority)
	}
	if got := stringPtrValue(item.LastCheckedAt); got != "2026-05-13T12:00:01+08:00" {
		t.Fatalf("last_checked_at = %q, want Beijing offset time", got)
	}
	if got := stringPtrValue(item.LastHealthyAt); got != "2026-05-13T12:00:02+08:00" {
		t.Fatalf("last_healthy_at = %q, want Beijing offset time", got)
	}
	if got := stringPtrValue(item.PrimaryResetAt); got != "2026-05-14T01:02:03+08:00" {
		t.Fatalf("primary_reset_at = %q, want Beijing offset time", got)
	}
}

func TestDBBackedAPIsReturnBeijingOffsetTimeStrings(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CPA_HELPER_DATA_DIR", dataDir)

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

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "cpa_helper.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		UPDATE users
		SET created_at = ?, updated_at = ?
		WHERE id = 1
	`, "2026-05-06 16:04:17.286273", "2026-05-09 20:44:15.891099")
	if err != nil {
		t.Fatalf("update user times: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO user_api_keys (api_key_hash, user_id, api_key, description, created_at, updated_at)
		VALUES (?, 1, ?, ?, ?, ?)
	`, "hash-for-time-test", "sk-test", "time key", "2026-05-08 22:04:51.729598", "2026-05-09 20:44:15.891099")
	if err != nil {
		t.Fatalf("insert api key times: %v", err)
	}
	_, err = db.Exec(`
		UPDATE collector_state
		SET last_poll_at = ?, last_success_at = ?, updated_at = ?
		WHERE id = 1
	`, "2026-05-13 12:34:56.123456", "2026-05-13 12:35:01.123456", "2026-05-13 12:35:02.123456")
	if err != nil {
		t.Fatalf("update collector times: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cached_usd_per_million, reasoning_usd_per_million, source,
			source_model, auto_synced, last_synced_at, updated_at
		) VALUES (?, ?, 1, 2, 0, 0, 'manual', NULL, 0, ?, ?)
	`, "openai", "gpt-time-test", "2026-05-10 08:09:10.123456", "2026-05-10 08:09:11.123456")
	if err != nil {
		t.Fatalf("insert model price times: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO usage_records (
			created_at, timestamp, usage_username, api_key_description, provider,
			model, endpoint, source, request_id, auth, latency_ms, failed,
			input_tokens, output_tokens, cached_tokens, reasoning_tokens,
			total_tokens, dedupe_key, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, 2, 0, 0, 3, ?, '{}')
	`, "2026-05-13T12:47:53+08:00", "2026-05-13T12:47:44+08:00", "admin", "time key", "openai", "gpt-time-test", "/v1/chat/completions", "test", "req-time", "auth", 123.0, "dedupe-time-test")
	if err != nil {
		t.Fatalf("insert usage record times: %v", err)
	}

	collector := collectorStatusTimeResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/collector/status", nil, cookies, &collector)
	if got := stringPtrValue(collector.LastPollAt); got != "2026-05-13T12:34:56+08:00" {
		t.Fatalf("collector last_poll_at = %q, want Beijing offset string", got)
	}
	if got := stringPtrValue(collector.LastSuccessAt); got != "2026-05-13T12:35:01+08:00" {
		t.Fatalf("collector last_success_at = %q, want Beijing offset string", got)
	}

	users := userTimesResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/users", nil, cookies, &users)
	if len(users) == 0 {
		t.Fatal("users response is empty")
	}
	if got := users[0].CreatedAt; got != "2026-05-06T16:04:17+08:00" {
		t.Fatalf("user created_at = %q, want Beijing offset string", got)
	}
	if got := users[0].UpdatedAt; got != "2026-05-09T20:44:15+08:00" {
		t.Fatalf("user updated_at = %q, want Beijing offset string", got)
	}
	if len(users[0].APIKeys) == 0 {
		t.Fatal("user api_keys response is empty")
	}
	if got := stringPtrValue(users[0].APIKeys[0].CreatedAt); got != "2026-05-08T22:04:51+08:00" {
		t.Fatalf("api key created_at = %q, want Beijing offset string", got)
	}

	prices := modelPriceTimesResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/model-prices", nil, cookies, &prices)
	if len(prices) == 0 {
		t.Fatal("model prices response is empty")
	}
	if got := stringPtrValue(prices[0].LastSyncedAt); got != "2026-05-10T08:09:10+08:00" {
		t.Fatalf("model price last_synced_at = %q, want Beijing offset string", got)
	}
	if got := prices[0].UpdatedAt; got != "2026-05-10T08:09:11+08:00" {
		t.Fatalf("model price updated_at = %q, want Beijing offset string", got)
	}

	records := usageRecordsTimeResponse{}
	requestJSON(t, handler, http.MethodGet, "/api/usage/records?scope=admin&page=1&page_size=1&start=2026-05-13T00:00:00&end=2026-05-14T00:00:00", nil, cookies, &records)
	if len(records.Items) != 1 {
		t.Fatalf("usage records length = %d, want 1", len(records.Items))
	}
	if got := records.Items[0].Timestamp; got != "2026-05-13T12:47:44+08:00" {
		t.Fatalf("usage timestamp = %q, want Beijing offset string", got)
	}
}

func assertStandardKeeperLogLine(t *testing.T, line string) {
	t.Helper()
	pattern := regexp.MustCompile(`^time=\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}\+08:00 level=INFO msg=.+ component=codex_keeper$`)
	if !pattern.MatchString(line) {
		t.Fatalf("keeper log line %q does not match standard format", line)
	}
}

func waitForKeeperAccounts(t *testing.T, handler http.Handler, cookies []*http.Cookie, expected int) keeperAccountsResponse {
	t.Helper()
	response := keeperAccountsResponse{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response = keeperAccountsResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/accounts", nil, cookies, &response)
		status := keeperStatusResponse{}
		requestJSON(t, handler, http.MethodGet, "/api/codex-keeper/status", nil, cookies, &status)
		if len(response.Items) == expected && !status.Running {
			return response
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("accounts length = %d, want %d after keeper run completed", len(response.Items), expected)
	return response
}

func assertKeeperPriorities(t *testing.T, response keeperAccountsResponse, expected map[string]int) {
	t.Helper()
	priorities := map[string]int{}
	for _, item := range response.Items {
		if item.Priority == nil {
			t.Fatalf("%s priority is nil, want maintained priority", item.Name)
		}
		priorities[item.Name] = *item.Priority
	}
	for name, want := range expected {
		if got := priorities[name]; got != want {
			t.Fatalf("%s priority = %d, want %d", name, got, want)
		}
	}
}

func assertKeeperPriorityPatchesLocked(t *testing.T, patches map[string][]int, expected map[string][]int) {
	t.Helper()
	if len(patches) != len(expected) {
		t.Fatalf("priority patches = %#v, want %#v", patches, expected)
	}
	for name, want := range expected {
		got := patches[name]
		if len(got) != len(want) {
			t.Fatalf("%s patched priorities = %#v, want %#v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s patched priorities = %#v, want %#v", name, got, want)
			}
		}
	}
}

func requestJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body any,
	cookies []*http.Cookie,
	target any,
) []*http.Cookie {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, path, recorder.Code, recorder.Body.String())
	}
	if target != nil {
		if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
			t.Fatalf("decode %s %s response: %v", method, path, err)
		}
	}
	return append(cookies, recorder.Result().Cookies()...)
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
