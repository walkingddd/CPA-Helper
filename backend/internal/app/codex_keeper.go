package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
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
	keeperUsageURL                 = "https://chatgpt.com/backend-api/wham/usage"
	keeperLogFilePrefix            = "codex-keeper-"
	keeperLogComponent             = "codex_keeper"
	keeperLogRetainedFiles         = 3
	keeperMaxInMemoryLogs          = 300
	keeperQuotaWindowUsageCacheTTL = 30 * time.Second
	keeperFiveHourWindowSeconds    = 5 * 60 * 60
	keeperWeekWindowSeconds        = 7 * 24 * 60 * 60
	keeperMonthWindowSeconds       = 30 * 24 * 60 * 60
)

type KeeperRunner struct {
	app            *App
	mu             sync.Mutex
	daemonStop     chan struct{}
	daemonDone     chan struct{}
	running        bool
	runningModes   map[string]struct{}
	inFlightAuths  map[string]string
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
	RunningModes   []string    `json:"running_modes"`
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
	ScheduleCron                      *string              `json:"schedule_cron"`
	QuotaThreshold                    *int                 `json:"quota_threshold"`
	UsageTimeoutSeconds               *int                 `json:"usage_timeout_seconds"`
	CPATimeoutSeconds                 *int                 `json:"cpa_timeout_seconds"`
	MaxRetries                        *int                 `json:"max_retries"`
	WorkerThreads                     *int                 `json:"worker_threads"`
	ConditionalRefreshIntervalSeconds *int                 `json:"conditional_refresh_interval_seconds"`
	AccountRefreshCacheMinutes        *int                 `json:"account_refresh_cache_minutes"`
	DryRun                            *bool                `json:"dry_run"`
	EnableCredentialWebsockets        *bool                `json:"enable_credential_websockets"`
	AutoStartDaemon                   *bool                `json:"auto_start_daemon"`
	PriorityRules                     []keeperPriorityRule `json:"priority_rules"`
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

type keeperQuotaResetRequest struct {
	AuthName string `json:"auth_name"`
}

type keeperAccount struct {
	Name                   string     `json:"name"`
	Email                  *string    `json:"email"`
	AuthIndex              *string    `json:"auth_index"`
	AccountType            *string    `json:"account_type"`
	Disabled               bool       `json:"disabled"`
	Priority               *int       `json:"priority"`
	PrimaryUsedPercent     *int       `json:"primary_used_percent"`
	SecondaryUsedPercent   *int       `json:"secondary_used_percent"`
	PrimaryResetAt         *time.Time `json:"primary_reset_at"`
	SecondaryResetAt       *time.Time `json:"secondary_reset_at"`
	PrimaryWindowSeconds   *int       `json:"primary_window_seconds"`
	SecondaryWindowSeconds *int       `json:"secondary_window_seconds"`
	QuotaThreshold         *int       `json:"quota_threshold"`
	LastStatusCode         *int       `json:"last_status_code"`
	QuotaResetCount        int        `json:"quota_reset_count"`
	LastQuotaResetAt       *time.Time `json:"last_quota_reset_at"`
	LastError              *string    `json:"last_error"`
	LatestAction           *string    `json:"latest_action"`
	LastCheckedAt          *time.Time `json:"last_checked_at"`
	LastHealthyAt          *time.Time `json:"last_healthy_at"`
}

type keeperAccountResponse struct {
	Name                   string                          `json:"name"`
	Email                  *string                         `json:"email"`
	AccountType            *string                         `json:"account_type"`
	Disabled               bool                            `json:"disabled"`
	Priority               *int                            `json:"priority"`
	PrimaryUsedPercent     *int                            `json:"primary_used_percent"`
	SecondaryUsedPercent   *int                            `json:"secondary_used_percent"`
	PrimaryResetAt         *string                         `json:"primary_reset_at"`
	SecondaryResetAt       *string                         `json:"secondary_reset_at"`
	PrimaryWindowSeconds   *int                            `json:"primary_window_seconds"`
	SecondaryWindowSeconds *int                            `json:"secondary_window_seconds"`
	PrimaryWindowUsage     *keeperQuotaWindowUsageResponse `json:"primary_window_usage"`
	SecondaryWindowUsage   *keeperQuotaWindowUsageResponse `json:"secondary_window_usage"`
	QuotaThreshold         *int                            `json:"quota_threshold"`
	LastStatusCode         *int                            `json:"last_status_code"`
	LastError              *string                         `json:"last_error"`
	LatestAction           *string                         `json:"latest_action"`
	LastCheckedAt          *string                         `json:"last_checked_at"`
	LastHealthyAt          *string                         `json:"last_healthy_at"`
	QuotaResetCount        int                             `json:"quota_reset_count"`
	LastQuotaResetAt       *string                         `json:"last_quota_reset_at"`
}

type keeperQuotaWindowUsageResponse struct {
	WindowStart      string  `json:"window_start"`
	WindowEnd        string  `json:"window_end"`
	ResetAt          string  `json:"reset_at"`
	WindowSeconds    int     `json:"window_seconds"`
	Records          int     `json:"records"`
	SuccessRecords   int     `json:"success_records"`
	FailedRecords    int     `json:"failed_records"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	UnpricedRecords  int     `json:"unpriced_records"`
	Stale            bool    `json:"stale"`
	WindowSource     string  `json:"window_source"`
}

type keeperQuotaWindowUsage struct {
	WindowStart      time.Time
	WindowEnd        time.Time
	ResetAt          time.Time
	WindowSeconds    int
	Records          int
	SuccessRecords   int
	FailedRecords    int
	InputTokens      int
	OutputTokens     int
	CachedTokens     int
	ReasoningTokens  int
	TotalTokens      int
	EstimatedCostUSD float64
	UnpricedRecords  int
	Stale            bool
	WindowSource     string
}

type keeperQuotaWindowUsagePair struct {
	Primary   *keeperQuotaWindowUsage
	Secondary *keeperQuotaWindowUsage
}

type keeperWindowUsageCache struct {
	mu        sync.Mutex
	key       string
	expiresAt time.Time
	usages    map[string]keeperQuotaWindowUsagePair
}

type keeperAuthState struct {
	keeperAccount
	RestorePriority *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type keeperUsageInfo struct {
	PlanType               string
	PrimaryUsedPercent     int
	SecondaryUsedPercent   *int
	PrimaryResetAt         *time.Time
	SecondaryResetAt       *time.Time
	PrimaryWindowSeconds   *int
	SecondaryWindowSeconds *int
}

type keeperHTTPResult struct {
	StatusCode *int
	JSONData   map[string]any
	Brief      string
	Error      string
}

type keeperAccountResult struct {
	Name                   string
	Result                 string
	Email                  *string
	AuthIndex              *string
	AccountType            *string
	Priority               *int
	RestorePriority        *int
	ClearRestorePriority   bool
	Disabled               *bool
	PrimaryUsedPercent     *int
	SecondaryUsedPercent   *int
	PrimaryResetAt         *time.Time
	SecondaryResetAt       *time.Time
	PrimaryWindowSeconds   *int
	SecondaryWindowSeconds *int
	QuotaThreshold         *int
	LastStatusCode         *int
	LastError              *string
	LatestAction           *string
	CheckedAt              time.Time
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
	logs := append([]string{}, r.logs...)
	runningModes := r.runningModeListLocked()
	response := keeperStatusResponse{
		Running:        len(runningModes) > 0,
		RunningModes:   runningModes,
		DaemonRunning:  r.daemonRunningLocked(),
		State:          r.state,
		Detail:         r.detail,
		Mode:           cloneStringPtr(r.mode),
		LastStartedAt:  apiDateTimePtr(r.lastStartedAt),
		LastFinishedAt: apiDateTimePtr(r.lastFinishedAt),
		Stats:          r.stats,
		Logs:           logs,
	}
	r.mu.Unlock()
	if r.app != nil {
		response.Stats = keeperStats{}
		if run, err := r.app.latestKeeperRunByMode(context.Background(), "daemon"); err == nil && run != nil {
			response.Stats = run.Stats
		}
	}
	return response
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
		delay := positiveDuration(time.Until(times[0]))
		r.log("下一轮计划：" + times[0].In(appTimeLocation).Format("2006-01-02 15:04:05"))
		cronTimer := time.NewTimer(delay)
		conditionalInterval := keeperConditionalRefreshInterval(cfg)
		var conditionalTicker *time.Ticker
		var conditionalC <-chan time.Time
		if conditionalInterval > 0 {
			conditionalTicker = time.NewTicker(conditionalInterval)
			conditionalC = conditionalTicker.C
		}

		restartCycle := false
		for !restartCycle {
			select {
			case <-stop:
				cronTimer.Stop()
				if conditionalTicker != nil {
					conditionalTicker.Stop()
				}
				return
			case <-cronTimer.C:
				if conditionalTicker != nil {
					conditionalTicker.Stop()
				}
				if r.markRunning("daemon") {
					go r.run("daemon")
				}
				restartCycle = true
			case <-conditionalC:
				nextCfg, err := r.app.loadConfig(context.Background())
				if err != nil {
					r.log("读取 Codex Keeper 条件刷新配置失败：" + err.Error())
					continue
				}
				nextInterval := keeperConditionalRefreshInterval(nextCfg)
				if nextInterval != conditionalInterval {
					cronTimer.Stop()
					if conditionalTicker != nil {
						conditionalTicker.Stop()
					}
					restartCycle = true
					continue
				}
				names, err := r.app.conditionalKeeperRefreshCandidates(context.Background(), nextCfg)
				if err != nil {
					r.log("Codex Keeper 条件刷新候选查询失败：" + err.Error())
					continue
				}
				if len(names) == 0 {
					continue
				}
				if r.markRunning("conditional") {
					go r.runAccounts("conditional", names)
				}
			}
		}
	}
}

func positiveDuration(delay time.Duration) time.Duration {
	if delay < 0 {
		return 0
	}
	return delay
}

func keeperConditionalRefreshInterval(cfg AppConfig) time.Duration {
	seconds := cfg.CodexKeeper.ConditionalRefreshIntervalSeconds
	if !validKeeperConditionalRefreshInterval(seconds) || seconds == 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (r *KeeperRunner) ensureRunningModesLocked() {
	if r.runningModes == nil {
		r.runningModes = map[string]struct{}{}
	}
}

func (r *KeeperRunner) runningModeListLocked() []string {
	r.ensureRunningModesLocked()
	modes := make([]string, 0, len(r.runningModes))
	for mode := range r.runningModes {
		modes = append(modes, mode)
	}
	sort.Slice(modes, func(i, j int) bool {
		leftOrder := keeperModeOrder(modes[i])
		rightOrder := keeperModeOrder(modes[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return modes[i] < modes[j]
	})
	return modes
}

func (r *KeeperRunner) ensureInFlightAuthsLocked() {
	if r.inFlightAuths == nil {
		r.inFlightAuths = map[string]string{}
	}
}

func (r *KeeperRunner) tryLockAuthName(mode string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInFlightAuthsLocked()
	if _, exists := r.inFlightAuths[name]; exists {
		return false
	}
	r.inFlightAuths[name] = mode
	return true
}

func (r *KeeperRunner) unlockAuthName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlightAuths == nil {
		return
	}
	delete(r.inFlightAuths, name)
}

func keeperModeOrder(mode string) int {
	switch mode {
	case "daemon":
		return 0
	case "once":
		return 1
	case "conditional":
		return 2
	case "accounts":
		return 3
	default:
		return 99
	}
}

func keeperModesConflict(existingMode string, nextMode string) bool {
	if existingMode == nextMode {
		return true
	}
	return (existingMode == "once" && nextMode == "daemon") ||
		(existingMode == "daemon" && nextMode == "once")
}

func keeperStatusModePtr(modes []string) *string {
	if len(modes) == 0 {
		return nil
	}
	mode := modes[0]
	return &mode
}

func keeperRunningDetail(modes []string) string {
	if len(modes) > 1 {
		return "正在运行多个 Codex Keeper 任务"
	}
	if len(modes) == 0 {
		return "尚未运行"
	}
	switch modes[0] {
	case "accounts":
		return "正在刷新 Codex 账号"
	case "conditional":
		return "正在按条件刷新 Codex 账号"
	default:
		return "正在巡检 Codex 账号"
	}
}

func (r *KeeperRunner) markRunning(mode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureRunningModesLocked()
	for runningMode := range r.runningModes {
		if keeperModesConflict(runningMode, mode) {
			return false
		}
	}
	r.runningModes[mode] = struct{}{}
	now := time.Now().In(appTimeLocation)
	r.running = true
	r.state = "running"
	runningModes := r.runningModeListLocked()
	r.detail = keeperRunningDetail(runningModes)
	r.mode = keeperStatusModePtr(runningModes)
	r.lastStartedAt = &now
	r.lastFinishedAt = nil
	r.stats = keeperStats{}
	return true
}

func (r *KeeperRunner) run(mode string) {
	r.runAccounts(mode, nil)
}

func (r *KeeperRunner) runAccounts(mode string, authNames []string) {
	options := keeperRunOptionsForMode(mode, authNames)
	options.TryLockAuthName = r.tryLockAuthName
	options.UnlockAuthName = r.unlockAuthName
	stats, detail, err := r.app.executeKeeperRunWithOptions(context.Background(), options, r.log)
	finishedAt := time.Now().In(appTimeLocation)
	logMessage := detail
	if err != nil {
		logMessage = "巡检失败：" + err.Error()
	}
	r.mu.Lock()
	r.ensureRunningModesLocked()
	delete(r.runningModes, mode)
	runningModes := r.runningModeListLocked()
	r.running = len(runningModes) > 0
	r.lastFinishedAt = &finishedAt
	r.stats = stats
	if r.running {
		r.state = "running"
		r.detail = keeperRunningDetail(runningModes)
		r.mode = keeperStatusModePtr(runningModes)
	} else if err != nil {
		r.state = "failed"
		r.detail = err.Error()
	} else {
		completedMode := mode
		r.mode = &completedMode
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
	case len(parts) == 1 && parts[0] == "reset-quota":
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		var payload keeperQuotaResetRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		if strings.TrimSpace(payload.AuthName) == "" {
			return validationError("auth_name 不能为空")
		}
		result, err := a.resetKeeperQuota(r.Context(), strings.TrimSpace(payload.AuthName))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "account": result})
		return nil
	case len(parts) == 1 && parts[0] == "accounts":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		accounts, err := a.listKeeperAccounts(r.Context())
		if err != nil {
			return err
		}
		windowUsages, err := a.keeperQuotaWindowUsages(r.Context(), accounts)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": keeperAccountResponses(accounts, windowUsages)})
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
		"cliaproxy_url":                        cfg.Collector.CLIProxyURL,
		"management_key_set":                   strings.TrimSpace(cfg.Collector.ManagementKey) != "",
		"schedule_cron":                        normalized,
		"next_run_times":                       apiDateTimes(times),
		"quota_threshold":                      cfg.CodexKeeper.QuotaThreshold,
		"usage_timeout_seconds":                cfg.CodexKeeper.UsageTimeoutSeconds,
		"cpa_timeout_seconds":                  cfg.CodexKeeper.CPATimeoutSeconds,
		"max_retries":                          cfg.CodexKeeper.MaxRetries,
		"worker_threads":                       cfg.CodexKeeper.WorkerThreads,
		"conditional_refresh_interval_seconds": cfg.CodexKeeper.ConditionalRefreshIntervalSeconds,
		"account_refresh_cache_minutes":        cfg.CodexKeeper.AccountRefreshCacheMinutes,
		"dry_run":                              cfg.CodexKeeper.DryRun,
		"enable_credential_websockets":         cfg.CodexKeeper.EnableCredentialWebsockets,
		"auto_start_daemon":                    cfg.CodexKeeper.AutoStartDaemon,
		"priority_rules":                       sortedPriorityRules(cfg.CodexKeeperPriorityRule),
	}
}

func keeperAccountResponses(accounts []keeperAccount, windowUsages map[string]keeperQuotaWindowUsagePair) []keeperAccountResponse {
	responses := make([]keeperAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		usage := windowUsages[account.Name]
		responses = append(responses, keeperAccountResponse{
			Name:                   account.Name,
			Email:                  account.Email,
			AccountType:            account.AccountType,
			Disabled:               account.Disabled,
			Priority:               keeperDisplayPriority(account.Priority),
			PrimaryUsedPercent:     account.PrimaryUsedPercent,
			SecondaryUsedPercent:   account.SecondaryUsedPercent,
			PrimaryResetAt:         apiDateTimePtr(account.PrimaryResetAt),
			SecondaryResetAt:       apiDateTimePtr(account.SecondaryResetAt),
			PrimaryWindowSeconds:   account.PrimaryWindowSeconds,
			SecondaryWindowSeconds: account.SecondaryWindowSeconds,
			PrimaryWindowUsage:     keeperQuotaWindowUsageResponseFrom(usage.Primary),
			SecondaryWindowUsage:   keeperQuotaWindowUsageResponseFrom(usage.Secondary),
			QuotaThreshold:         account.QuotaThreshold,
			LastStatusCode:         account.LastStatusCode,
			LastError:              account.LastError,
			LatestAction:           account.LatestAction,
			LastCheckedAt:          apiDateTimePtr(account.LastCheckedAt),
			LastHealthyAt:          apiDateTimePtr(account.LastHealthyAt),
			QuotaResetCount:        account.QuotaResetCount,
			LastQuotaResetAt:       apiDateTimePtr(account.LastQuotaResetAt),
		})
	}
	return responses
}

func keeperQuotaWindowUsageResponseFrom(usage *keeperQuotaWindowUsage) *keeperQuotaWindowUsageResponse {
	if usage == nil {
		return nil
	}
	return &keeperQuotaWindowUsageResponse{
		WindowStart:      apiDateTime(usage.WindowStart),
		WindowEnd:        apiDateTime(usage.WindowEnd),
		ResetAt:          apiDateTime(usage.ResetAt),
		WindowSeconds:    usage.WindowSeconds,
		Records:          usage.Records,
		SuccessRecords:   usage.SuccessRecords,
		FailedRecords:    usage.FailedRecords,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CachedTokens:     usage.CachedTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		TotalTokens:      usage.TotalTokens,
		EstimatedCostUSD: usage.EstimatedCostUSD,
		UnpricedRecords:  usage.UnpricedRecords,
		Stale:            usage.Stale,
		WindowSource:     usage.WindowSource,
	}
}

func (a *App) keeperQuotaWindowUsages(ctx context.Context, accounts []keeperAccount) (map[string]keeperQuotaWindowUsagePair, error) {
	if len(accounts) == 0 {
		return map[string]keeperQuotaWindowUsagePair{}, nil
	}
	key, err := a.keeperQuotaWindowUsageCacheKey(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(appTimeLocation)
	if cached, ok := a.cachedKeeperQuotaWindowUsages(key, now); ok {
		return cached, nil
	}
	usages, err := a.computeKeeperQuotaWindowUsages(ctx, accounts, now)
	if err != nil {
		return nil, err
	}
	a.storeKeeperQuotaWindowUsages(key, now.Add(keeperQuotaWindowUsageCacheTTL), usages)
	return usages, nil
}

func (a *App) keeperQuotaWindowUsageCacheKey(ctx context.Context) (string, error) {
	var stateUpdated, usageID sql.NullString
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(updated_at AS TEXT)), '') FROM codex_keeper_auth_states`).Scan(&stateUpdated); err != nil {
		return "", err
	}
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(CAST(MAX(id) AS TEXT), '') FROM usage_records`).Scan(&usageID); err != nil {
		return "", err
	}
	return stateUpdated.String + "|" + usageID.String, nil
}

func (a *App) cachedKeeperQuotaWindowUsages(key string, now time.Time) (map[string]keeperQuotaWindowUsagePair, bool) {
	a.keeperUsageCache.mu.Lock()
	defer a.keeperUsageCache.mu.Unlock()
	if key == "" || key != a.keeperUsageCache.key || !now.Before(a.keeperUsageCache.expiresAt) {
		return nil, false
	}
	return copyKeeperQuotaWindowUsagePairs(a.keeperUsageCache.usages), true
}

func (a *App) storeKeeperQuotaWindowUsages(key string, expiresAt time.Time, usages map[string]keeperQuotaWindowUsagePair) {
	a.keeperUsageCache.mu.Lock()
	defer a.keeperUsageCache.mu.Unlock()
	a.keeperUsageCache.key = key
	a.keeperUsageCache.expiresAt = expiresAt
	a.keeperUsageCache.usages = copyKeeperQuotaWindowUsagePairs(usages)
}

func copyKeeperQuotaWindowUsagePairs(source map[string]keeperQuotaWindowUsagePair) map[string]keeperQuotaWindowUsagePair {
	if source == nil {
		return map[string]keeperQuotaWindowUsagePair{}
	}
	copied := make(map[string]keeperQuotaWindowUsagePair, len(source))
	for name, pair := range source {
		copied[name] = keeperQuotaWindowUsagePair{
			Primary:   copyKeeperQuotaWindowUsage(pair.Primary),
			Secondary: copyKeeperQuotaWindowUsage(pair.Secondary),
		}
	}
	return copied
}

func copyKeeperQuotaWindowUsage(source *keeperQuotaWindowUsage) *keeperQuotaWindowUsage {
	if source == nil {
		return nil
	}
	copied := *source
	return &copied
}

func (a *App) computeKeeperQuotaWindowUsages(ctx context.Context, accounts []keeperAccount, now time.Time) (map[string]keeperQuotaWindowUsagePair, error) {
	usages := map[string]keeperQuotaWindowUsagePair{}
	sourceAccounts := map[string]string{}
	aliases := map[string][]string{}
	var minStart, maxEnd time.Time
	maxWindowSeconds := 0

	for _, account := range accounts {
		addKeeperAuthAlias(aliases, account.Name, account.Name)
		addKeeperSourceAccountAlias(sourceAccounts, account.Name, account.Name)
		if account.AuthIndex != nil {
			addKeeperAuthAlias(aliases, *account.AuthIndex, account.Name)
		}
		if account.Email != nil {
			addKeeperAuthAlias(aliases, *account.Email, account.Name)
			addKeeperSourceAccountAlias(sourceAccounts, *account.Email, account.Name)
		}
		if account.Disabled {
			continue
		}

		pair := keeperQuotaWindowPairForAccount(account, now)
		if pair.Primary == nil && pair.Secondary == nil {
			continue
		}
		usages[account.Name] = pair
		minStart, maxEnd = keeperQuotaWindowBounds(minStart, maxEnd, pair.Primary)
		minStart, maxEnd = keeperQuotaWindowBounds(minStart, maxEnd, pair.Secondary)
		for _, usage := range []*keeperQuotaWindowUsage{pair.Primary, pair.Secondary} {
			if usage != nil && usage.WindowSeconds > maxWindowSeconds {
				maxWindowSeconds = usage.WindowSeconds
			}
		}
	}
	if minStart.IsZero() || maxEnd.IsZero() {
		return usages, nil
	}

	queryStart := minStart
	if maxWindowSeconds > 0 {
		maxLookbackStart := now.Add(-time.Duration(maxWindowSeconds) * time.Second)
		if queryStart.Before(maxLookbackStart) {
			queryStart = maxLookbackStart
		}
	}
	records, err := a.keeperUsageRecordsInRange(ctx, queryStart, maxEnd)
	if err != nil {
		return nil, err
	}
	prices, err := a.priceMap(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		accountName, ok := keeperAccountNameForUsageRecord(record, sourceAccounts, aliases)
		if !ok {
			continue
		}
		pair, ok := usages[accountName]
		if !ok {
			continue
		}
		if keeperRecordInQuotaWindow(record, pair.Primary) {
			addRecordToKeeperQuotaWindowUsage(pair.Primary, record, prices)
		}
		if keeperRecordInQuotaWindow(record, pair.Secondary) {
			addRecordToKeeperQuotaWindowUsage(pair.Secondary, record, prices)
		}
	}
	return usages, nil
}

func keeperQuotaWindowPairForAccount(account keeperAccount, now time.Time) keeperQuotaWindowUsagePair {
	return keeperQuotaWindowUsagePair{
		Primary:   keeperQuotaWindowForAccount(account, true, now),
		Secondary: keeperQuotaWindowForAccount(account, false, now),
	}
}

func keeperQuotaWindowForAccount(account keeperAccount, primary bool, now time.Time) *keeperQuotaWindowUsage {
	if !primary && keeperFreeQuotaWindowAccount(account.AccountType) {
		return nil
	}
	resetAt := account.PrimaryResetAt
	windowSeconds := account.PrimaryWindowSeconds
	if !primary {
		resetAt = account.SecondaryResetAt
		windowSeconds = account.SecondaryWindowSeconds
	}
	if resetAt == nil {
		return nil
	}
	seconds, source, ok := keeperQuotaWindowSeconds(account.AccountType, windowSeconds, primary)
	if !ok {
		return nil
	}
	windowEnd := resetAt.In(appTimeLocation)
	windowStart := windowEnd.Add(-time.Duration(seconds) * time.Second)
	return &keeperQuotaWindowUsage{
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		ResetAt:       windowEnd,
		WindowSeconds: seconds,
		Stale:         !now.Before(windowEnd),
		WindowSource:  source,
	}
}

func keeperQuotaWindowSeconds(accountType *string, saved *int, primary bool) (int, string, bool) {
	if saved != nil && *saved > 0 {
		return *saved, "codex", true
	}
	if keeperFreeQuotaWindowAccount(accountType) {
		return keeperMonthWindowSeconds, "inferred", true
	}
	if keeperPaidQuotaWindowAccount(accountType) {
		if primary {
			return keeperFiveHourWindowSeconds, "inferred", true
		}
		return keeperWeekWindowSeconds, "inferred", true
	}
	return 0, "", false
}

func keeperFreeQuotaWindowAccount(accountType *string) bool {
	return strings.ToLower(strings.TrimSpace(valueOr(accountType, ""))) == "free"
}

func keeperPaidQuotaWindowAccount(accountType *string) bool {
	normalized := strings.ToLower(strings.TrimSpace(valueOr(accountType, "")))
	return normalized == "plus" || normalized == "team" || normalized == "k12" || strings.HasPrefix(normalized, "pro")
}

func keeperQuotaWindowBounds(minStart, maxEnd time.Time, usage *keeperQuotaWindowUsage) (time.Time, time.Time) {
	if usage == nil {
		return minStart, maxEnd
	}
	if minStart.IsZero() || usage.WindowStart.Before(minStart) {
		minStart = usage.WindowStart
	}
	if maxEnd.IsZero() || usage.WindowEnd.After(maxEnd) {
		maxEnd = usage.WindowEnd
	}
	return minStart, maxEnd
}

func (a *App) keeperUsageRecordsInRange(ctx context.Context, start, end time.Time) ([]UsageRecord, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json
		FROM usage_records
		WHERE timestamp >= ? AND timestamp < ?
		ORDER BY timestamp`, dbTime(start), dbTime(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsageRecords(rows)
}

func addKeeperSourceAccountAlias(aliases map[string]string, value string, name string) {
	valuePtr := &value
	sourceAccount := sourceAccountFromUsageSource(valuePtr)
	if sourceAccount == nil {
		return
	}
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return
	}
	existing, ok := aliases[*sourceAccount]
	if ok && existing != normalizedName {
		aliases[*sourceAccount] = ""
		return
	}
	aliases[*sourceAccount] = normalizedName
}

func keeperAccountNameForUsageRecord(record UsageRecord, sourceAccounts map[string]string, aliases map[string][]string) (string, bool) {
	if sourceAccount := keeperUsageRecordSourceAccount(record); sourceAccount != "" {
		if name, ok := sourceAccounts[sourceAccount]; ok {
			if name != "" {
				return name, true
			}
		} else {
			return "", false
		}
	}

	identifiers := []string{}
	seen := map[string]bool{}
	addIdentifier := func(value *string) {
		if value == nil {
			return
		}
		normalized := strings.TrimSpace(*value)
		key := keeperAuthAliasKey(normalized)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		identifiers = append(identifiers, normalized)
	}
	addIdentifier(record.AuthIndex)
	addIdentifier(record.Source)
	for _, field := range []string{"auth_index", "authIndex", "index", "auth_name", "authName", "account_id", "accountId", "email", "account_email", "accountEmail", "user_email", "userEmail"} {
		addIdentifier(rawJSONStringField(record.RawJSON, field))
	}
	for _, identifier := range identifiers {
		if name, ok := keeperSingleAuthNameForUsageIdentifier(identifier, aliases); ok {
			return name, true
		}
	}
	return "", false
}

func keeperUsageRecordSourceAccount(record UsageRecord) string {
	if record.SourceAccount != nil {
		return strings.ToLower(strings.TrimSpace(*record.SourceAccount))
	}
	if sourceAccount := sourceAccountFromUsageSource(record.Source); sourceAccount != nil {
		return *sourceAccount
	}
	return ""
}

func keeperSingleAuthNameForUsageIdentifier(identifier string, aliases map[string][]string) (string, bool) {
	names := keeperAuthNamesForUsageIdentifier(identifier, aliases)
	if len(names) != 1 {
		return "", false
	}
	normalized := strings.TrimSpace(names[0])
	if normalized == "" {
		return "", false
	}
	return normalized, true
}

func keeperRecordInQuotaWindow(record UsageRecord, usage *keeperQuotaWindowUsage) bool {
	if usage == nil {
		return false
	}
	return !record.Timestamp.Before(usage.WindowStart) && record.Timestamp.Before(usage.WindowEnd)
}

func addRecordToKeeperQuotaWindowUsage(usage *keeperQuotaWindowUsage, record UsageRecord, prices map[[2]string]ModelPrice) {
	if usage == nil {
		return
	}
	usage.Records++
	if record.Failed {
		usage.FailedRecords++
	} else {
		usage.SuccessRecords++
	}
	usage.InputTokens += usageAggregateInputTokens(record)
	usage.OutputTokens += record.OutputTokens
	usage.CachedTokens += record.CachedTokens
	usage.ReasoningTokens += record.ReasoningTokens
	usage.TotalTokens += usageAggregateTotalTokens(record)
	amount, unpriced := recordCost(record, prices)
	if unpriced {
		usage.UnpricedRecords++
		return
	}
	usage.EstimatedCostUSD = mathRound(usage.EstimatedCostUSD+amount, 8)
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
	if payload.ConditionalRefreshIntervalSeconds != nil {
		if !validKeeperConditionalRefreshInterval(*payload.ConditionalRefreshIntervalSeconds) {
			return validationError("conditional_refresh_interval_seconds 超出范围")
		}
		cfg.CodexKeeper.ConditionalRefreshIntervalSeconds = *payload.ConditionalRefreshIntervalSeconds
	}
	if payload.AccountRefreshCacheMinutes != nil {
		if *payload.AccountRefreshCacheMinutes < 1 {
			return validationError("account_refresh_cache_minutes 不能小于 1")
		}
		cfg.CodexKeeper.AccountRefreshCacheMinutes = *payload.AccountRefreshCacheMinutes
	}
	if payload.DryRun != nil {
		cfg.CodexKeeper.DryRun = *payload.DryRun
	}
	if payload.EnableCredentialWebsockets != nil {
		cfg.CodexKeeper.EnableCredentialWebsockets = *payload.EnableCredentialWebsockets
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

type keeperRunOptions struct {
	Mode            string
	AuthNames       []string
	ManualRefresh   bool
	UseRefreshCache bool
	PersistRun      bool
	TryLockAuthName func(string, string) bool
	UnlockAuthName  func(string)
}

func (a *App) executeKeeperRunForAccounts(ctx context.Context, mode string, authNames []string, logFn func(string)) (keeperStats, string, error) {
	return a.executeKeeperRunWithOptions(ctx, keeperRunOptionsForMode(mode, authNames), logFn)
}

func keeperRunOptionsForMode(mode string, authNames []string) keeperRunOptions {
	return keeperRunOptions{
		Mode:            mode,
		AuthNames:       authNames,
		ManualRefresh:   mode == "accounts",
		UseRefreshCache: mode == "daemon" || mode == "conditional",
		PersistRun:      keeperModePersistsRun(mode),
	}
}

func keeperModePersistsRun(mode string) bool {
	return mode != "accounts" && mode != "conditional"
}

func (a *App) executeKeeperRunWithOptions(ctx context.Context, options keeperRunOptions, logFn func(string)) (keeperStats, string, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return keeperStats{}, "", err
	}
	if strings.TrimSpace(cfg.Collector.ManagementKey) == "" {
		return keeperStats{}, "", validationError("管理密钥未设置，无法运行 Codex Keeper")
	}
	runID := 0
	if options.PersistRun {
		runID, err = a.createKeeperRun(ctx, options.Mode)
		if err != nil {
			return keeperStats{}, "", err
		}
	}
	targetNames, err := normalizeOptionalKeeperAuthNames(options.AuthNames)
	if err != nil {
		if runID > 0 {
			_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), keeperStats{})
		}
		return keeperStats{}, "", err
	}
	targetSet := map[string]bool{}
	for _, name := range targetNames {
		targetSet[name] = true
	}
	if options.Mode == "conditional" {
		logFn(fmt.Sprintf("开始按条件刷新 %d 个 Codex 账号", len(targetSet)))
	} else if len(targetSet) > 0 {
		logFn(fmt.Sprintf("开始刷新 %d 个 Codex 账号", len(targetSet)))
	} else {
		logFn("开始 Codex 账号巡检")
	}
	stats := keeperStats{}
	detail := "巡检完成"
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		if runID > 0 {
			_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
		}
		return stats, "", err
	}
	filtered := make([]map[string]any, 0, len(authFiles))
	remoteCodexNames := map[string]bool{}
	for _, item := range authFiles {
		if keeperString(item["type"]) != "codex" {
			continue
		}
		name := keeperString(item["name"])
		if name != "" {
			remoteCodexNames[name] = true
		}
		if len(targetSet) == 0 || targetSet[name] {
			filtered = append(filtered, item)
		}
	}
	if len(targetSet) == 0 {
		pruned, err := a.pruneKeeperMissingAuthStates(ctx, remoteCodexNames)
		if err != nil {
			if runID > 0 {
				_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
			}
			return stats, "", err
		}
		if pruned > 0 {
			logFn(fmt.Sprintf("清理本地已不存在的 Codex 账号 %d 个", pruned))
		}
	}
	stats.Total = len(filtered)
	if cfg.CodexKeeper.EnableCredentialWebsockets && !cfg.CodexKeeper.DryRun {
		var websocketFailures []keeperAccountResult
		filtered, websocketFailures = a.ensureKeeperAuthWebsockets(ctx, cfg, options.Mode, filtered, logFn, options.TryLockAuthName, options.UnlockAuthName)
		for _, result := range websocketFailures {
			a.mergeKeeperStats(&stats, result)
			if runID > 0 {
				if err := a.recordKeeperRunAccount(ctx, runID, result); err != nil {
					logFn("写入巡检账号历史失败：" + err.Error())
				}
			}
		}
	}
	if options.UseRefreshCache {
		var skippedNames []string
		filtered, skippedNames, err = a.filterKeeperCachedAuthItems(ctx, filtered, cfg)
		if err != nil {
			if runID > 0 {
				_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
			}
			return stats, "", err
		}
		stats.Skipped += len(skippedNames)
		if err := a.addKeeperCachedAuthStats(ctx, &stats, skippedNames); err != nil {
			if runID > 0 {
				_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
			}
			return stats, "", err
		}
	}
	if len(filtered) == 0 {
		if stats.NetworkError > 0 {
			detail = fmt.Sprintf("巡检完成：网络错误 %d", stats.NetworkError)
		} else if stats.Total > 0 && options.UseRefreshCache {
			detail = "缓存时间内没有需要自动刷新的 Codex auth file"
		} else if len(targetSet) > 0 {
			detail = "未发现指定 Codex auth file"
		} else {
			detail = "未发现 Codex auth file"
		}
		if runID > 0 {
			_ = a.finishKeeperRun(ctx, runID, "completed", detail, stats)
		}
		return stats, detail, nil
	}
	for _, item := range filtered {
		name := keeperString(item["name"])
		locked := false
		if options.TryLockAuthName != nil && name != "" {
			if !options.TryLockAuthName(options.Mode, name) {
				stats.Skipped++
				logFn(name + ": 正在其他 Keeper 任务处理中，跳过")
				continue
			}
			locked = true
		}
		unlock := func() {
			if locked && options.UnlockAuthName != nil {
				options.UnlockAuthName(name)
				locked = false
			}
		}
		if locked && options.UseRefreshCache {
			cutoff := time.Now().In(appTimeLocation).Add(-keeperRefreshCacheDuration(cfg))
			cached, err := a.keeperAuthCheckedSince(ctx, name, cutoff)
			if err != nil {
				unlock()
				if runID > 0 {
					_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
				}
				return stats, "", err
			}
			if cached {
				unlock()
				stats.Skipped++
				if err := a.addKeeperCachedAuthStats(ctx, &stats, []string{name}); err != nil {
					if runID > 0 {
						_ = a.finishKeeperRun(ctx, runID, "failed", err.Error(), stats)
					}
					return stats, "", err
				}
				logFn(name + ": 缓存时间内已刷新，跳过")
				continue
			}
		}
		result := a.processKeeperAuth(ctx, cfg, item, logFn, options.ManualRefresh)
		unlock()
		a.mergeKeeperStats(&stats, result)
		if runID > 0 {
			if err := a.recordKeeperRunAccount(ctx, runID, result); err != nil {
				logFn("写入巡检账号历史失败：" + err.Error())
			}
		}
	}
	if options.Mode == "conditional" {
		detail = fmt.Sprintf("条件刷新完成：健康 %d，坏凭证禁用 %d，恢复启用 %d，优先级降级 %d，优先级恢复 %d，网络错误 %d，缓存跳过 %d", stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.PriorityRestored, stats.NetworkError, stats.Skipped)
	} else if len(targetSet) > 0 {
		detail = fmt.Sprintf("账号刷新完成：健康 %d，凭证异常 %d，恢复启用 %d，优先级降级 %d，优先级恢复 %d，网络错误 %d", stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.PriorityRestored, stats.NetworkError)
	} else {
		detail = fmt.Sprintf("巡检完成：健康 %d，坏凭证禁用 %d，恢复启用 %d，优先级降级 %d，网络错误 %d，缓存跳过 %d", stats.Healthy, stats.StatusDisabled, stats.StatusEnabled, stats.PriorityDegraded, stats.NetworkError, stats.Skipped)
	}
	if runID > 0 {
		_ = a.finishKeeperRun(ctx, runID, "completed", detail, stats)
	}
	return stats, detail, nil
}

func keeperRefreshCacheDuration(cfg AppConfig) time.Duration {
	minutes := cfg.CodexKeeper.AccountRefreshCacheMinutes
	if minutes < 1 {
		minutes = 10
	}
	return time.Duration(minutes) * time.Minute
}

func (a *App) conditionalKeeperRefreshCandidates(ctx context.Context, cfg AppConfig) ([]string, error) {
	cacheWindow := keeperRefreshCacheDuration(cfg)
	since := time.Now().In(appTimeLocation).Add(-cacheWindow)
	aliases, err := a.keeperAuthNameAliases(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{}
	seen := map[string]bool{}
	usageIdentifiers := []string{}
	seenUsageIdentifiers := map[string]bool{}
	addName := func(name string) {
		normalized := strings.TrimSpace(name)
		if normalized == "" || seen[normalized] {
			return
		}
		seen[normalized] = true
		names = append(names, normalized)
	}
	addUsageIdentifier := func(identifier string, allowOpaque bool) bool {
		normalized := strings.TrimSpace(identifier)
		aliasKey := keeperAuthAliasKey(normalized)
		if aliasKey == "" || seenUsageIdentifiers[aliasKey] {
			return len(keeperAuthNamesForUsageIdentifier(normalized, aliases)) > 0
		}
		if !allowOpaque && !keeperLooksLikeAuthIdentifier(normalized, aliases) {
			return false
		}
		seenUsageIdentifiers[aliasKey] = true
		usageIdentifiers = append(usageIdentifiers, normalized)
		resolved := false
		for _, name := range keeperAuthNamesForUsageIdentifier(normalized, aliases) {
			addName(name)
			resolved = true
		}
		return resolved
	}

	rows, err := a.db.QueryContext(ctx, `
		SELECT source, raw_json
		FROM usage_records
		WHERE timestamp >= ?
		ORDER BY timestamp DESC
	`, dbTime(since))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var source sql.NullString
		var rawJSON string
		if err := rows.Scan(&source, &rawJSON); err != nil {
			_ = rows.Close()
			return nil, err
		}
		sourceResolved := false
		if source.Valid {
			sourceResolved = addUsageIdentifier(source.String, false)
		}
		if identifier := rawJSONStringField(rawJSON, "source"); identifier != nil {
			sourceResolved = addUsageIdentifier(*identifier, false) || sourceResolved
		}
		if sourceResolved {
			continue
		}
		for _, field := range []string{"auth_index", "authIndex", "index", "auth_name", "authName", "account_id", "accountId"} {
			if identifier := rawJSONStringField(rawJSON, field); identifier != nil {
				addUsageIdentifier(*identifier, true)
			}
		}
		for _, field := range []string{"email", "account_email", "accountEmail", "user_email", "userEmail"} {
			if identifier := rawJSONStringField(rawJSON, field); identifier != nil {
				addUsageIdentifier(*identifier, false)
			}
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range a.keeperAuthNamesFromRemoteUsageIdentifiers(ctx, cfg, usageIdentifiers, aliases) {
		addName(name)
	}
	if err := a.reconcileKeeperConditionalRemoteAuthStates(ctx, cfg, addName); err != nil {
		return nil, err
	}

	rows, err = a.db.QueryContext(ctx, `
		SELECT auth_name
		FROM codex_keeper_auth_states
		WHERE disabled = 0
		  AND (
		      (primary_reset_at IS NOT NULL AND primary_reset_at <= ?)
		   OR (secondary_reset_at IS NOT NULL AND secondary_reset_at <= ?)
		  )
		ORDER BY auth_name
	`, dbTime(time.Now().In(appTimeLocation)), dbTime(time.Now().In(appTimeLocation)))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		addName(name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = a.db.QueryContext(ctx, `
		SELECT auth_name
		FROM codex_keeper_auth_states
		WHERE disabled = 0
		  AND last_error IS NOT NULL
		  AND TRIM(last_error) <> ''
		ORDER BY auth_name
	`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		addName(name)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	enabledNames, err := a.filterKeeperEnabledAuthNames(ctx, names)
	if err != nil {
		return nil, err
	}
	filtered, _, err := a.filterKeeperCachedAuthNames(ctx, enabledNames, cfg)
	return filtered, err
}

func (a *App) reconcileKeeperConditionalRemoteAuthStates(ctx context.Context, cfg AppConfig, addName func(string)) error {
	if strings.TrimSpace(cfg.Collector.CLIProxyURL) == "" {
		return nil
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return err
	}
	remoteNames := map[string]bool{}
	refreshableRemoteNames := map[string]bool{}
	for _, item := range authFiles {
		if keeperString(item["type"]) != "codex" {
			continue
		}
		name := keeperString(item["name"])
		if name == "" {
			continue
		}
		remoteNames[name] = true
		if !keeperBool(item["disabled"]) {
			refreshableRemoteNames[name] = true
		}
	}
	localNames, err := a.keeperAuthStateNameSet(ctx)
	if err != nil {
		return err
	}
	for name := range refreshableRemoteNames {
		if !localNames[name] && !a.keeperRemoteAuthDisabledForConditional(ctx, cfg, name, authFiles) {
			addName(name)
		}
	}
	_, err = a.pruneKeeperMissingAuthStates(ctx, remoteNames)
	return err
}

func (a *App) keeperRemoteAuthDisabledForConditional(ctx context.Context, cfg AppConfig, name string, authFiles []map[string]any) bool {
	for _, item := range authFiles {
		if keeperString(item["name"]) != name {
			continue
		}
		if keeperBool(item["disabled"]) {
			return true
		}
		if _, ok := item["disabled"]; ok {
			return false
		}
		detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
		if err != nil || detail == nil {
			return false
		}
		return keeperBool(mergeKeeperObjects(item, detail)["disabled"])
	}
	return false
}

func (a *App) keeperAuthStateNameSet(ctx context.Context) (map[string]bool, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT auth_name FROM codex_keeper_auth_states`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func (a *App) keeperAuthNameAliases(ctx context.Context) (map[string][]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email
		FROM codex_keeper_auth_states
		WHERE disabled = 0
		ORDER BY auth_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aliases := map[string][]string{}
	for rows.Next() {
		var name string
		var email sql.NullString
		if err := rows.Scan(&name, &email); err != nil {
			return nil, err
		}
		addKeeperAuthAlias(aliases, name, name)
		if email.Valid {
			addKeeperAuthAlias(aliases, email.String, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aliases, nil
}

func (a *App) keeperAuthNamesFromRemoteUsageIdentifiers(ctx context.Context, cfg AppConfig, identifiers []string, aliases map[string][]string) []string {
	if !keeperHasUnresolvedUsageIdentifiers(identifiers, aliases) || strings.TrimSpace(cfg.Collector.CLIProxyURL) == "" {
		return nil
	}
	authFiles, err := a.listKeeperRemoteAuthFiles(ctx, cfg)
	if err != nil {
		return nil
	}
	codexFiles := make([]map[string]any, 0, len(authFiles))
	for _, item := range authFiles {
		if keeperString(item["type"]) != "codex" {
			continue
		}
		if keeperBool(item["disabled"]) {
			continue
		}
		codexFiles = append(codexFiles, item)
		addKeeperAuthObjectAliases(aliases, item)
	}
	if !keeperHasUnresolvedUsageIdentifiers(identifiers, aliases) {
		return keeperAuthNamesForUsageIdentifiers(identifiers, aliases)
	}
	for _, item := range codexFiles {
		name := keeperString(item["name"])
		if name == "" {
			continue
		}
		detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
		if err != nil || detail == nil {
			continue
		}
		merged := mergeKeeperObjects(item, detail)
		if keeperBool(merged["disabled"]) {
			continue
		}
		addKeeperAuthObjectAliases(aliases, merged)
		if !keeperHasUnresolvedUsageIdentifiers(identifiers, aliases) {
			break
		}
	}
	return keeperAuthNamesForUsageIdentifiers(identifiers, aliases)
}

func keeperHasUnresolvedUsageIdentifiers(identifiers []string, aliases map[string][]string) bool {
	for _, identifier := range identifiers {
		if len(keeperAuthNamesForUsageIdentifier(identifier, aliases)) == 0 {
			return true
		}
	}
	return false
}

func keeperAuthNamesForUsageIdentifiers(identifiers []string, aliases map[string][]string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, identifier := range identifiers {
		for _, name := range keeperAuthNamesForUsageIdentifier(identifier, aliases) {
			normalized := strings.TrimSpace(name)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			names = append(names, normalized)
		}
	}
	return names
}

func keeperAuthNamesForUsageIdentifier(identifier string, aliases map[string][]string) []string {
	normalized := strings.TrimSpace(identifier)
	if normalized == "" {
		return nil
	}
	if names := aliases[keeperAuthAliasKey(normalized)]; len(names) > 0 {
		return names
	}
	if strings.HasSuffix(strings.ToLower(normalized), ".json") {
		return []string{normalized}
	}
	return nil
}

func keeperLooksLikeAuthIdentifier(identifier string, aliases map[string][]string) bool {
	normalized := strings.TrimSpace(identifier)
	if normalized == "" {
		return false
	}
	if len(aliases[keeperAuthAliasKey(normalized)]) > 0 {
		return true
	}
	lower := strings.ToLower(normalized)
	return strings.HasSuffix(lower, ".json") || strings.Contains(normalized, "@")
}

func addKeeperAuthObjectAliases(aliases map[string][]string, object map[string]any) {
	name := keeperString(object["name"])
	if name == "" {
		return
	}
	addKeeperAuthAlias(aliases, name, name)
	for _, key := range []string{"auth_name", "authName", "auth_index", "authIndex", "index", "source", "email", "account_email", "accountEmail", "user_email", "userEmail", "account_id", "accountId"} {
		if value := keeperAliasString(object[key]); value != "" {
			addKeeperAuthAlias(aliases, value, name)
		}
	}
}

func addKeeperAuthAlias(aliases map[string][]string, alias string, name string) {
	normalizedAlias := keeperAuthAliasKey(alias)
	normalizedName := strings.TrimSpace(name)
	if normalizedAlias == "" || normalizedName == "" {
		return
	}
	for _, existing := range aliases[normalizedAlias] {
		if existing == normalizedName {
			return
		}
	}
	aliases[normalizedAlias] = append(aliases[normalizedAlias], normalizedName)
}

func keeperAliasString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func keeperAuthAliasKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (a *App) filterKeeperCachedAuthItems(ctx context.Context, items []map[string]any, cfg AppConfig) ([]map[string]any, []string, error) {
	if len(items) == 0 {
		return items, nil, nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name := keeperString(item["name"]); name != "" {
			names = append(names, name)
		}
	}
	allowedNames, skippedNames, err := a.filterKeeperCachedAuthNames(ctx, names, cfg)
	if err != nil {
		return nil, nil, err
	}
	allowed := map[string]bool{}
	for _, name := range allowedNames {
		allowed[name] = true
	}
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		name := keeperString(item["name"])
		if name == "" || allowed[name] {
			filtered = append(filtered, item)
		}
	}
	return filtered, skippedNames, nil
}

func (a *App) filterKeeperCachedAuthNames(ctx context.Context, names []string, cfg AppConfig) ([]string, []string, error) {
	normalized, err := normalizeOptionalKeeperAuthNames(names)
	if err != nil {
		return nil, nil, err
	}
	if len(normalized) == 0 {
		return normalized, nil, nil
	}
	cutoff := time.Now().In(appTimeLocation).Add(-keeperRefreshCacheDuration(cfg))
	filtered := make([]string, 0, len(normalized))
	skipped := []string{}
	for _, name := range normalized {
		cached, err := a.keeperAuthCheckedSince(ctx, name, cutoff)
		if err != nil {
			return nil, nil, err
		}
		if cached {
			skipped = append(skipped, name)
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered, skipped, nil
}

func (a *App) filterKeeperEnabledAuthNames(ctx context.Context, names []string) ([]string, error) {
	normalized, err := normalizeOptionalKeeperAuthNames(names)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return normalized, nil
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name
		FROM codex_keeper_auth_states
		WHERE disabled = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	disabledNames := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		disabledNames[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(normalized))
	for _, name := range normalized {
		if disabledNames[name] {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered, nil
}

func (a *App) addKeeperCachedAuthStats(ctx context.Context, stats *keeperStats, names []string) error {
	cachedStats, err := a.keeperCachedAuthStats(ctx, names)
	if err != nil {
		return err
	}
	stats.add(cachedStats)
	return nil
}

func (a *App) keeperCachedAuthStats(ctx context.Context, names []string) (keeperStats, error) {
	normalized, err := normalizeOptionalKeeperAuthNames(names)
	if err != nil {
		return keeperStats{}, err
	}
	stats := keeperStats{}
	for _, name := range normalized {
		state, err := a.getKeeperState(ctx, name)
		if err != nil {
			var appErr *AppError
			if errors.As(err, &appErr) && appErr.Code == "not_found" {
				continue
			}
			return keeperStats{}, err
		}
		stats.mergeCachedState(*state)
	}
	return stats, nil
}

func (a *App) keeperAuthCheckedSince(ctx context.Context, name string, cutoff time.Time) (bool, error) {
	var lastChecked sql.NullString
	err := a.db.QueryRowContext(ctx, `
		SELECT CAST(last_checked_at AS TEXT)
		FROM codex_keeper_auth_states
		WHERE auth_name = ?
	`, name).Scan(&lastChecked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !lastChecked.Valid {
		return false, nil
	}
	checkedAt, ok := parseDBTime(lastChecked.String)
	if !ok {
		return false, nil
	}
	return checkedAt.After(cutoff) || checkedAt.Equal(cutoff), nil
}

func (a *App) ensureKeeperAuthWebsockets(
	ctx context.Context,
	cfg AppConfig,
	mode string,
	items []map[string]any,
	logFn func(string),
	tryLock func(string, string) bool,
	unlock func(string),
) ([]map[string]any, []keeperAccountResult) {
	if len(items) == 0 {
		return items, nil
	}
	remaining := make([]map[string]any, 0, len(items))
	failures := []keeperAccountResult{}
	now := time.Now().In(appTimeLocation)
	for _, item := range items {
		name := keeperString(item["name"])
		if name == "" || keeperBool(item["websockets"]) {
			remaining = append(remaining, item)
			continue
		}
		locked := false
		if tryLock != nil && unlock != nil {
			if !tryLock(mode, name) {
				remaining = append(remaining, item)
				continue
			}
			locked = true
		}
		unlockIfNeeded := func() {
			if locked && unlock != nil {
				unlock(name)
				locked = false
			}
		}
		if err := a.setKeeperRemoteWebsockets(ctx, cfg, name); err != nil {
			message := "启用 WebSocket 传输失败：" + err.Error()
			disabled := keeperBool(item["disabled"])
			result := keeperAccountResult{
				Name:         name,
				AuthIndex:    keeperRemoteAuthIndex(item),
				AccountType:  accountTypeFromKeeperDetail(item, nil),
				Disabled:     &disabled,
				Priority:     keeperIntPtr(item["priority"]),
				Result:       "network_error",
				LastError:    &message,
				LatestAction: &message,
				CheckedAt:    now,
			}
			_ = a.upsertKeeperState(ctx, result)
			logFn(name + ": " + message)
			failures = append(failures, result)
			unlockIfNeeded()
			continue
		}
		item["websockets"] = true
		unlockIfNeeded()
		logFn(name + ": 已启用 WebSocket 传输")
		remaining = append(remaining, item)
	}
	return remaining, failures
}

func (a *App) processKeeperAuth(ctx context.Context, cfg AppConfig, authInfo map[string]any, logFn func(string), manualRefresh bool) keeperAccountResult {
	now := time.Now().In(appTimeLocation)
	name := keeperString(authInfo["name"])
	if name == "" {
		name = "unknown"
	}
	result := keeperAccountResult{Name: name, Result: "skipped", CheckedAt: now}
	detail, err := a.getKeeperRemoteAuthFile(ctx, cfg, name)
	if err != nil {
		message := "读取 auth file 详情失败：" + err.Error()
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + message)
		return result
	}
	if detail == nil {
		message := "读取 auth file 详情失败"
		result.Result = "network_error"
		result.LastError = &message
		result.LatestAction = &message
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	merged := mergeKeeperObjects(authInfo, detail)
	result.Email = keeperStringPtr(merged["email"], merged["account_email"], merged["user_email"])
	result.AuthIndex = keeperRemoteAuthIndex(merged)
	result.Priority = keeperIntPtr(merged["priority"])
	disabled := keeperBool(merged["disabled"])
	result.Disabled = &disabled
	result.AccountType = accountTypeFromKeeperDetail(merged, nil)
	var state *keeperAuthState
	var restorePriority *int
	if loadedState, err := a.getKeeperState(ctx, name); err == nil {
		state = loadedState
		restorePriority = loadedState.RestorePriority
	}
	recoverableUnauthorizedDisabled := disabled && isKeeperRecoverableUnauthorizedDisabledState(state)
	if disabled && !manualRefresh && !recoverableUnauthorizedDisabled {
		result.Result = "disabled"
		a.preserveKeeperBadCredentialDiagnosis(ctx, &result)
		_ = a.upsertKeeperState(ctx, result)
		return result
	}
	if keeperString(merged["access_token"]) == "" {
		message := "缺少 access token"
		action := "刷新发现凭证不可用：" + message
		if !cfg.CodexKeeper.DryRun {
			if !disabled {
				if err := a.setKeeperRemoteDisabled(ctx, cfg, name, true); err != nil {
					message = "禁用坏凭证失败：" + err.Error()
					result.LastError = &message
					result.Result = "network_error"
					_ = a.upsertKeeperState(ctx, result)
					return result
				}
			}
			_ = a.setKeeperRemotePriority(ctx, cfg, name, nil)
			disabled = true
			result.Disabled = &disabled
			result.Priority = nil
			action = "禁用凭证：" + message
		} else {
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
		if !cfg.CodexKeeper.DryRun {
			if !disabled {
				if err := a.setKeeperRemoteDisabled(ctx, cfg, name, true); err != nil {
					message = "禁用坏凭证失败：" + err.Error()
					result.Result = "network_error"
					result.LastError = &message
					_ = a.upsertKeeperState(ctx, result)
					return result
				}
			}
			_ = a.setKeeperRemotePriority(ctx, cfg, name, nil)
			disabled = true
			result.Disabled = &disabled
			result.Priority = nil
			action = "禁用凭证：" + message
		} else {
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
	result.PrimaryWindowSeconds = usage.PrimaryWindowSeconds
	result.SecondaryWindowSeconds = usage.SecondaryWindowSeconds
	result.QuotaThreshold = &cfg.CodexKeeper.QuotaThreshold
	result.Result = "healthy"

	if recoverableUnauthorizedDisabled {
		action := fmt.Sprintf("恢复启用：usage 检测恢复 HTTP %d", *usageResult.StatusCode)
		if !cfg.CodexKeeper.DryRun {
			if err := a.setKeeperRemoteDisabled(ctx, cfg, name, false); err != nil {
				message := "恢复启用失败：" + err.Error()
				result.Result = "network_error"
				result.LastError = &message
				result.LatestAction = &message
				_ = a.upsertKeeperState(ctx, result)
				logFn(name + ": " + message)
				return result
			}
			disabled = false
			result.Disabled = &disabled
		} else {
			action = "模拟" + action
		}
		result.Result = "status_enabled"
		result.LatestAction = &action
		result.ClearRestorePriority = true
		result.LastError = nil
		_ = a.upsertKeeperState(ctx, result)
		logFn(name + ": " + action)
		return result
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
		if manualRefresh {
			action := fmt.Sprintf("刷新完成，类型 %s", accountType)
			result.LatestAction = &action
			logFn(name + ": " + action)
		} else {
			logFn(fmt.Sprintf("%s: 巡检正常，类型 %s", name, accountType))
		}
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

func (stats *keeperStats) add(delta keeperStats) {
	stats.Total += delta.Total
	stats.Healthy += delta.Healthy
	stats.StatusDisabled += delta.StatusDisabled
	stats.StatusEnabled += delta.StatusEnabled
	stats.PriorityDegraded += delta.PriorityDegraded
	stats.PriorityRestored += delta.PriorityRestored
	stats.Skipped += delta.Skipped
	stats.NetworkError += delta.NetworkError
}

func (stats *keeperStats) mergeCachedState(state keeperAuthState) {
	if isKeeperCachedBadCredentialState(state) {
		stats.StatusDisabled++
		return
	}
	if state.Disabled {
		return
	}
	if state.LastError != nil && strings.TrimSpace(*state.LastError) != "" {
		stats.NetworkError++
		return
	}
	if state.Priority != nil && *state.Priority == -1 {
		stats.PriorityDegraded++
		return
	}
	if state.LastHealthyAt != nil || state.LastCheckedAt != nil {
		stats.Healthy++
	}
}

func isKeeperCachedBadCredentialState(state keeperAuthState) bool {
	if isKeeperBadCredentialDisableAction(state.LatestAction) {
		return true
	}
	if state.LastStatusCode != nil && (*state.LastStatusCode == http.StatusUnauthorized || *state.LastStatusCode == http.StatusPaymentRequired) {
		return true
	}
	if state.LatestAction != nil && strings.HasPrefix(strings.TrimSpace(*state.LatestAction), "模拟禁用") {
		return true
	}
	if state.LastError == nil {
		return false
	}
	message := strings.TrimSpace(*state.LastError)
	return strings.Contains(message, "缺少 access token") || strings.Contains(message, "凭证不可用")
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

func (a *App) setKeeperRemoteWebsockets(ctx context.Context, cfg AppConfig, name string) error {
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodPatch, "/v0/management/auth-files/fields", nil, map[string]any{"name": name, "websockets": true}, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) deleteKeeperRemoteAuthFile(ctx context.Context, cfg AppConfig, name string) error {
	query := url.Values{"name": []string{name}}
	_, _, err := a.keeperRequest(ctx, cfg, http.MethodDelete, "/v0/management/auth-files", query, nil, time.Duration(cfg.CodexKeeper.CPATimeoutSeconds)*time.Second)
	return err
}

func (a *App) keeperRequest(ctx context.Context, cfg AppConfig, method, path string, query url.Values, body any, timeout time.Duration) (*http.Response, []byte, error) {
	attempts := keeperRequestAttempts(cfg.CodexKeeper)
	target := makeURL(cfg.Collector.CLIProxyURL, path, query)
	headers := managementHeaders(cfg.Collector.ManagementKey)
	var lastResponse *http.Response
	var lastPayload []byte
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		response, payload, err := doJSON(ctx, httpClient(timeout), method, target, headers, body)
		lastResponse = response
		lastPayload = payload
		if err != nil {
			lastErr = validationError("CLIProxyAPI 管理请求失败：" + err.Error())
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, err) {
				continue
			}
			return nil, nil, lastErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = validationError(fmt.Sprintf("CLIProxyAPI 管理请求失败：HTTP %d", response.StatusCode))
			if keeperShouldRetryRequest(ctx, attempt, attempts, response, nil) {
				continue
			}
			return response, payload, lastErr
		}
		return response, payload, nil
	}
	return lastResponse, lastPayload, lastErr
}

func keeperRequestAttempts(cfg KeeperConfig) int {
	return clampInt(cfg.MaxRetries, 0, 5, 2) + 1
}

func keeperShouldRetryRequest(ctx context.Context, attempt, attempts int, response *http.Response, err error) bool {
	if attempt >= attempts || ctx.Err() != nil {
		return false
	}
	if err != nil {
		return true
	}
	return response != nil && response.StatusCode >= 500
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
		SELECT auth_name, email, auth_index, account_type, disabled, priority, primary_used_percent,
		       secondary_used_percent, CAST(primary_reset_at AS TEXT), CAST(secondary_reset_at AS TEXT), quota_threshold,
		       last_status_code, last_error, latest_action, CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT),
		       primary_window_seconds, secondary_window_seconds, restore_priority, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := a.mergeKeeperQuotaResetCounts(ctx, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

// mergeKeeperQuotaResetCounts fills QuotaResetCount/LastQuotaResetAt from the
// codex_keeper_quota_resets table (rows without an entry keep the zero count).
func (a *App) mergeKeeperQuotaResetCounts(ctx context.Context, accounts []keeperAccount) error {
	if len(accounts) == 0 {
		return nil
	}
	rows, err := a.db.QueryContext(ctx, `SELECT auth_name, reset_count, CAST(last_reset_at AS TEXT) FROM codex_keeper_quota_resets`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type resetInfo struct {
		count  int
		lastAt *time.Time
	}
	counts := map[string]resetInfo{}
	for rows.Next() {
		var name string
		var count int
		var lastAt sql.NullString
		if err := rows.Scan(&name, &count, &lastAt); err != nil {
			return err
		}
		counts[name] = resetInfo{count: count, lastAt: timePtr(lastAt)}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range accounts {
		if info, ok := counts[accounts[i].Name]; ok {
			accounts[i].QuotaResetCount = info.count
			accounts[i].LastQuotaResetAt = info.lastAt
		}
	}
	return nil
}

// resetKeeperQuota asks CLIProxyAPI to reset the quota/cooldown state of one
// auth (by its auth_index) and records the reset in the local counter table.
// The CLIProxyAPI call must succeed before the counter is incremented.
// keeperQuotaResetResult is the deliberately minimal wire shape of a reset:
// it must not leak internal keeperAccount fields (auth_index, email, errors).
type keeperQuotaResetResult struct {
	Name             string  `json:"name"`
	QuotaResetCount  int     `json:"quota_reset_count"`
	LastQuotaResetAt *string `json:"last_quota_reset_at"`
}

func (a *App) resetKeeperQuota(ctx context.Context, authName string) (keeperQuotaResetResult, error) {
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return keeperQuotaResetResult{}, err
	}
	state, err := a.getKeeperState(ctx, authName)
	if err != nil {
		return keeperQuotaResetResult{}, err
	}
	if state == nil {
		return keeperQuotaResetResult{}, notFoundError("账号不存在")
	}
	if state.AuthIndex == nil || strings.TrimSpace(*state.AuthIndex) == "" {
		return keeperQuotaResetResult{}, validationError("该账号缺少 auth_index，请先刷新账号列表")
	}
	authIndex := strings.TrimSpace(*state.AuthIndex)
	timeout := time.Duration(cfg.CodexKeeper.CPATimeoutSeconds) * time.Second
	_, payload, err := a.keeperRequest(ctx, cfg, http.MethodPost, "/v0/management/reset-quota", nil,
		map[string]any{"auth_index": authIndex}, timeout)
	if err != nil {
		return keeperQuotaResetResult{}, err
	}
	// A 2xx alone is not proof of a reset: require the CLIProxyAPI response to
	// confirm status=ok for the exact auth_index we asked about, otherwise fail
	// closed and do not count the reset.
	var cpaResult struct {
		Status    string `json:"status"`
		AuthIndex string `json:"auth_index"`
	}
	// authIndex was already trimmed before the request; require CPA to echo it
	// exactly (no lenient trimming of the response) to honor the exact-match contract.
	if err := json.Unmarshal(payload, &cpaResult); err != nil ||
		cpaResult.Status != "ok" || cpaResult.AuthIndex != authIndex {
		return keeperQuotaResetResult{}, validationError("CLIProxyAPI 未确认重置成功（响应缺少 status=ok 或 auth_index 不匹配）")
	}
	now := dbTime(time.Now())
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_quota_resets (auth_name, reset_count, last_reset_at)
		VALUES (?, 1, ?)
		ON CONFLICT(auth_name) DO UPDATE SET
			reset_count = codex_keeper_quota_resets.reset_count + 1,
			last_reset_at = excluded.last_reset_at
	`, authName, now); err != nil {
		return keeperQuotaResetResult{}, err
	}
	var count int
	var lastAt sql.NullString
	if err := a.db.QueryRowContext(ctx, `SELECT reset_count, CAST(last_reset_at AS TEXT) FROM codex_keeper_quota_resets WHERE auth_name = ?`, authName).Scan(&count, &lastAt); err != nil {
		return keeperQuotaResetResult{}, err
	}
	return keeperQuotaResetResult{
		Name:             authName,
		QuotaResetCount:  count,
		LastQuotaResetAt: apiDateTimePtr(timePtr(lastAt)),
	}, nil
}

func (a *App) pruneKeeperMissingAuthStates(ctx context.Context, remoteNames map[string]bool) (int, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT auth_name FROM codex_keeper_auth_states`)
	if err != nil {
		return 0, err
	}
	stale := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if !remoteNames[name] {
			stale = append(stale, name)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(stale) == 0 {
		return 0, nil
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM codex_keeper_auth_states WHERE auth_name = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	pruned := 0
	for _, name := range stale {
		result, err := stmt.ExecContext(ctx, name)
		if err != nil {
			return 0, err
		}
		affected, _ := result.RowsAffected()
		pruned += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return pruned, nil
}

func (a *App) getKeeperState(ctx context.Context, name string) (*keeperAuthState, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT auth_name, email, auth_index, account_type, disabled, priority, primary_used_percent,
		       secondary_used_percent, CAST(primary_reset_at AS TEXT), CAST(secondary_reset_at AS TEXT), quota_threshold,
		       last_status_code, last_error, latest_action, CAST(last_checked_at AS TEXT), CAST(last_healthy_at AS TEXT),
		       primary_window_seconds, secondary_window_seconds, restore_priority, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
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
	var email, authIndex, accountType, primaryReset, secondaryReset, lastError, latestAction, lastChecked, lastHealthy, createdAt, updatedAt sql.NullString
	var priority, primaryUsed, secondaryUsed, quotaThreshold, lastStatus, primaryWindowSeconds, secondaryWindowSeconds, restorePriority sql.NullInt64
	err := scanner.Scan(
		&state.Name, &email, &authIndex, &accountType, &state.Disabled, &priority, &primaryUsed,
		&secondaryUsed, &primaryReset, &secondaryReset, &quotaThreshold, &lastStatus,
		&lastError, &latestAction, &lastChecked, &lastHealthy, &primaryWindowSeconds, &secondaryWindowSeconds, &restorePriority,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return keeperAuthState{}, err
	}
	state.Email = nullableString(email)
	state.AuthIndex = nullableString(authIndex)
	state.AccountType = keeperAccountTypeOrUnknown(nullableString(accountType))
	state.Priority = nullableInt(priority)
	state.PrimaryUsedPercent = nullableInt(primaryUsed)
	state.SecondaryUsedPercent = nullableInt(secondaryUsed)
	state.PrimaryResetAt = timePtr(primaryReset)
	state.SecondaryResetAt = timePtr(secondaryReset)
	state.PrimaryWindowSeconds = nullableInt(primaryWindowSeconds)
	state.SecondaryWindowSeconds = nullableInt(secondaryWindowSeconds)
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
	if result.Result == "healthy" || result.Result == "status_enabled" || result.Result == "priority_degraded" || result.Result == "priority_restored" {
		lastHealthy = checkedAt
	}
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, auth_index, account_type, disabled, priority, restore_priority, latest_action, last_error,
			last_status_code, primary_used_percent, secondary_used_percent, quota_threshold,
			primary_reset_at, secondary_reset_at, primary_window_seconds, secondary_window_seconds,
			last_checked_at, last_healthy_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(auth_name) DO UPDATE SET
			email = excluded.email,
			auth_index = excluded.auth_index,
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
			primary_window_seconds = excluded.primary_window_seconds,
			secondary_window_seconds = excluded.secondary_window_seconds,
			last_checked_at = excluded.last_checked_at,
			last_healthy_at = COALESCE(excluded.last_healthy_at, codex_keeper_auth_states.last_healthy_at),
			updated_at = excluded.updated_at
	`, result.Name, result.Email, result.AuthIndex, result.AccountType, boolValue(result.Disabled), result.Priority, result.RestorePriority, result.LatestAction, result.LastError, result.LastStatusCode, result.PrimaryUsedPercent, result.SecondaryUsedPercent, result.QuotaThreshold, dbTimePtr(result.PrimaryResetAt), dbTimePtr(result.SecondaryResetAt), result.PrimaryWindowSeconds, result.SecondaryWindowSeconds, checkedAt, lastHealthy, now, now, result.ClearRestorePriority)
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
	return scanKeeperRunRecord(row)
}

func (a *App) latestKeeperRunByMode(ctx context.Context, mode string) (*keeperRunRecord, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT mode, state, detail, CAST(started_at AS TEXT), CAST(finished_at AS TEXT), total, healthy, status_disabled,
		       status_enabled, priority_degraded, priority_restored, skipped, network_error
		FROM codex_keeper_runs WHERE mode = ? ORDER BY id DESC LIMIT 1
	`, mode)
	return scanKeeperRunRecord(row)
}

func scanKeeperRunRecord(row interface {
	Scan(dest ...any) error
}) (*keeperRunRecord, error) {
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
	} else if value := keeperString(payload["planType"]); value != "" {
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
	usage.PrimaryWindowSeconds = quotaWindowSeconds(primary)
	usage.SecondaryWindowSeconds = quotaWindowSeconds(secondary)
	return usage
}

func quotaWindowSeconds(window map[string]any) *int {
	if window == nil {
		return nil
	}
	value := keeperIntPtr(
		window["limit_window_seconds"],
		window["limitWindowSeconds"],
		window["window_seconds"],
		window["windowSeconds"],
		window["rolling_window_seconds"],
		window["rollingWindowSeconds"],
	)
	if value == nil || *value <= 0 {
		return nil
	}
	return value
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
	values = append(values, keeperAccountTypeValues(detail)...)
	text := strings.ToLower(strings.Join(values, " "))
	text = strings.NewReplacer("-", "_", " ", "_", ".", "_", "@", "_", "/", "_", "\\", "_").Replace(text)
	bounded := "_" + text + "_"
	var result string
	switch {
	case strings.Contains(bounded, "_k12_"):
		result = "k12"
	case strings.Contains(text, "prolite") || strings.Contains(text, "pro_lite") || strings.Contains(text, "5x") || strings.Contains(text, "pro_5"):
		result = "pro_5x"
	case strings.Contains(text, "20x") || strings.Contains(text, "pro_20") || strings.Contains(bounded, "_pro_"):
		result = "pro_20x"
	case strings.Contains(text, "team") || strings.Contains(text, "business"):
		result = "team"
	case strings.Contains(text, "plus"):
		result = "plus"
	case strings.Contains(text, "free"):
		result = "free"
	default:
		result = "unknown"
	}
	return &result
}

func keeperAccountTypeOrUnknown(accountType *string) *string {
	if accountType == nil || strings.TrimSpace(*accountType) == "" {
		result := "unknown"
		return &result
	}
	result := strings.ToLower(strings.TrimSpace(*accountType))
	return &result
}

func keeperAccountTypeValues(detail map[string]any) []string {
	if detail == nil {
		return nil
	}
	values := []string{}
	appendString := func(value any) {
		if text := keeperString(value); text != "" {
			values = append(values, text)
		}
	}
	for _, key := range []string{"plan_type", "planType", "plan", "tier", "account_plan", "subscription_plan", "sku", "account_type", "accountType"} {
		appendString(detail[key])
	}
	for _, key := range []string{"attributes", "metadata"} {
		if object, ok := detail[key].(map[string]any); ok {
			for _, nestedKey := range []string{"plan_type", "planType", "chatgpt_plan_type", "chatgptPlanType"} {
				appendString(object[nestedKey])
			}
			values = append(values, keeperIDTokenPlanValues(object["id_token"])...)
		}
	}
	values = append(values, keeperIDTokenPlanValues(detail["id_token"])...)
	for _, key := range []string{"name", "file_name", "filename"} {
		appendString(detail[key])
	}
	if path := keeperString(detail["path"]); path != "" {
		values = append(values, filepath.Base(path))
	}
	return values
}

func keeperIDTokenPlanValues(value any) []string {
	object := keeperIDTokenClaims(value)
	if object == nil {
		return nil
	}
	values := []string{}
	appendString := func(value any) {
		if text := keeperString(value); text != "" {
			values = append(values, text)
		}
	}
	for _, key := range []string{"plan_type", "planType", "chatgpt_plan_type", "chatgptPlanType"} {
		appendString(object[key])
	}
	if authInfo, ok := object["https://api.openai.com/auth"].(map[string]any); ok {
		for _, key := range []string{"plan_type", "planType", "chatgpt_plan_type", "chatgptPlanType"} {
			appendString(authInfo[key])
		}
	}
	return values
}

func keeperIDTokenClaims(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return typed
	case string:
		token := strings.TrimSpace(typed)
		if token == "" {
			return nil
		}
		var object map[string]any
		if json.Unmarshal([]byte(token), &object) == nil {
			return object
		}
		parts := strings.Split(token, ".")
		if len(parts) < 2 {
			return nil
		}
		decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(parts[1])
		}
		if err != nil {
			return nil
		}
		if json.Unmarshal(decoded, &object) != nil {
			return nil
		}
		return object
	default:
		return nil
	}
}

func keeperPriorityForType(accountType *string, rules map[string]int) *int {
	key := "unknown"
	if accountType != nil {
		if normalized := strings.ToLower(strings.TrimSpace(*accountType)); normalized != "" {
			key = normalized
		}
	}
	value, ok := normalizePriorityRules(rules)[key]
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

func (a *App) preserveKeeperBadCredentialDiagnosis(ctx context.Context, result *keeperAccountResult) {
	state, err := a.getKeeperState(ctx, result.Name)
	if err != nil || !isKeeperBadCredentialDisableAction(state.LatestAction) {
		return
	}
	result.LastStatusCode = state.LastStatusCode
	result.LastError = cloneStringPtr(state.LastError)
	result.LatestAction = cloneStringPtr(state.LatestAction)
}

func isKeeperRecoverableUnauthorizedDisabledState(state *keeperAuthState) bool {
	return state != nil &&
		state.Disabled &&
		state.LastStatusCode != nil &&
		*state.LastStatusCode == http.StatusUnauthorized &&
		isKeeperBadCredentialDisableAction(state.LatestAction)
}

func isKeeperBadCredentialDisableAction(action *string) bool {
	if action == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(*action), "禁用凭证")
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

func keeperRemoteAuthIndex(detail map[string]any) *string {
	for _, key := range []string{"auth_index", "authIndex", "index"} {
		if value := keeperString(detail[key]); value != "" {
			return &value
		}
	}
	return nil
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
