package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	keeperUsageURL         = "https://chatgpt.com/backend-api/wham/usage"
	keeperLogFilePrefix    = "codex-keeper-"
	keeperLogComponent     = "codex_keeper"
	keeperLogRetainedFiles = 3
	keeperMaxInMemoryLogs  = 300
)

type KeeperRunner struct {
	app            *App
	mu             sync.Mutex
	daemonStop     chan struct{}
	daemonDone     chan struct{}
	running        bool
	state          string
	detail         string
	mode           *string
	lastStartedAt  *time.Time
	lastFinishedAt *time.Time
	stats          keeperStats
	logs           []string
}

type keeperStats struct {
	Total            int `json:"total"`
	Healthy          int `json:"healthy"`
	StatusDisabled   int `json:"status_disabled"`
	StatusEnabled    int `json:"status_enabled"`
	PriorityDegraded int `json:"priority_degraded"`
	PriorityRestored int `json:"priority_restored"`
	Skipped          int `json:"skipped"`
	NetworkError     int `json:"network_error"`
}

type keeperStatusResponse struct {
	Running        bool        `json:"running"`
	DaemonRunning  bool        `json:"daemon_running"`
	State          string      `json:"state"`
	Detail         string      `json:"detail"`
	Mode           *string     `json:"mode"`
	LastStartedAt  *string     `json:"last_started_at"`
	LastFinishedAt *string     `json:"last_finished_at"`
	Stats          keeperStats `json:"stats"`
	Logs           []string    `json:"logs"`
}

type keeperPriorityRule struct {
	AccountType string `json:"account_type"`
	Priority    int    `json:"priority"`
}

type keeperSettingsUpdateRequest struct {
	ScheduleCron        *string              `json:"schedule_cron"`
	QuotaThreshold      *int                 `json:"quota_threshold"`
	UsageTimeoutSeconds *int                 `json:"usage_timeout_seconds"`
	CPATimeoutSeconds   *int                 `json:"cpa_timeout_seconds"`
	MaxRetries          *int                 `json:"max_retries"`
	WorkerThreads       *int                 `json:"worker_threads"`
	DryRun              *bool                `json:"dry_run"`
	AutoStartDaemon     *bool                `json:"auto_start_daemon"`
	PriorityRules       []keeperPriorityRule `json:"priority_rules"`
}

type keeperCronPreviewRequest struct {
	ScheduleCron string `json:"schedule_cron"`
}

type keeperBulkDeleteRequest struct {
	AuthNames []string `json:"auth_names"`
}

type keeperRefreshAccountsRequest struct {
	AuthNames []string `json:"auth_names"`
}

type keeperPriorityUpdateRequest struct {
	Priority int `json:"priority"`
}

type keeperAccount struct {
	Name                 string     `json:"name"`
	Email                *string    `json:"email"`
	AccountType          *string    `json:"account_type"`
	Disabled             bool       `json:"disabled"`
	Priority             *int       `json:"priority"`
	PrimaryUsedPercent   *int       `json:"primary_used_percent"`
	SecondaryUsedPercent *int       `json:"secondary_used_percent"`
	PrimaryResetAt       *time.Time `json:"primary_reset_at"`
	SecondaryResetAt     *time.Time `json:"secondary_reset_at"`
	QuotaThreshold       *int       `json:"quota_threshold"`
	LastStatusCode       *int       `json:"last_status_code"`
	LastError            *string    `json:"last_error"`
	LatestAction         *string    `json:"latest_action"`
	LastCheckedAt        *time.Time `json:"last_checked_at"`
	LastHealthyAt        *time.Time `json:"last_healthy_at"`
}

type keeperAccountResponse struct {
	Name                 string  `json:"name"`
	Email                *string `json:"email"`
	AccountType          *string `json:"account_type"`
	Disabled             bool    `json:"disabled"`
	Priority             *int    `json:"priority"`
	PrimaryUsedPercent   *int    `json:"primary_used_percent"`
	SecondaryUsedPercent *int    `json:"secondary_used_percent"`
	PrimaryResetAt       *string `json:"primary_reset_at"`
	SecondaryResetAt     *string `json:"secondary_reset_at"`
	QuotaThreshold       *int    `json:"quota_threshold"`
	LastStatusCode       *int    `json:"last_status_code"`
	LastError            *string `json:"last_error"`
	LatestAction         *string `json:"latest_action"`
	LastCheckedAt        *string `json:"last_checked_at"`
	LastHealthyAt        *string `json:"last_healthy_at"`
}

type keeperAuthState struct {
	keeperAccount
	RestorePriority *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type keeperUsageInfo struct {
	PlanType             string
	PrimaryUsedPercent   int
	SecondaryUsedPercent *int
	PrimaryResetAt       *time.Time
	SecondaryResetAt     *time.Time
}

type keeperHTTPResult struct {
	StatusCode *int
	JSONData   map[string]any
	Brief      string
	Error      string
}

type keeperAccountResult struct {
	Name                 string
	Result               string
	Email                *string
	AccountType          *string
	Priority             *int
	RestorePriority      *int
	ClearRestorePriority bool
	Disabled             *bool
	PrimaryUsedPercent   *int
	SecondaryUsedPercent *int
	PrimaryResetAt       *time.Time
	SecondaryResetAt     *time.Time
	QuotaThreshold       *int
	LastStatusCode       *int
	LastError            *string
	LatestAction         *string
	CheckedAt            time.Time
}

func NewKeeperRunner(app *App) *KeeperRunner {
	return &KeeperRunner{
		app:    app,
		state:  "idle",
		detail: "尚未运行",
		logs:   []string{},
	}
}

func (r *KeeperRunner) LoadPersistedState(ctx context.Context) {
	logs, logErr := r.app.loadKeeperLogLines(keeperMaxInMemoryLogs)
	if logErr != nil {
		log.Printf("restore codex keeper logs failed: %v", logErr)
	}
	run, err := r.app.latestKeeperRun(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = logs
	if err != nil || run == nil {
		return
	}
	r.state = run.State
	r.detail = run.Detail
	r.mode = run.Mode
	r.lastStartedAt = run.StartedAt
	r.lastFinishedAt = run.FinishedAt
	r.stats = run.Stats
}

func (r *KeeperRunner) StartAutoIfConfigured() {
	cfg, err := r.app.loadConfig(context.Background())
	if err != nil {
		r.log("读取 Codex Keeper 自动启动配置失败：" + err.Error())
		return
	}
	if cfg.CodexKeeper.AutoStartDaemon && strings.TrimSpace(cfg.Collector.ManagementKey) != "" {
		if err := r.StartDaemon(); err != nil {
			r.log("启动 Codex Keeper 自动巡检失败：" + err.Error())
		}
	}
}

func (r *KeeperRunner) StartOnce() error {
	if !r.markRunning("once") {
		return conflictError("Codex Keeper 正在运行")
	}
	go r.run("once")
	return nil
}

func (r *KeeperRunner) StartAccounts(authNames []string) error {
	names, err := normalizeKeeperAuthNames(authNames)
	if err != nil {
		return err
	}
	if !r.markRunning("accounts") {
		return conflictError("Codex Keeper 正在运行")
	}
	go r.runAccounts("accounts", names)
	return nil
}

func (r *KeeperRunner) StartDaemon() error {
	cfg, err := r.app.loadConfig(context.Background())
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return validationError("管理密钥未设置，无法运行 Codex Keeper")
	}
	if _, _, err := nextRunTimes(cfg.CodexKeeper.ScheduleCron, 1, time.Now()); err != nil {
		return err
	}

	r.mu.Lock()
	if r.daemonRunningLocked() {
		r.mu.Unlock()
		return nil
	}
	r.daemonStop = make(chan struct{})
	r.daemonDone = make(chan struct{})
	stop := r.daemonStop
	done := r.daemonDone
	r.mu.Unlock()

	go r.daemonLoop(stop, done)
	r.log("Codex Keeper 已开始按计划自动巡检")
	return nil
}

func (r *KeeperRunner) Stop() {
	r.mu.Lock()
	stop := r.daemonStop
	done := r.daemonDone
	if stop == nil || done == nil {
		r.mu.Unlock()
		return
	}
	select {
	case <-done:
		r.daemonStop = nil
		r.daemonDone = nil
		r.mu.Unlock()
		return
	default:
	}
	select {
	case <-stop:
	default:
		close(stop)
	}
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	if r.daemonStop == stop {
		r.daemonStop = nil
	}
	if r.daemonDone == done {
		r.daemonDone = nil
	}
	r.mu.Unlock()
	r.log("Codex Keeper 已停止自动巡检")
}

func (r *KeeperRunner) ClearLogs() {
	r.mu.Lock()
	r.logs = []string{}
	r.mu.Unlock()
	if err := r.app.clearKeeperLogFiles(); err != nil {
		log.Printf("clear codex keeper log files failed: %v", err)
	}
}

func (r *KeeperRunner) Status() keeperStatusResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	logs := append([]string{}, r.logs...)
	return keeperStatusResponse{
		Running:        r.running,
		DaemonRunning:  r.daemonRunningLocked(),
		State:          r.state,
		Detail:         r.detail,
		Mode:           cloneStringPtr(r.mode),
		LastStartedAt:  apiDateTimePtr(r.lastStartedAt),
		LastFinishedAt: apiDateTimePtr(r.lastFinishedAt),
		Stats:          r.stats,
		Logs:           logs,
	}
}

func (r *KeeperRunner) daemonRunningLocked() bool {
	if r.daemonDone == nil {
		return false
	}
	select {
	case <-r.daemonDone:
		return false
	default:
		return true
	}
}

func (r *KeeperRunner) daemonLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		cfg, err := r.app.loadConfig(context.Background())
		if err != nil {
			r.log("读取 Codex Keeper 配置失败：" + err.Error())
			if waitForStop(stop, time.Minute) {
				return
			}
			continue
		}
		times, _, err := nextRunTimes(cfg.CodexKeeper.ScheduleCron, 1, time.Now().In(appTimeLocation))
		if err != nil {
			r.log("Codex Keeper 定时表达式无效：" + err.Error())
			if waitForStop(stop, time.Minute) {
				return
			}
			continue
		}
		delay := time.Until(times[0])
		if delay < 0 {
			delay = 0
		}
		r.log("下一轮计划：" + times[0].In(appTimeLocation).Format("2006-01-02 15:04:05"))
		if waitForStop(stop, delay) {
			return
		}
		if r.markRunning("daemon") {
			r.run("daemon")
		}
	}
}

func (r *KeeperRunner) markRunning(mode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return false
	}
	now := time.Now().In(appTimeLocation)
	runningMode := mode
	r.running = true
	r.state = "running"
	if mode == "accounts" {
		r.detail = "正在刷新 Codex 账号"
	} else {
		r.detail = "正在巡检 Codex 账号"
	}
	r.mode = &runningMode
	r.lastStartedAt = &now
	r.lastFinishedAt = nil
	r.stats = keeperStats{}
	return true
}

func (r *KeeperRunner) run(mode string) {
	r.runAccounts(mode, nil)
}

func (r *KeeperRunner) runAccounts(mode string, authNames []string) {
	stats, detail, err := r.app.executeKeeperRunForAccounts(context.Background(), mode, authNames, r.log)
	finishedAt := time.Now().In(appTimeLocation)
	logMessage := detail
	r.mu.Lock()
	r.running = false
	r.lastFinishedAt = &finishedAt
	r.stats = stats
	if err != nil {
		r.state = "failed"
		r.detail = err.Error()
		logMessage = "巡检失败：" + err.Error()
	} else {
		r.state = "completed"
		r.detail = detail
	}
	r.mu.Unlock()
	if strings.TrimSpace(logMessage) != "" {
		r.log(logMessage)
	}
}

func (r *KeeperRunner) log(message string) {
	timestamp := time.Now().In(appTimeLocation)
	line := formatKeeperLogLine(timestamp, message)
	r.mu.Lock()
	r.logs = appendKeeperLog(r.logs, line)
	r.mu.Unlock()
	if err := r.app.appendKeeperLogFile(timestamp, line); err != nil {
		log.Printf("write codex keeper log failed: %v", err)
	}
}

func appendKeeperLog(logs []string, line string) []string {
	logs = append(logs, line)
	if len(logs) > keeperMaxInMemoryLogs {
		logs = logs[len(logs)-keeperMaxInMemoryLogs:]
	}
	return logs
}

func formatKeeperLogLine(timestamp time.Time, message string) string {
	var output strings.Builder
	handler := slog.NewTextHandler(&output, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, timestamp.In(appTimeLocation).Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return attr
		},
	})
	record := slog.NewRecord(timestamp.In(appTimeLocation), slog.LevelInfo, message, 0)
	record.AddAttrs(slog.String("component", keeperLogComponent))
	_ = handler.Handle(context.Background(), record)
	return strings.TrimSuffix(output.String(), "\n")
}

type keeperLogFile struct {
	path string
	date time.Time
}

func (a *App) keeperLogDir() string {
	return filepath.Join(a.dataDir, "logs")
}

func (a *App) appendKeeperLogFile(timestamp time.Time, line string) error {
	dir := a.keeperLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, keeperLogFilePrefix+timestamp.In(appTimeLocation).Format("2006-01-02")+".log")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(line + "\n")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return a.pruneKeeperLogFiles()
}

func (a *App) loadKeeperLogLines(limit int) ([]string, error) {
	files, err := a.keeperLogFiles()
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].date.Before(files[j].date)
	})
	if len(files) > keeperLogRetainedFiles {
		files = files[len(files)-keeperLogRetainedFiles:]
	}
	lines := []string{}
	for _, file := range files {
		handle, err := os.Open(file.path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(handle)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lines = appendKeeperLog(lines, line)
		}
		scanErr := scanner.Err()
		closeErr := handle.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if limit > 0 && len(lines) > limit {
		return lines[len(lines)-limit:], nil
	}
	return lines, nil
}

func (a *App) pruneKeeperLogFiles() error {
	files, err := a.keeperLogFiles()
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].date.After(files[j].date)
	})
	for index, file := range files {
		if index < keeperLogRetainedFiles {
			continue
		}
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *App) clearKeeperLogFiles() error {
	files, err := a.keeperLogFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (a *App) keeperLogFiles() ([]keeperLogFile, error) {
	dir := a.keeperLogDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := []keeperLogFile{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, keeperLogFilePrefix) || !strings.HasSuffix(name, ".log") {
			continue
		}
		dateText := strings.TrimSuffix(strings.TrimPrefix(name, keeperLogFilePrefix), ".log")
		date, err := time.ParseInLocation("2006-01-02", dateText, appTimeLocation)
		if err != nil {
			continue
		}
		files = append(files, keeperLogFile{
			path: filepath.Join(dir, name),
			date: date,
		})
	}
	return files, nil
}

func (a *App) handleCodexKeeper(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	parts := splitPath(r.URL.Path, "/api/codex-keeper/")
	if len(parts) == 0 {
		return notFoundError("Not Found")
	}
	switch {
	case len(parts) == 1 && parts[0] == "settings":
		if r.Method == http.MethodGet {
			cfg, err := a.loadConfig(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, keeperSettingsResponse(cfg))
			return nil
		}
		if r.Method == http.MethodPut {
			return a.updateKeeperSettings(w, r)
		}
		return methodNotAllowed()
	case len(parts) == 2 && parts[0] == "schedule" && parts[1] == "preview":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload keeperCronPreviewRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		times, normalized, err := nextRunTimes(payload.ScheduleCron, 5, time.Now())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"schedule_cron": normalized, "next_run_times": apiDateTimes(times)})
		return nil
	case len(parts) == 1 && parts[0] == "status":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, a.keeper.Status())
		return nil
	case len(parts) == 1 && parts[0] == "accounts":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		accounts, err := a.listKeeperAccounts(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": keeperAccountResponses(accounts)})
		return nil
	case len(parts) == 1 && parts[0] == "run-once":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		if err := a.keeper.StartOnce(); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return nil
	case len(parts) == 1 && parts[0] == "start":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		if err := a.keeper.StartDaemon(); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return nil
	case len(parts) == 1 && parts[0] == "stop":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		a.keeper.Stop()
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
		return nil
	case len(parts) == 2 && parts[0] == "logs" && parts[1] == "clear":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		a.keeper.ClearLogs()
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
		return nil
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "bulk-delete":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.bulkDeleteKeeperAccounts(w, r)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "refresh":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload keeperRefreshAccountsRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		if err := a.keeper.StartAccounts(payload.AuthNames); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
		return nil
	case len(parts) == 3 && parts[0] == "accounts" && (parts[2] == "enable" || parts[2] == "disable"):
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		authName, err := url.PathUnescape(parts[1])
		if err != nil {
			return validationError("账号名称无效")
		}
		disabled := parts[2] == "disable"
		if err := a.setKeeperAccountDisabled(r.Context(), authName, disabled); err != nil {
			return err
		}
		if disabled {
			writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
		} else {
			writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
		}
		return nil
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodDelete:
		authName, err := url.PathUnescape(parts[1])
		if err != nil {
			return validationError("账号名称无效")
		}
		if err := a.deleteKeeperAccount(r.Context(), authName); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return nil
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "priority":
		if err := requireMethod(r, http.MethodPatch); err != nil {
			return err
		}
		authName, err := url.PathUnescape(parts[1])
		if err != nil {
			return validationError("账号名称无效")
		}
		var payload keeperPriorityUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		if err := a.updateKeeperAccountPriority(r.Context(), authName, payload.Priority); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return nil
	default:
		return notFoundError("Not Found")
	}
}

func keeperSettingsResponse(cfg AppConfig) map[string]any {
	times, normalized, err := nextRunTimes(cfg.CodexKeeper.ScheduleCron, 5, time.Now())
	if err != nil {
		normalized = cfg.CodexKeeper.ScheduleCron
		times = []time.Time{}
	}
	return map[string]any{
		"cliaproxy_url":         cfg.Collector.CLIProxyURL,
		"management_key_set":    strings.TrimSpace(cfg.Collector.ManagementKey) != "",
		"schedule_cron":         normalized,
		"next_run_times":        apiDateTimes(times),
		"quota_threshold":       cfg.CodexKeeper.QuotaThreshold,
		"usage_timeout_seconds": cfg.CodexKeeper.UsageTimeoutSeconds,
		"cpa_timeout_seconds":   cfg.CodexKeeper.CPATimeoutSeconds,
		"max_retries":           cfg.CodexKeeper.MaxRetries,
		"worker_threads":        cfg.CodexKeeper.WorkerThreads,
		"dry_run":               cfg.CodexKeeper.DryRun,
		"auto_start_daemon":     cfg.CodexKeeper.AutoStartDaemon,
		"priority_rules":        sortedPriorityRules(cfg.CodexKeeperPriorityRule),
	}
}

func keeperAccountResponses(accounts []keeperAccount) []keeperAccountResponse {
	responses := make([]keeperAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		responses = append(responses, keeperAccountResponse{
			Name:                 account.Name,
			Email:                account.Email,
			AccountType:          account.AccountType,
			Disabled:             account.Disabled,
			Priority:             keeperDisplayPriority(account.Priority),
			PrimaryUsedPercent:   account.PrimaryUsedPercent,
			SecondaryUsedPercent: account.SecondaryUsedPercent,
			PrimaryResetAt:       apiDateTimePtr(account.PrimaryResetAt),
			SecondaryResetAt:     apiDateTimePtr(account.SecondaryResetAt),
			QuotaThreshold:       account.QuotaThreshold,
			LastStatusCode:       account.LastStatusCode,
			LastError:            account.LastError,
			LatestAction:         account.LatestAction,
			LastCheckedAt:        apiDateTimePtr(account.LastCheckedAt),
			LastHealthyAt:        apiDateTimePtr(account.LastHealthyAt),
		})
	}
	return responses
}

func (a *App) updateKeeperSettings(w http.ResponseWriter, r *http.Request) error {
	var payload keeperSettingsUpdateRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	if payload.ScheduleCron != nil {
		_, normalized, err := nextRunTimes(*payload.ScheduleCron, 5, time.Now())
		if err != nil {
			return err
		}
		cfg.CodexKeeper.ScheduleCron = normalized
	}
	if payload.QuotaThreshold != nil {
		if *payload.QuotaThreshold < 0 || *payload.QuotaThreshold > 100 {
			return validationError("quota_threshold 超出范围")
		}
		cfg.CodexKeeper.QuotaThreshold = *payload.QuotaThreshold
	}
	if payload.UsageTimeoutSeconds != nil {
		if *payload.UsageTimeoutSeconds < 1 {
			return validationError("usage_timeout_seconds 不能小于 1")
		}
		cfg.CodexKeeper.UsageTimeoutSeconds = *payload.UsageTimeoutSeconds
	}
	if payload.CPATimeoutSeconds != nil {
		if *payload.CPATimeoutSeconds < 1 {
			return validationError("cpa_timeout_seconds 不能小于 1")
		}
		cfg.CodexKeeper.CPATimeoutSeconds = *payload.CPATimeoutSeconds
	}
	if payload.MaxRetries != nil {
		if *payload.MaxRetries < 0 || *payload.MaxRetries > 5 {
			return validationError("max_retries 超出范围")
		}
		cfg.CodexKeeper.MaxRetries = *payload.MaxRetries
	}
	if payload.WorkerThreads != nil {
		if *payload.WorkerThreads < 1 || *payload.WorkerThreads > 64 {
			return validationError("worker_threads 超出范围")
		}
		cfg.CodexKeeper.WorkerThreads = *payload.WorkerThreads
	}
	if payload.DryRun != nil {
		cfg.CodexKeeper.DryRun = *payload.DryRun
	}
	if payload.AutoStartDaemon != nil {
		cfg.CodexKeeper.AutoStartDaemon = *payload.AutoStartDaemon
	}
	if payload.PriorityRules != nil {
		rules := map[string]int{}
		for _, item := range payload.PriorityRules {
			key := strings.ToLower(strings.TrimSpace(item.AccountType))
			if key == "" {
				return validationError("账号类型不能为空")
			}
			if item.Priority < 0 || item.Priority > 20 {
				return validationError("priority 超出范围")
			}
			rules[key] = item.Priority
		}
		cfg.CodexKeeperPriorityRule = normalizePriorityRules(rules)
	}
	if err := a.saveConfig(r.Context(), cfg); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, keeperSettingsResponse(cfg))
	return nil
}

func (a *App) executeKeeperRun(ctx context.Context, mode string, logFn func(string)) (keeperStats, string, error) {
	return a.executeKeeperRunForAccounts(ctx, mode, nil, logFn)
}

func (a *App) executeKeeperRunForAccounts(ctx context.Context, mode string, authNames []string, logFn func(string)) (keeperStats, string, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return keeperStats{}, "", err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return keeperStats{}, "", validationError("管理密钥未设置，无法运行 Codex Keeper")
	}
	runID, err := a.createKeeperRun(ctx, mode)
	if err != nil {
		return keeperStats{}, "", err
	}
	targetNames, err := normalizeOptionalKeeperAuthNames(authNames)
	if err != nil {
		_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), keeperStats{})
		return keeperStats{}, "", err
	}
	targetSet := map[string]bool{}
	for _, name := range targetNames {
		targetSet[name] = true
	}
	if len(targetSet) > 0 {
		logFn(fmt.Sprintf("开始刷新 %d 个 Codex 账号", len(targetSet)))
	} else {
		logFn("开始 Codex 账号巡检")
	}
	stats := keeperStats{}
	detail := "巡检完成"
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
		return stats, "", err
	}
	filtered := make([]map[string]any, 0, len(authFiles))
	for _, item := range authFiles {
		if keeperString(item["type"]) != "codex" {
			continue
		}
		name := keeperString(item["name"])
		if len(targetSet) == 0 || targetSet[name] {
			filtered = append(filtered, item)
		}
	}
	stats.Total = len(filtered)
	if len(filtered) == 0 {
		if len(targetSet) > 0 {
			detail = "未发现指定 Codex auth file"
		} else {
			detail = "未发现 Codex auth file"
		}
		_ = a.finishKeeperRun(ctx, runID, "completed", detail, stats)
		return stats, detail, nil
	}
	for _, item := range filtered {
		result := a.processKeeperAuth(ctx, cfg, item, logFn, mode == "accounts")
		a.mergeKeeperStats(&stats, result)
		if err := a.recordKeeperRunAccount(ctx, runID, result); err != nil {
			logFn("写入巡检账号历史失败：" + err.Error())
		}
	}
	if len(targetSet) > 0 {
		detail = fmt.Sprintf("账号刷新完成：健康 %d，凭证异常 %d，网络错误 %d", stats.Healthy, stats.StatusDisabled, stats.NetworkError)
	} else {
		detail = fmt.Sprintf("巡检完成：健康 %d，坏凭证禁用 %d，优先级降级 %d，网络错误 %d", stats.Healthy, stats.StatusDisabled, stats.PriorityDegraded, stats.NetworkError)
	}
	_ = a.finishKeeperRun(ctx, runID, "completed", detail, stats)
	return stats, detail, nil
}

func (a *App) processKeeperAuth(ctx context.Context, cfg AppConfig, authInfo map[string]any, logFn func(string), stateOnly bool) keeperAccountResult {
	now := time.Now().In(appTimeLocation)
	name := keeperString(authInfo["name"])
	if name == "" {
		name = "unknown"
	}
	result := keeperAccountResult{Name: name, Result: "skipped", CheckedAt: now}
	detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
	if err != nil {
		message := "读取 auth file 详情失败：" + err.Error()
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + message)
		return result
	}
	if detail == nil {
		message := "读取 auth file 详情失败"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	merged := mergeKeeperObjects(authInfo, detail)
	result.Email = keeperStringPtr(merged["email"], merged["account_email"], merged["user_email"])
	result.Priority = keeperIntPtr(merged["priority"])
	disabled := keeperBool(merged["disabled"])
	result.Disabled = &disabled
	result.AccountType = accountTypeFromKeeperDetail(merged, nil)
	if disabled {
		result.Result = "disabled"
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	if keeperString(merged["access_token"]) == "" {
		message := "缺少 access token"
		action := "刷新发现凭证不可用：" + message
		if !stateOnly && !cfg.CodexKeeper.DryRun {
			if err := a.setKeeperRemoteDisabled(ctx, cfg, name, true); err != nil {
				message = "禁用坏凭证失败：" + err.Error()
				result.LastError = &message
				result.Result = "network_error"
				_ = a.upsertKeeperState(ctx, result)
				return result
			}
			_ = a.setKeeperRemotePriority(ctx, cfg, name, nil)
			disabled = true
			result.Disabled = &disabled
			action = "禁用凭证：" + message
		} else if !stateOnly {
			action = "模拟禁用：" + message
		}
		result.Result = "status_disabled"
		result.LastError = &message
		result.LatestAction = &action
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
	}

	usageResult := a.checkKeeperUsage(ctx, cfg, merged)
	if usageResult.StatusCode == nil {
		message := "网络检测失败：" + usageResult.Error
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + message)
		return result
	}
	result.LastStatusCode = usageResult.StatusCode
	if isBadKeeperCredential(usageResult) {
		message := fmt.Sprintf("凭证不可用：HTTP %d", *usageResult.StatusCode)
		if usageResult.Brief != "" {
			message += "，" + usageResult.Brief
		}
		action := "刷新发现凭证不可用：" + message
		if !stateOnly && !cfg.CodexKeeper.DryRun {
			if err := a.setKeeperRemoteDisabled(ctx, cfg, name, true); err != nil {
				message = "禁用坏凭证失败：" + err.Error()
				result.Result = "network_error"
				result.LastError = &message
				_ = a.upsertKeeperState(ctx, result)
				return result
			}
			_ = a.setKeeperRemotePriority(ctx, cfg, name, nil)
			disabled = true
			result.Disabled = &disabled
			action = "禁用凭证：" + message
		} else if !stateOnly {
			action = "模拟禁用：" + message
		}
		result.Result = "status_disabled"
		result.LastError = &message
		result.LatestAction = &action
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
	}
	if *usageResult.StatusCode < 200 || *usageResult.StatusCode >= 300 {
		message := fmt.Sprintf("usage 检测失败：HTTP %d", *usageResult.StatusCode)
		if usageResult.Brief != "" {
			message += "，" + usageResult.Brief
		}
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	usage := parseKeeperUsageInfo(usageResult.JSONData)
	result.AccountType = accountTypeFromKeeperDetail(merged, &usage)
	result.PrimaryUsedPercent = &usage.PrimaryUsedPercent
	result.SecondaryUsedPercent = usage.SecondaryUsedPercent
	result.PrimaryResetAt = usage.PrimaryResetAt
	result.SecondaryResetAt = usage.SecondaryResetAt
	result.QuotaThreshold = &cfg.CodexKeeper.QuotaThreshold
	result.Result = "healthy"

	if stateOnly {
		accountType := "unknown"
		if result.AccountType != nil && strings.TrimSpace(*result.AccountType) != "" {
			accountType = *result.AccountType
		}
		action := fmt.Sprintf("刷新完成，类型 %s", accountType)
		result.LatestAction = &action
		result.LastError = nil
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
	}

	var restorePriority *int
	if state, err := a.getKeeperState(ctx, name); err == nil {
		restorePriority = state.RestorePriority
	}
	action := a.applyKeeperPriorityPolicy(ctx, cfg, name, result.AccountType, result.Priority, restorePriority, usage)
	if action != nil {
		result.LatestAction = &action.Message
		if action.Result == "priority_degraded" {
			result.Result = "priority_degraded"
			result.Priority = action.Priority
			result.RestorePriority = action.RestorePriority
		}
		if action.Result == "priority_restored" {
			result.Result = "priority_restored"
			result.Priority = action.Priority
			result.ClearRestorePriority = true
		}
		logFn(name + ": " + action.Message)
	} else {
		accountType := "unknown"
		if result.AccountType != nil && strings.TrimSpace(*result.AccountType) != "" {
			accountType = *result.AccountType
		}
		logFn(fmt.Sprintf("%s: 巡检正常，类型 %s", name, accountType))
	}
	if result.Priority == nil || *result.Priority != -1 {
		result.ClearRestorePriority = true
	}
	result.LastError = nil
	_ = a.upsertKeeperState(ctx, result)
	return result
}

type keeperPriorityPolicyAction struct {
	Message         string
	Result          string
	Priority        *int
	RestorePriority *int
}

func (a *App) applyKeeperPriorityPolicy(ctx context.Context, cfg AppConfig, name string, accountType *string, priority *int, restorePriority *int, usage keeperUsageInfo) *keeperPriorityPolicyAction {
	quotaReached := usage.PrimaryUsedPercent >= cfg.CodexKeeper.QuotaThreshold ||
		(usage.SecondaryUsedPercent != nil && *usage.SecondaryUsedPercent >= cfg.CodexKeeper.QuotaThreshold)
	currentPriority := keeperEffectivePriority(priority)
	next := keeperPriorityForType(accountType, cfg.CodexKeeperPriorityRule)
	if quotaReached {
		if currentPriority <= -1 {
			return nil
		}
		restoreTo := restorePriority
		if restoreTo == nil {
			restoreTo = next
		}
		if currentPriority > 20 {
			restoreTo = &currentPriority
		}
		if restoreTo == nil {
			restoreTo = &currentPriority
		}
		message := fmt.Sprintf("降为低优先级：额度使用率达到阈值 %d%%", cfg.CodexKeeper.QuotaThreshold)
		if cfg.CodexKeeper.DryRun {
			message = "模拟" + message
			low := -1
			return &keeperPriorityPolicyAction{Message: message, Result: "priority_degraded", Priority: &low, RestorePriority: restoreTo}
		}
		low := -1
		if err := a.setKeeperRemotePriority(ctx, cfg, name, &low); err != nil {
			message = "写入低优先级失败：" + err.Error()
			return &keeperPriorityPolicyAction{Message: message}
		}
		return &keeperPriorityPolicyAction{Message: message, Result: "priority_degraded", Priority: &low, RestorePriority: restoreTo}
	}
	if currentPriority == -1 {
		restoreTo := restorePriority
		if restoreTo == nil {
			restoreTo = next
		}
		if restoreTo == nil {
			zero := 0
			restoreTo = &zero
		}
		message := fmt.Sprintf("恢复优先级：priority %d", *restoreTo)
		if cfg.CodexKeeper.DryRun {
			message = "模拟" + message
			return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: restoreTo}
		}
		if err := a.setKeeperRemotePriority(ctx, cfg, name, restoreTo); err != nil {
			message = "恢复优先级失败：" + err.Error()
			return &keeperPriorityPolicyAction{Message: message}
		}
		return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: restoreTo}
	}
	if currentPriority < -1 || currentPriority > 20 {
		return nil
	}
	if next == nil {
		return nil
	}
	if currentPriority != *next {
		message := fmt.Sprintf("应用类型优先级：%s -> priority %d", valueOr(accountType, "unknown"), *next)
		if cfg.CodexKeeper.DryRun {
			message = "模拟" + message
			return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: next}
		}
		if err := a.setKeeperRemotePriority(ctx, cfg, name, next); err != nil {
			message = "写入类型优先级失败：" + err.Error()
			return &keeperPriorityPolicyAction{Message: message}
		}
		return &keeperPriorityPolicyAction{Message: message, Result: "priority_restored", Priority: next}
	}
	return nil
}

func keeperEffectivePriority(priority *int) int {
	if priority == nil {
		return 0
	}
	return *priority
}

func keeperDisplayPriority(priority *int) *int {
	if priority != nil {
		return priority
	}
	zero := 0
	return &zero
}

func (a *App) mergeKeeperStats(stats *keeperStats, result keeperAccountResult) {
	switch result.Result {
	case "healthy":
		stats.Healthy++
	case "status_disabled":
		stats.StatusDisabled++
	case "status_enabled":
		stats.StatusEnabled++
	case "priority_degraded":
		stats.PriorityDegraded++
	case "priority_restored":
		stats.PriorityRestored++
	case "network_error":
		stats.NetworkError++
	default:
		stats.Skipped++
	}
}

func (a *App) listKeeperRemoteAuthFiles(ctx context.Context, cfg AppConfig) ([]map[string]any, error) {
	_, payload, err := a.keeperRequest(ctx, cfg, http.MethodGet, "/v0/management/auth-files", nil, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, validationError("读取 auth files 失败：响应不是有效 JSON")
	}
	return extractKeeperObjects(raw, []string{"files", "items", "data", "value"}), nil
}

func (a *App) getKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string) (map[string]any, error) {
	query := url.Values{"name": []string{name}}
	response, payload, err := a.keeperRequest(ctx, cfg, http.MethodGet, "/v0/management/auth-files/download", query, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, validationError("读取 auth file 详情失败：响应不是有效 JSON")
	}
	return raw, nil
}

func (a *App) setKeeperRemoteDisabled(ctx context.Context, cfg AppConfig, name string, disabled bool) error {
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/status", nil, map[string]any{"name": name, "disabled": disabled}, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) setKeeperRemotePriority(ctx context.Context, cfg AppConfig, name string, priority *int) error {
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/fields", nil, map[string]any{"name": name, "priority": priority}, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) deleteKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string) error {
	query := url.Values{"name": []string{name}}
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodDelete, "/v0/management/auth-files", query, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) keeperRequest(ctx context.Context, cfg AppConfig, method, path string, query url.Values, body any, timeout time.Duration) (*http.Response, []byte, error) {
	response, payload, err := doJSON(ctx, httpClient(timeout), method, makeURL(cfg.Collector.CLIProxyURL, path, query), managementHeaders(cfg.Collector.ManagementKey), body)
	if err != nil {
		return nil, nil, validationError("CLIProxyAPI 管理请求失败：" + err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, payload, validationError(fmt.Sprintf("CLIProxyAPI 管理请求失败：HTTP %d", response.StatusCode))
	}
	return response, payload, nil
}

func (a *App) checkKeeperUsage(ctx context.Context, cfg AppConfig, detail map[string]any) keeperHTTPResult {
	authIndex := keeperAuthIndex(detail)
	header := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "codex_cli_rs/0.76.0",
	}
	if accountID := keeperString(detail["account_id"]); accountID != "" {
		header["Chatgpt-Account-Id"] = accountID
	}
	body := map[string]any{
		"auth_index": authIndex,
		"method":     "GET",
		"url":        keeperUsageURL,
		"header":     header,
		"data":       "",
	}
	response, payload, err := a.keeperRequest(ctx, cfg, http.MethodPost, "/v0/management/api-call", nil, body, time.Duration(cfg.CodexKeeper.UsageTimeoutSeconds)*time.Second)
	if err != nil {
		return keeperHTTPResult{Error: err.Error()}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return keeperHTTPResult{Error: fmt.Sprintf("api-call 管理请求失败：HTTP %d", response.StatusCode), Brief: briefPayload(payload)}
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return keeperHTTPResult{Error: "api-call 响应不是有效 JSON"}
	}
	statusCode := keeperIntPtr(raw["status_code"], raw["statusCode"])
	if statusCode == nil {
		return keeperHTTPResult{Error: "api-call 响应缺少 status_code"}
	}
	bodyJSON := keeperBodyJSON(raw["body"])
	return keeperHTTPResult{
		StatusCode: statusCode,
		JSONData:   bodyJSON,
		Brief:      briefAny(raw["body"]),
	}
}

func (a *App) listKeeperAccounts(ctx context.Context) ([]keeperAccount, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email, account_type, disabled, priority, primary_used_percent,
		       secondary_used_percent, CAST(primary_reset_at AS TEXT), CAST(secondary_reset_at AS TEXT), quota_threshold,
		       last_status_code, last_error, latest_action, CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT),
		       restore_priority, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
		FROM codex_keeper_auth_states
		ORDER BY COALESCE(email, ''), auth_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []keeperAccount{}
	for rows.Next() {
		state, err := scanKeeperState(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, state.keeperAccount)
	}
	return accounts, rows.Err()
}

func (a *App) getKeeperState(ctx context.Context, name string) (*keeperAuthState, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email, account_type, disabled, priority, primary_used_percent,
		       secondary_used_percent, CAST(primary_reset_at AS TEXT), CAST(secondary_reset_at AS TEXT), quota_threshold,
		       last_status_code, last_error, latest_action, CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT),
		       restore_priority, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
		FROM codex_keeper_auth_states WHERE auth_name = ?
	`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, notFoundError("账号状态不存在")
	}
	state, err := scanKeeperState(rows)
	if err != nil {
		return nil, err
	}
	return &state, rows.Err()
}

func scanKeeperState(scanner interface{ Scan(dest ...any) error }) (keeperAuthState, error) {
	var state keeperAuthState
	var email, accountType, primaryReset, secondaryReset, lastError, latestAction, lastChecked, lastHealthy, createdAt, updatedAt sql.NullString
	var priority, primaryUsed, secondaryUsed, quotaThreshold, lastStatus, restorePriority sql.NullInt64
	err := scanner.Scan(
		&state.Name, &email, &accountType, &state.Disabled, &priority, &primaryUsed,
		&secondaryUsed, &primaryReset, &secondaryReset, &quotaThreshold, &lastStatus,
		&lastError, &latestAction, &lastChecked, &lastHealthy, &restorePriority,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return keeperAuthState{}, err
	}
	state.Email = nullableString(email)
	state.AccountType = nullableString(accountType)
	state.Priority = nullableInt(priority)
	state.PrimaryUsedPercent = nullableInt(primaryUsed)
	state.SecondaryUsedPercent = nullableInt(secondaryUsed)
	state.PrimaryResetAt = timePtr(primaryReset)
	state.SecondaryResetAt = timePtr(secondaryReset)
	state.QuotaThreshold = nullableInt(quotaThreshold)
	state.LastStatusCode = nullableInt(lastStatus)
	state.LastError = nullableString(lastError)
	state.LatestAction = nullableString(latestAction)
	state.LastCheckedAt = timePtr(lastChecked)
	state.LastHealthyAt = timePtr(lastHealthy)
	state.RestorePriority = nullableInt(restorePriority)
	if parsed, ok := parseDBTime(createdAt.String); ok {
		state.CreatedAt = parsed
	}
	if parsed, ok := parseDBTime(updatedAt.String); ok {
		state.UpdatedAt = parsed
	}
	return state, nil
}

func (a *App) upsertKeeperState(ctx context.Context, result keeperAccountResult) error {
	now := dbTime(time.Now())
	checkedAt := dbTime(result.CheckedAt)
	var lastHealthy any
	if result.Result == "healthy" || result.Result == "priority_degraded" || result.Result == "priority_restored" {
		lastHealthy = checkedAt
	}
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, account_type, disabled, priority, restore_priority, latest_action, last_error,
			last_status_code, primary_used_percent, secondary_used_percent, quota_threshold,
			primary_reset_at, secondary_reset_at, last_checked_at, last_healthy_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(auth_name) DO UPDATE SET
			email = excluded.email,
			account_type = excluded.account_type,
			disabled = excluded.disabled,
			priority = excluded.priority,
			restore_priority = CASE
				WHEN ? THEN NULL
				WHEN excluded.restore_priority IS NOT NULL THEN excluded.restore_priority
				ELSE codex_keeper_auth_states.restore_priority
			END,
			latest_action = excluded.latest_action,
			last_error = excluded.last_error,
			last_status_code = excluded.last_status_code,
			primary_used_percent = excluded.primary_used_percent,
			secondary_used_percent = excluded.secondary_used_percent,
			quota_threshold = excluded.quota_threshold,
			primary_reset_at = excluded.primary_reset_at,
			secondary_reset_at = excluded.secondary_reset_at,
			last_checked_at = excluded.last_checked_at,
			last_healthy_at = COALESCE(excluded.last_healthy_at, codex_keeper_auth_states.last_healthy_at),
			updated_at = excluded.updated_at
	`, result.Name, result.Email, result.AccountType, boolValue(result.Disabled), result.Priority, result.RestorePriority, result.LatestAction, result.LastError, result.LastStatusCode, result.PrimaryUsedPercent, result.SecondaryUsedPercent, result.QuotaThreshold, dbTimePtr(result.PrimaryResetAt), dbTimePtr(result.SecondaryResetAt), checkedAt, lastHealthy, now, now, result.ClearRestorePriority)
	return err
}

func (a *App) setKeeperAccountDisabled(ctx context.Context, authName string, disabled bool) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	state, err := a.getKeeperState(ctx, authName)
	if err != nil {
		return err
	}
	if err := a.setKeeperRemoteDisabled(ctx, cfg, authName, disabled); err != nil {
		return err
	}
	now := dbTime(time.Now())
	var checkedAt any = now
	var lastHealthy any
	if !disabled {
		lastHealthy = now
	}
	_, err = a.db.ExecContext(ctx, `
		UPDATE codex_keeper_auth_states
		SET disabled = ?, restore_priority = NULL, latest_action = NULL, last_error = NULL,
		    last_status_code = NULL, primary_used_percent = CASE WHEN ? THEN NULL ELSE primary_used_percent END,
		    secondary_used_percent = CASE WHEN ? THEN NULL ELSE secondary_used_percent END,
		    primary_reset_at = CASE WHEN ? THEN NULL ELSE primary_reset_at END,
		    secondary_reset_at = CASE WHEN ? THEN NULL ELSE secondary_reset_at END,
		    quota_threshold = CASE WHEN ? THEN NULL ELSE quota_threshold END,
		    last_checked_at = ?, last_healthy_at = COALESCE(?, last_healthy_at), updated_at = ?
		WHERE auth_name = ?
	`, disabled, disabled, disabled, disabled, disabled, disabled, checkedAt, lastHealthy, now, state.Name)
	return err
}

func (a *App) deleteKeeperAccount(ctx context.Context, authName string) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	state, err := a.getKeeperState(ctx, authName)
	if err != nil {
		return err
	}
	if !state.Disabled {
		return validationError("只能删除已禁用账号")
	}
	if err := a.deleteKeeperRemoteAuthFile(ctx, cfg, authName); err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `DELETE FROM codex_keeper_auth_states WHERE auth_name = ?`, authName)
	return err
}

func (a *App) bulkDeleteKeeperAccounts(w http.ResponseWriter, r *http.Request) error {
	var payload keeperBulkDeleteRequest
	if err := decodeJSON(r, &payload); err != nil {
		return err
	}
	names, err := normalizeKeeperAuthNames(payload.AuthNames)
	if err != nil {
		return err
	}
	deleted := []string{}
	failures := []map[string]string{}
	for _, name := range names {
		if err := a.deleteKeeperAccount(r.Context(), name); err != nil {
			failures = append(failures, map[string]string{"name": name, "message": err.Error()})
			continue
		}
		deleted = append(deleted, name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "deleted": deleted, "failed": failures})
	return nil
}

func (a *App) updateKeeperAccountPriority(ctx context.Context, authName string, priority int) error {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return err
	}
	state, err := a.getKeeperState(ctx, authName)
	if err != nil {
		return err
	}
	if err := validateKeeperPriority(priority, state.AccountType, cfg.CodexKeeperPriorityRule); err != nil {
		return err
	}
	if err := a.setKeeperRemotePriority(ctx, cfg, authName, &priority); err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `
		UPDATE codex_keeper_auth_states
		SET priority = ?, restore_priority = NULL, latest_action = NULL, last_error = NULL, updated_at = ?
		WHERE auth_name = ?
	`, priority, dbTime(time.Now()), authName)
	return err
}

func (a *App) createKeeperRun(ctx context.Context, mode string) (int, error) {
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_runs (mode, state, detail, started_at, created_at, updated_at)
		VALUES (?, 'running', '', ?, ?, ?)
	`, mode, now, now, now)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return int(id), nil
}

func (a *App) finishKeeperRun(ctx context.Context, runID int, state, detail string, stats keeperStats) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE codex_keeper_runs
		SET state = ?, detail = ?, finished_at = ?, total = ?, healthy = ?, status_disabled = ?,
		    status_enabled = ?, priority_degraded = ?, priority_restored = ?, skipped = ?,
		    network_error = ?, updated_at = ?
		WHERE id = ?
	`, state, detail, dbTime(time.Now()), stats.Total, stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.PriorityRestored, stats.Skipped, stats.NetworkError, dbTime(time.Now()), runID)
	return err
}

type keeperRunRecord struct {
	Mode       *string
	State      string
	Detail     string
	StartedAt  *time.Time
	FinishedAt *time.Time
	Stats      keeperStats
}

func (a *App) latestKeeperRun(ctx context.Context) (*keeperRunRecord, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT mode, state, detail, CAST(started_at AS TEXT), CAST(finished_at AS TEXT), total, healthy, status_disabled,
		       status_enabled, priority_degraded, priority_restored, skipped, network_error
		FROM codex_keeper_runs ORDER BY id DESC LIMIT 1
	`)
	var run keeperRunRecord
	var mode, startedAt, finishedAt sql.NullString
	err := row.Scan(&mode, &run.State, &run.Detail, &startedAt, &finishedAt, &run.Stats.Total, &run.Stats.Healthy, &run.Stats.StatusDisabled, &run.Stats.StatusEnabled, &run.Stats.PriorityDegraded, &run.Stats.PriorityRestored, &run.Stats.Skipped, &run.Stats.NetworkError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.Mode = nullableString(mode)
	run.StartedAt = timePtr(startedAt)
	run.FinishedAt = timePtr(finishedAt)
	return &run, nil
}

func (a *App) recordKeeperRunAccount(ctx context.Context, runID int, result keeperAccountResult) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_run_accounts (
			run_id, auth_name, email, result, account_type, priority, disabled,
			keeper_action, primary_used_percent, secondary_used_percent, quota_threshold,
			last_status_code, last_error, latest_action, checked_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, result.Name, result.Email, result.Result, result.AccountType, result.Priority, result.Disabled, valueOr(result.LatestAction, "none"), result.PrimaryUsedPercent, result.SecondaryUsedPercent, result.QuotaThreshold, result.LastStatusCode, result.LastError, result.LatestAction, dbTime(result.CheckedAt), dbTime(time.Now()))
	return err
}

func extractKeeperObjects(payload any, keys []string) []map[string]any {
	if items, ok := payload.([]any); ok {
		return mapItems(items)
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return []map[string]any{}
	}
	for _, key := range keys {
		if items, ok := object[key].([]any); ok {
			return mapItems(items)
		}
	}
	return []map[string]any{}
}

func mapItems(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func mergeKeeperObjects(left, right map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		result[key] = value
	}
	return result
}

func parseKeeperUsageInfo(payload map[string]any) keeperUsageInfo {
	usage := keeperUsageInfo{PlanType: "unknown"}
	if payload == nil {
		return usage
	}
	if value := keeperString(payload["plan_type"]); value != "" {
		usage.PlanType = value
	}
	rateLimit, _ := payload["rate_limit"].(map[string]any)
	primary, _ := rateLimit["primary_window"].(map[string]any)
	secondary, _ := rateLimit["secondary_window"].(map[string]any)
	if value := keeperIntPtr(primary["used_percent"]); value != nil {
		usage.PrimaryUsedPercent = *value
	}
	usage.SecondaryUsedPercent = keeperIntPtr(secondary["used_percent"])
	usage.PrimaryResetAt = quotaResetAt(primary, time.Now().In(appTimeLocation))
	usage.SecondaryResetAt = quotaResetAt(secondary, time.Now().In(appTimeLocation))
	return usage
}

func quotaResetAt(window map[string]any, base time.Time) *time.Time {
	if window == nil {
		return nil
	}
	if ts := keeperIntPtr(window["reset_at"], window["resetAt"], window["reset_at_seconds"], window["resetAtSeconds"]); ts != nil {
		seconds := int64(*ts)
		if seconds > 10_000_000_000 {
			seconds /= 1000
		}
		parsed := time.Unix(seconds, 0).In(appTimeLocation)
		return &parsed
	}
	if after := keeperIntPtr(window["reset_after_seconds"], window["resetAfterSeconds"]); after != nil && *after >= 0 {
		parsed := base.Add(time.Duration(*after) * time.Second)
		return &parsed
	}
	return nil
}

func accountTypeFromKeeperDetail(detail map[string]any, usage *keeperUsageInfo) *string {
	values := []string{}
	if usage != nil {
		values = append(values, usage.PlanType)
	}
	for _, key := range []string{"plan_type", "plan", "tier", "account_plan", "subscription_plan", "sku", "account_type"} {
		if value := keeperString(detail[key]); value != "" {
			values = append(values, value)
		}
	}
	text := strings.ToLower(strings.Join(values, " "))
	text = strings.NewReplacer("-", "_", " ", "_").Replace(text)
	var result string
	switch {
	case strings.Contains(text, "20x") || strings.Contains(text, "pro_20"):
		result = "pro_20x"
	case strings.Contains(text, "5x") || strings.Contains(text, "pro_5"):
		result = "pro_5x"
	case strings.Contains(text, "team") || strings.Contains(text, "business"):
		result = "team"
	case strings.Contains(text, "plus"):
		result = "plus"
	case strings.Contains(text, "free"):
		result = "free"
	default:
		return nil
	}
	return &result
}

func keeperPriorityForType(accountType *string, rules map[string]int) *int {
	if accountType == nil {
		return nil
	}
	value, ok := normalizePriorityRules(rules)[strings.ToLower(strings.TrimSpace(*accountType))]
	if !ok {
		return nil
	}
	return &value
}

func validateKeeperPriority(priority int, accountType *string, rules map[string]int) error {
	if priority < -1 || priority > 20 {
		return nil
	}
	expected := keeperPriorityForType(accountType, rules)
	if expected != nil && *expected == priority {
		return nil
	}
	if accountType == nil || expected == nil {
		return validationError("该账号类型没有可设置的系统 priority")
	}
	return validationError(fmt.Sprintf("只能设置小于 -1、大于 20，或当前账号类型 %s 对应的 priority %d", *accountType, *expected))
}

func isBadKeeperCredential(result keeperHTTPResult) bool {
	if result.StatusCode != nil && (*result.StatusCode == 401 || *result.StatusCode == 402) {
		return true
	}
	text := strings.ToLower(result.Brief)
	if result.JSONData != nil {
		payload, _ := json.Marshal(result.JSONData)
		text += " " + strings.ToLower(string(payload))
	}
	return strings.Contains(text, "workspace") && (strings.Contains(text, "disabled") || strings.Contains(text, "deactivated"))
}

func keeperBodyJSON(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	var object map[string]any
	if json.Unmarshal([]byte(text), &object) != nil {
		return nil
	}
	return object
}

func keeperAuthIndex(detail map[string]any) string {
	for _, key := range []string{"auth_index", "authIndex", "index", "name"} {
		if value := keeperString(detail[key]); value != "" {
			return value
		}
	}
	return "unknown"
}

func keeperString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func keeperStringPtr(values ...any) *string {
	for _, value := range values {
		if text := keeperString(value); text != "" {
			return &text
		}
	}
	return nil
}

func keeperIntPtr(values ...any) *int {
	for _, value := range values {
		switch typed := value.(type) {
		case nil:
			continue
		case int:
			return &typed
		case int64:
			converted := int(typed)
			return &converted
		case float64:
			converted := int(typed)
			return &converted
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func keeperBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func briefPayload(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if len(text) > 160 {
		return text[:160] + "..."
	}
	return text
}

func briefAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		if len(typed) > 160 {
			return typed[:160] + "..."
		}
		return typed
	default:
		payload, _ := json.Marshal(typed)
		return briefPayload(payload)
	}
}

func normalizeKeeperAuthNames(raw []string) ([]string, error) {
	result, err := normalizeOptionalKeeperAuthNames(raw)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, validationError("账号名称不能为空")
	}
	return result, nil
}

func normalizeOptionalKeeperAuthNames(raw []string) ([]string, error) {
	seen := map[string]bool{}
	result := []string{}
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" {
			return nil, validationError("账号名称不能为空")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result, nil
}

func waitForStop(stop <-chan struct{}, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-stop:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-stop:
		return true
	case <-timer.C:
		return false
	}
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sortKeeperAccounts(accounts []keeperAccount) {
	sort.Slice(accounts, func(i, j int) bool {
		left := valueOr(accounts[i].Email, "") + accounts[i].Name
		right := valueOr(accounts[j].Email, "") + accounts[j].Name
		return left < right
	})
}
