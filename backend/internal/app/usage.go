package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var usageEmailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

type UsageFilters struct {
	Scope             string
	Start             *time.Time
	End               *time.Time
	UserID            *int
	UsageUsername     *string
	APIKeyDescription *string
	Provider          *string
	Model             *string
	SourceKey         *string
	Endpoint          *string
	Failed            *bool
	RequestID         *string
}

type UsageRecord struct {
	ID                  int
	Timestamp           time.Time
	UsageUsername       *string
	APIKeyDescription   *string
	Provider            *string
	Model               *string
	ReasoningEffort     *string
	Endpoint            *string
	Source              *string
	SourceAccount       *string
	RequestID           *string
	Auth                *string
	AuthIndex           *string
	LatencyMS           *float64
	TTFTMS              *float64
	Failed              bool
	InputTokens         int
	OutputTokens        int
	CachedTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	ReasoningTokens     int
	TotalTokens         int
	DedupeKey           string
	RawJSON             string
}

func usageAPITime(value time.Time) string {
	return apiDateTime(value)
}

func usageAPITimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := usageAPITime(*value)
	return &formatted
}

type usageAccessScope struct {
	UserID   int
	Username string
	IsAdmin  bool
}

type usageRedactionOptions struct {
	MaskSource    bool
	MaskAuthIndex bool
}

func (a *App) handleUsage(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}
	parts := splitPath(r.URL.Path, "/api/usage/")
	if len(parts) == 0 {
		return notFoundError("Not Found")
	}
	filters, err := parseUsageFilters(r)
	if err != nil {
		return err
	}
	switch parts[0] {
	case "summary":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.usageSummary(w, r, filters, user)
	case "trends":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.usageTrends(w, r, filters, user)
	case "rankings":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.usageRankings(w, r, filters, user)
	case "distributions":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.usageDistributions(w, r, filters, user)
	case "overview":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.usageOverview(w, r, filters, user)
	case "records":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		if len(parts) == 2 {
			id, err := parseIntPath(parts[1])
			if err != nil {
				return err
			}
			return a.usageRecordDetail(w, r, id, user)
		}
		return a.usageRecords(w, r, filters, user)
	case "options":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.usageOptions(w, r, filters, user)
	default:
		return notFoundError("Not Found")
	}
}

func parseUsageFilters(r *http.Request) (UsageFilters, error) {
	query := r.URL.Query()
	filters := UsageFilters{Scope: query.Get("scope")}
	if filters.Scope != "" && filters.Scope != "admin" && filters.Scope != "account" {
		return filters, validationError("scope 参数无效")
	}
	if value := strings.TrimSpace(query.Get("start")); value != "" {
		parsed, err := parseQueryTime(value)
		if err != nil {
			return filters, validationError("start 时间格式无效")
		}
		filters.Start = &parsed
	}
	if value := strings.TrimSpace(query.Get("end")); value != "" {
		parsed, err := parseQueryTime(value)
		if err != nil {
			return filters, validationError("end 时间格式无效")
		}
		filters.End = &parsed
	}
	if value := strings.TrimSpace(query.Get("user_id")); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil {
			return filters, validationError("user_id 参数无效")
		}
		filters.UserID = &id
	}
	filters.APIKeyDescription = stringPtrFromQuery(query.Get("api_key_description"))
	filters.Provider = stringPtrFromQuery(query.Get("provider"))
	filters.Model = stringPtrFromQuery(query.Get("model"))
	filters.SourceKey = stringPtrFromQuery(query.Get("source_key"))
	filters.Endpoint = stringPtrFromQuery(query.Get("endpoint"))
	filters.RequestID = stringPtrFromQuery(query.Get("request_id"))
	if value := strings.TrimSpace(query.Get("failed")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return filters, validationError("failed 参数无效")
		}
		filters.Failed = &parsed
	}
	return filters, nil
}

func parseQueryTime(value string) (time.Time, error) {
	if parsed, ok := parseInputTime(value); ok {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func stringPtrFromQuery(value string) *string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func defaultTodayRange() (time.Time, time.Time) {
	now := time.Now().In(appTimeLocation)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, appTimeLocation)
	return start, start.Add(24 * time.Hour)
}

func normalizedUsageFilters(filters UsageFilters) UsageFilters {
	if filters.Start != nil && filters.End != nil {
		return filters
	}
	start, end := defaultTodayRange()
	if filters.Start == nil {
		filters.Start = &start
	}
	if filters.End == nil {
		filters.End = &end
	}
	return filters
}

func accessScope(user *AuthUser, requested string) usageAccessScope {
	accountScoped := requested == "account" || !user.IsAdmin
	return usageAccessScope{
		UserID:   user.ID,
		Username: user.Username,
		IsAdmin:  !accountScoped,
	}
}

func (a *App) scopedFilters(ctx context.Context, filters UsageFilters, scope usageAccessScope) (UsageFilters, error) {
	if !scope.IsAdmin {
		filters.UsageUsername = &scope.Username
		filters.UserID = &scope.UserID
		filters.SourceKey = nil
		return filters, nil
	}
	if filters.UserID != nil && filters.UsageUsername == nil {
		user, err := a.getUser(ctx, *filters.UserID)
		if err != nil {
			return filters, nil
		}
		filters.UsageUsername = &user.Username
	}
	return filters, nil
}

func (a *App) usageSummary(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	scope := accessScope(user, filters.Scope)
	scoped, err := a.scopedFilters(r.Context(), normalizedUsageFilters(filters), scope)
	if err != nil {
		return err
	}
	records, err := a.filteredUsageRecords(r.Context(), scoped, "")
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, usageSummaryFromRecords(scoped, records, prices))
	return nil
}

func (a *App) usageTrends(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	scope := accessScope(user, filters.Scope)
	scoped, err := a.scopedFilters(r.Context(), normalizedUsageFilters(filters), scope)
	if err != nil {
		return err
	}
	records, err := a.filteredUsageRecords(r.Context(), scoped, "timestamp ASC")
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, trendPointsFromRecords(scoped, records, prices))
	return nil
}

func (a *App) usageRankings(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" || groupBy == "api_key" {
		groupBy = "api_key_description"
	}
	if groupBy != "api_key_description" && groupBy != "model" && groupBy != "user" {
		return validationError("group_by 参数无效")
	}
	scope := accessScope(user, filters.Scope)
	if !scope.IsAdmin && groupBy == "user" {
		writeJSON(w, http.StatusOK, map[string]any{"group_by": "user", "items": []any{}})
		return nil
	}
	scoped, err := a.scopedFilters(r.Context(), filters, scope)
	if err != nil {
		return err
	}
	records, err := a.filteredUsageRecords(r.Context(), scoped, "")
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	users, err := a.userLookup(r.Context(), scope)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rankingFromRecords(records, prices, groupBy, users))
	return nil
}

func (a *App) usageDistributions(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	scope := accessScope(user, filters.Scope)
	scoped, err := a.scopedFilters(r.Context(), filters, scope)
	if err != nil {
		return err
	}
	records, err := a.filteredUsageRecords(r.Context(), scoped, "")
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, distributionsFromRecords(records, prices))
	return nil
}

func (a *App) usageOverview(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	scope := accessScope(user, filters.Scope)
	scoped, err := a.scopedFilters(r.Context(), normalizedUsageFilters(filters), scope)
	if err != nil {
		return err
	}
	records, err := a.filteredUsageRecords(r.Context(), scoped, "timestamp ASC")
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	users, err := a.userLookup(r.Context(), scope)
	if err != nil {
		return err
	}
	apiKeyRanking := rankingFromRecords(records, prices, "api_key_description", users)
	userRanking := map[string]any{"group_by": "user", "items": []any{}}
	if scope.IsAdmin {
		userRanking = rankingFromRecords(records, prices, "user", users)
	}
	options, err := a.usageOptionsResponse(r.Context(), user, usageOptionFilters(filters))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary":                     usageSummaryFromRecords(scoped, records, prices),
		"trends":                      trendPointsFromRecords(scoped, records, prices),
		"user_ranking":                userRanking,
		"api_key_description_ranking": apiKeyRanking,
		"api_key_ranking":             apiKeyRanking,
		"model_ranking":               rankingFromRecords(records, prices, "model", users),
		"distributions":               distributionsFromRecords(records, prices),
		"options":                     options,
	})
	return nil
}

func (a *App) usageRecords(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	scope := accessScope(user, filters.Scope)
	scoped, err := a.scopedFilters(r.Context(), normalizedUsageFilters(filters), scope)
	if err != nil {
		return err
	}
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 50)
	if pageSize > 200 {
		pageSize = 200
	}
	total, err := a.countUsageRecords(r.Context(), scoped)
	if err != nil {
		return err
	}
	records, err := a.pagedUsageRecords(r.Context(), scoped, page, pageSize)
	if err != nil {
		return err
	}
	users, err := a.userLookup(r.Context(), scope)
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(records))
	redaction := usageRedactionOptions{MaskAuthIndex: !scope.IsAdmin}
	for _, record := range records {
		items = append(items, listItemFromRecord(record, users, prices, redaction))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"start":     usageAPITimePtr(scoped.Start),
		"end":       usageAPITimePtr(scoped.End),
	})
	return nil
}

func (a *App) usageRecordDetail(w http.ResponseWriter, r *http.Request, recordID int, user *AuthUser) error {
	scope := accessScope(user, r.URL.Query().Get("scope"))
	record, err := a.getUsageRecord(r.Context(), recordID)
	if err != nil {
		return err
	}
	if !scope.IsAdmin && (record.UsageUsername == nil || *record.UsageUsername != scope.Username) {
		return notFoundError("usage 记录不存在")
	}
	users, err := a.userLookup(r.Context(), scope)
	if err != nil {
		return err
	}
	prices, err := a.priceMap(r.Context())
	if err != nil {
		return err
	}
	redaction := usageRedactionOptions{MaskSource: !scope.IsAdmin, MaskAuthIndex: !scope.IsAdmin}
	item := listItemFromRecord(record, users, prices, redaction)
	item["raw_json"] = redactedRawJSON(record.RawJSON, usageRecordAuth(record), redaction)
	writeJSON(w, http.StatusOK, item)
	return nil
}

func (a *App) usageOptions(w http.ResponseWriter, r *http.Request, filters UsageFilters, user *AuthUser) error {
	response, err := a.usageOptionsResponse(r.Context(), user, usageOptionFilters(filters))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, response)
	return nil
}

func usageOptionFilters(filters UsageFilters) UsageFilters {
	return normalizedUsageFilters(UsageFilters{
		Scope: filters.Scope,
		Start: filters.Start,
		End:   filters.End,
	})
}

func (a *App) usageOptionsResponse(ctx context.Context, user *AuthUser, filters UsageFilters) (map[string]any, error) {
	scope := accessScope(user, filters.Scope)
	scoped, err := a.scopedFilters(ctx, filters, scope)
	if err != nil {
		return nil, err
	}
	where, args := usageWhere(scoped)
	users := []map[string]any{}
	distinctStrings := func(column string) ([]string, error) {
		rows, err := a.db.QueryContext(ctx, fmt.Sprintf(`SELECT DISTINCT %s FROM usage_records %s AND %s IS NOT NULL`, column, where, column), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var values []string
		for rows.Next() {
			var value sql.NullString
			if err := rows.Scan(&value); err != nil {
				return nil, err
			}
			if value.Valid && strings.TrimSpace(value.String) != "" {
				values = append(values, value.String)
			}
		}
		sort.Strings(values)
		return values, rows.Err()
	}
	distinctSourceOptions := func() ([]map[string]string, error) {
		rows, err := a.db.QueryContext(ctx, `SELECT DISTINCT source, auth, raw_json FROM usage_records `+where+` AND source IS NOT NULL`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		type sourceOption struct {
			Key   string
			Label string
		}
		values := map[string]sourceOption{}
		for rows.Next() {
			var source, auth, rawJSON sql.NullString
			if err := rows.Scan(&source, &auth, &rawJSON); err != nil {
				return nil, err
			}
			sourceValue := strings.TrimSpace(source.String)
			if !source.Valid || sourceValue == "" {
				continue
			}
			record := UsageRecord{Source: &sourceValue, RawJSON: rawJSON.String}
			if auth.Valid {
				record.Auth = &auth.String
			}
			displaySource := redactedUsageSource(record.Source, usageRecordAuth(record), usageRedactionOptions{})
			sourceKey := usageSourceKey(record.Source)
			if displaySource == nil || sourceKey == nil {
				continue
			}
			existing, ok := values[*sourceKey]
			if !ok || (existing.Label == sourceValue && *displaySource != sourceValue) {
				values[*sourceKey] = sourceOption{Key: *sourceKey, Label: *displaySource}
			}
		}
		options := make([]sourceOption, 0, len(values))
		for _, option := range values {
			options = append(options, option)
		}
		sort.Slice(options, func(i, j int) bool {
			if options[i].Label == options[j].Label {
				return options[i].Key < options[j].Key
			}
			return options[i].Label < options[j].Label
		})
		sources := make([]map[string]string, 0, len(options))
		for _, option := range options {
			sources = append(sources, map[string]string{
				"key":   option.Key,
				"label": option.Label,
			})
		}
		return sources, rows.Err()
	}
	if scope.IsAdmin {
		usernames, err := distinctStrings("usage_username")
		if err != nil {
			return nil, err
		}
		userLookup, err := a.userLookup(ctx, scope)
		if err != nil {
			return nil, err
		}
		for _, username := range usernames {
			if info, ok := userLookup[username]; ok {
				id := info.ID
				users = append(users, rankingItem(strconv.Itoa(id), info.Name, 0, 0, 0, 0, &id, nil))
			}
		}
	}
	providers, err := distinctStrings("provider")
	if err != nil {
		return nil, err
	}
	models, err := distinctStrings("model")
	if err != nil {
		return nil, err
	}
	sources := []map[string]string{}
	if scope.IsAdmin {
		sources, err = distinctSourceOptions()
		if err != nil {
			return nil, err
		}
	}
	endpoints, err := distinctStrings("endpoint")
	if err != nil {
		return nil, err
	}
	descriptions, err := distinctStrings("api_key_description")
	if err != nil {
		return nil, err
	}
	descriptionItems := make([]map[string]any, 0, len(descriptions))
	for _, description := range descriptions {
		value := description
		descriptionItems = append(descriptionItems, rankingItem(value, value, 0, 0, 0, 0, nil, &value))
	}
	return map[string]any{
		"users":                users,
		"api_key_descriptions": descriptionItems,
		"providers":            providers,
		"models":               models,
		"sources":              sources,
		"endpoints":            endpoints,
	}, nil
}

func parsePositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func (a *App) filteredUsageRecords(ctx context.Context, filters UsageFilters, orderBy string) ([]UsageRecord, error) {
	where, args := usageWhere(filters)
	query := `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json FROM usage_records ` + where
	if strings.TrimSpace(orderBy) != "" {
		query += " ORDER BY " + orderBy
	} else {
		query += " ORDER BY timestamp"
	}
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records, err := scanUsageRecords(rows)
	if err != nil {
		return nil, err
	}
	return applyUsagePostFilters(records, filters), nil
}

func (a *App) countUsageRecords(ctx context.Context, filters UsageFilters) (int, error) {
	if hasUsagePostFilters(filters) {
		records, err := a.filteredUsageRecords(ctx, filters, "")
		if err != nil {
			return 0, err
		}
		return len(records), nil
	}
	where, args := usageWhere(filters)
	var total int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_records `+where, args...).Scan(&total)
	return total, err
}

func (a *App) pagedUsageRecords(ctx context.Context, filters UsageFilters, page, pageSize int) ([]UsageRecord, error) {
	if hasUsagePostFilters(filters) {
		records, err := a.filteredUsageRecords(ctx, filters, "timestamp DESC")
		if err != nil {
			return nil, err
		}
		start := (page - 1) * pageSize
		if start >= len(records) {
			return []UsageRecord{}, nil
		}
		end := start + pageSize
		if end > len(records) {
			end = len(records)
		}
		return records[start:end], nil
	}
	where, args := usageWhere(filters)
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json FROM usage_records `+where+` ORDER BY timestamp DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsageRecords(rows)
}

func hasUsagePostFilters(filters UsageFilters) bool {
	return filters.SourceKey != nil
}

func applyUsagePostFilters(records []UsageRecord, filters UsageFilters) []UsageRecord {
	if !hasUsagePostFilters(filters) {
		return records
	}
	filtered := records[:0]
	for _, record := range records {
		if filters.SourceKey != nil {
			key := usageSourceKey(record.Source)
			if key == nil || *key != *filters.SourceKey {
				continue
			}
		}
		filtered = append(filtered, record)
	}
	return filtered
}

func (a *App) getUsageRecord(ctx context.Context, id int) (UsageRecord, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json FROM usage_records WHERE id = ?`, id)
	if err != nil {
		return UsageRecord{}, err
	}
	defer rows.Close()
	records, err := scanUsageRecords(rows)
	if err != nil {
		return UsageRecord{}, err
	}
	if len(records) == 0 {
		return UsageRecord{}, notFoundError("usage 记录不存在")
	}
	return records[0], nil
}

func usageWhere(filters UsageFilters) (string, []any) {
	clauses := []string{"1 = 1"}
	args := []any{}
	if filters.Start != nil {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, dbTime(*filters.Start))
	}
	if filters.End != nil {
		clauses = append(clauses, "timestamp < ?")
		args = append(args, dbTime(*filters.End))
	}
	if filters.UsageUsername != nil {
		clauses = append(clauses, "usage_username = ?")
		args = append(args, *filters.UsageUsername)
	}
	if filters.APIKeyDescription != nil {
		clauses = append(clauses, "api_key_description = ?")
		args = append(args, *filters.APIKeyDescription)
	}
	if filters.Provider != nil {
		clauses = append(clauses, "provider = ?")
		args = append(args, *filters.Provider)
	}
	if filters.Model != nil {
		clauses = append(clauses, "model = ?")
		args = append(args, *filters.Model)
	}
	if filters.Endpoint != nil {
		clauses = append(clauses, "endpoint = ?")
		args = append(args, *filters.Endpoint)
	}
	if filters.Failed != nil {
		clauses = append(clauses, "failed = ?")
		args = append(args, *filters.Failed)
	}
	if filters.RequestID != nil {
		clauses = append(clauses, "request_id LIKE ?")
		args = append(args, "%"+*filters.RequestID+"%")
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func scanUsageRecords(rows *sql.Rows) ([]UsageRecord, error) {
	var records []UsageRecord
	for rows.Next() {
		var record UsageRecord
		var timestamp, usageUsername, description, provider, model, reasoningEffort, endpoint, source, sourceAccount, requestID, auth, authIndex, latency sql.NullString
		var latencyFloat, ttftFloat sql.NullFloat64
		if err := rows.Scan(&record.ID, &timestamp, &usageUsername, &description, &provider, &model, &reasoningEffort, &endpoint, &source, &sourceAccount, &requestID, &auth, &authIndex, &latencyFloat, &ttftFloat, &record.Failed, &record.InputTokens, &record.OutputTokens, &record.CachedTokens, &record.CacheReadTokens, &record.CacheCreationTokens, &record.ReasoningTokens, &record.TotalTokens, &record.DedupeKey, &record.RawJSON); err != nil {
			return nil, err
		}
		_ = latency
		if parsed, ok := parseDBTime(timestamp.String); ok {
			record.Timestamp = parsed
		}
		record.UsageUsername = nullableString(usageUsername)
		record.APIKeyDescription = nullableString(description)
		record.Provider = nullableString(provider)
		record.Model = nullableString(model)
		record.ReasoningEffort = nullableString(reasoningEffort)
		record.Endpoint = nullableString(endpoint)
		record.Source = nullableString(source)
		record.SourceAccount = nullableString(sourceAccount)
		record.RequestID = nullableString(requestID)
		record.Auth = nullableString(auth)
		record.AuthIndex = nullableString(authIndex)
		record.LatencyMS = nullableFloat(latencyFloat)
		record.TTFTMS = nullableFloat(ttftFloat)
		records = append(records, record)
	}
	return records, rows.Err()
}

type userInfo struct {
	ID       int
	Username string
	Name     string
}

func (a *App) userLookup(ctx context.Context, scope usageAccessScope) (map[string]userInfo, error) {
	if !scope.IsAdmin {
		return map[string]userInfo{
			scope.Username: {ID: scope.UserID, Username: scope.Username, Name: scope.Username},
		}, nil
	}
	users, err := a.allUsers(ctx)
	if err != nil {
		return nil, err
	}
	lookup := map[string]userInfo{}
	for _, user := range users {
		name := displayUserName(user)
		if user.DisabledAt != nil {
			name += " (已禁用)"
		}
		lookup[user.Username] = userInfo{ID: user.ID, Username: user.Username, Name: name}
	}
	return lookup, nil
}

func listItemFromRecord(record UsageRecord, users map[string]userInfo, prices map[[2]string]ModelPrice, redaction usageRedactionOptions) map[string]any {
	amount, unpriced := recordCost(record, prices)
	userID := (*int)(nil)
	userLabel := "未绑定"
	if record.UsageUsername != nil {
		if info, ok := users[*record.UsageUsername]; ok {
			id := info.ID
			userID = &id
			userLabel = info.Name
		} else {
			userLabel = *record.UsageUsername
		}
	}
	authIndex := record.AuthIndex
	if authIndex == nil {
		authIndex = rawJSONStringField(record.RawJSON, "auth_index")
	}
	authIndex = redactedAuthIndex(authIndex, redaction)
	auth := usageRecordAuth(record)
	return map[string]any{
		"id":                    record.ID,
		"timestamp":             usageAPITime(record.Timestamp),
		"api_key_description":   record.APIKeyDescription,
		"user_id":               userID,
		"user_label":            userLabel,
		"provider":              record.Provider,
		"model":                 record.Model,
		"reasoning_effort":      record.ReasoningEffort,
		"endpoint":              record.Endpoint,
		"source":                redactedUsageSource(record.Source, auth, redaction),
		"request_id":            record.RequestID,
		"auth_index":            authIndex,
		"auth":                  auth,
		"latency_ms":            record.LatencyMS,
		"ttft_ms":               record.TTFTMS,
		"failed":                record.Failed,
		"input_tokens":          record.InputTokens,
		"output_tokens":         record.OutputTokens,
		"cached_tokens":         record.CachedTokens,
		"cache_read_tokens":     record.CacheReadTokens,
		"cache_creation_tokens": record.CacheCreationTokens,
		"reasoning_tokens":      record.ReasoningTokens,
		"total_tokens":          record.TotalTokens,
		"estimated_cost_usd":    amount,
		"unpriced":              unpriced,
	}
}

func usageSummaryFromRecords(filters UsageFilters, records []UsageRecord, prices map[[2]string]ModelPrice) map[string]any {
	failed := 0
	input, output, cached, reasoning, total := 0, 0, 0, 0, 0
	cacheHit := 0
	estimated := 0.0
	unpriced := 0
	ttftTotal := 0.0
	ttftCount := 0
	for _, record := range records {
		if record.Failed {
			failed++
		}
		input += usageAggregateInputTokens(record)
		output += record.OutputTokens
		cached += record.CachedTokens
		cacheHit += usageCacheHitTokens(record)
		reasoning += record.ReasoningTokens
		total += usageAggregateTotalTokens(record)
		amount, isUnpriced := recordCost(record, prices)
		estimated = mathRound(estimated+amount, 8)
		if isUnpriced {
			unpriced++
		}
		if record.TTFTMS != nil && *record.TTFTMS > 0 {
			ttftTotal += *record.TTFTMS
			ttftCount++
		}
	}
	if filters.Start == nil || filters.End == nil {
		start, end := defaultTodayRange()
		filters.Start = &start
		filters.End = &end
	}
	return map[string]any{
		"start":              usageAPITimePtr(filters.Start),
		"end":                usageAPITimePtr(filters.End),
		"total_records":      len(records),
		"failed_records":     failed,
		"success_records":    len(records) - failed,
		"input_tokens":       input,
		"output_tokens":      output,
		"cached_tokens":      cached,
		"cache_hit_tokens":   cacheHit,
		"reasoning_tokens":   reasoning,
		"total_tokens":       total,
		"estimated_cost_usd": estimated,
		"unpriced_records":   unpriced,
		"average_ttft_ms":    averageTTFTMS(ttftTotal, ttftCount),
	}
}

func averageTTFTMS(total float64, count int) *float64 {
	if count == 0 {
		return nil
	}
	average := mathRound(total/float64(count), 2)
	return &average
}

func trendPointsFromRecords(filters UsageFilters, records []UsageRecord, prices map[[2]string]ModelPrice) []map[string]any {
	buckets := map[string][]UsageRecord{}
	duration := 24 * time.Hour
	if filters.Start != nil && filters.End != nil {
		duration = filters.End.Sub(*filters.Start)
	}
	for _, record := range records {
		timestamp := record.Timestamp.In(appTimeLocation)
		bucket := timestamp.Format("2006-01-02")
		if duration <= 48*time.Hour {
			bucket = timestamp.Format("2006-01-02 15:00")
		}
		buckets[bucket] = append(buckets[bucket], record)
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	points := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		group := buckets[key]
		failed, tokens, cost := 0, 0, 0.0
		for _, record := range group {
			if record.Failed {
				failed++
			}
			tokens += usageAggregateTotalTokens(record)
			amount, _ := recordCost(record, prices)
			cost = mathRound(cost+amount, 8)
		}
		points = append(points, map[string]any{
			"bucket":             key,
			"records":            len(group),
			"failed_records":     failed,
			"total_tokens":       tokens,
			"estimated_cost_usd": cost,
		})
	}
	return points
}

func rankingFromRecords(records []UsageRecord, prices map[[2]string]ModelPrice, groupBy string, users map[string]userInfo) map[string]any {
	grouped := map[string][]UsageRecord{}
	labels := map[string]string{}
	userIDs := map[string]*int{}
	descriptions := map[string]*string{}
	for _, record := range records {
		key := ""
		label := ""
		switch groupBy {
		case "model":
			provider := valueOr(record.Provider, "unknown")
			model := valueOr(record.Model, "unknown")
			key = provider + "::" + model
			label = provider + " / " + model
		case "user":
			if record.UsageUsername == nil {
				continue
			}
			info, ok := users[*record.UsageUsername]
			if !ok {
				key = *record.UsageUsername
				label = *record.UsageUsername
			} else {
				key = strconv.Itoa(info.ID)
				label = info.Name
				id := info.ID
				userIDs[key] = &id
			}
		default:
			description := ""
			if record.APIKeyDescription != nil {
				description = strings.TrimSpace(*record.APIKeyDescription)
			}
			key = description
			if key == "" {
				key = "unlabeled"
				label = "未设置 KEY 描述"
			} else {
				label = description
				value := description
				descriptions[key] = &value
			}
		}
		grouped[key] = append(grouped[key], record)
		labels[key] = label
	}
	items := make([]map[string]any, 0, len(grouped))
	for key, group := range grouped {
		failed, tokens, cost := 0, 0, 0.0
		for _, record := range group {
			if record.Failed {
				failed++
			}
			tokens += usageAggregateTotalTokens(record)
			amount, _ := recordCost(record, prices)
			cost = mathRound(cost+amount, 8)
		}
		items = append(items, rankingItem(key, labels[key], len(group), failed, tokens, cost, userIDs[key], descriptions[key]))
	}
	sort.Slice(items, func(i, j int) bool {
		leftTokens := items[i]["total_tokens"].(int)
		rightTokens := items[j]["total_tokens"].(int)
		if leftTokens == rightTokens {
			return items[i]["records"].(int) > items[j]["records"].(int)
		}
		return leftTokens > rightTokens
	})
	if len(items) > 20 {
		items = items[:20]
	}
	return map[string]any{"group_by": groupBy, "items": items}
}

func rankingItem(key, label string, records, failed, tokens int, cost float64, userID *int, description *string) map[string]any {
	return map[string]any{
		"key":                 key,
		"label":               label,
		"records":             records,
		"failed_records":      failed,
		"total_tokens":        tokens,
		"estimated_cost_usd":  cost,
		"user_id":             userID,
		"api_key_description": description,
	}
}

func distributionsFromRecords(records []UsageRecord, prices map[[2]string]ModelPrice) map[string]any {
	return map[string]any{
		"providers": distributionItems(records, prices, func(record UsageRecord) string { return valueOr(record.Provider, "unknown") }),
		"models":    distributionItems(records, prices, func(record UsageRecord) string { return valueOr(record.Model, "unknown") }),
		"endpoints": distributionItems(records, prices, func(record UsageRecord) string { return valueOr(record.Endpoint, "unknown") }),
	}
}

func distributionItems(records []UsageRecord, prices map[[2]string]ModelPrice, keyFn func(UsageRecord) string) []map[string]any {
	grouped := map[string][]UsageRecord{}
	for _, record := range records {
		key := keyFn(record)
		grouped[key] = append(grouped[key], record)
	}
	items := make([]map[string]any, 0, len(grouped))
	for key, group := range grouped {
		tokens, cost := 0, 0.0
		for _, record := range group {
			tokens += usageAggregateTotalTokens(record)
			amount, _ := recordCost(record, prices)
			cost = mathRound(cost+amount, 8)
		}
		items = append(items, map[string]any{
			"key":                key,
			"label":              key,
			"records":            len(group),
			"total_tokens":       tokens,
			"estimated_cost_usd": cost,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["records"].(int) > items[j]["records"].(int) })
	if len(items) > 20 {
		items = items[:20]
	}
	return items
}

func valueOr(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func rawJSONStringField(rawJSON, fieldName string) *string {
	var payload map[string]any
	if json.Unmarshal([]byte(rawJSON), &payload) != nil {
		return nil
	}
	value, ok := payload[fieldName]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		normalized := strings.TrimSpace(typed)
		if normalized == "" {
			return nil
		}
		return &normalized
	case float64:
		text := strconv.FormatFloat(typed, 'f', -1, 64)
		return &text
	case bool:
		text := strconv.FormatBool(typed)
		return &text
	default:
		return nil
	}
}

func sourceAccountFromUsageSource(source *string) *string {
	if source == nil {
		return nil
	}
	if match := usageEmailPattern.FindString(*source); match != "" {
		normalized := strings.ToLower(strings.TrimSpace(match))
		return &normalized
	}
	return nil
}

func authIndexFromUsagePayload(parsed any) *string {
	return toString(findFirst(parsed, "auth_index", "authIndex", "index", "auth_name", "authName", "account_id", "accountId"))
}

func usageRecordAuth(record UsageRecord) *string {
	auth := rawJSONStringField(record.RawJSON, "auth_type")
	if auth == nil {
		auth = record.Auth
	}
	return auth
}

func redactedUsageSource(source *string, authType *string, redaction usageRedactionOptions) *string {
	if source == nil {
		return nil
	}
	if !redaction.MaskSource && !isAPIKeyAuth(authType) {
		return source
	}
	masked := maskSecret(source)
	return &masked
}

func usageSourceKey(source *string) *string {
	if source == nil {
		return nil
	}
	normalized := strings.TrimSpace(*source)
	if normalized == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(normalized))
	key := hex.EncodeToString(sum[:])
	return &key
}

func redactedAuthIndex(authIndex *string, redaction usageRedactionOptions) *string {
	if authIndex == nil || !redaction.MaskAuthIndex {
		return authIndex
	}
	masked := maskSecret(authIndex)
	return &masked
}

func isAPIKeyAuth(authType *string) bool {
	if authType == nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(*authType))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	return normalized == "apikey"
}

func redactedRawJSON(rawJSON string, authType *string, redaction usageRedactionOptions) any {
	var payload any
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		value := rawJSON
		return maskSecret(&value)
	}
	return redactJSON(payload, authType, redaction)
}

func redactJSON(value any, inheritedAuthType *string, redaction usageRedactionOptions) any {
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		authType := jsonStringField(typed, "auth_type")
		if authType == nil {
			authType = jsonStringField(typed, "authType")
		}
		if authType == nil {
			authType = jsonStringField(typed, "auth")
		}
		if authType == nil {
			authType = inheritedAuthType
		}
		for key, child := range typed {
			if shouldRedactJSONField(key, authType, redaction) {
				if child == nil {
					result[key] = nil
				} else {
					text := fmt.Sprint(child)
					result[key] = maskSecret(&text)
				}
			} else {
				result[key] = redactJSON(child, authType, redaction)
			}
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			result = append(result, redactJSON(child, inheritedAuthType, redaction))
		}
		return result
	default:
		return value
	}
}

func jsonStringField(payload map[string]any, fieldName string) *string {
	value, ok := payload[fieldName]
	if !ok || value == nil {
		return nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return nil
	}
	return &text
}

func shouldRedactJSONField(key string, authType *string, redaction usageRedactionOptions) bool {
	lower := strings.ToLower(key)
	if (redaction.MaskSource || isAPIKeyAuth(authType)) && lower == "source" {
		return true
	}
	if redaction.MaskAuthIndex && (lower == "auth_index" || lower == "authindex") {
		return true
	}
	return strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "bearer") ||
		strings.Contains(lower, "cookie") ||
		strings.Contains(lower, "key") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token")
}

func (a *App) saveUsageMessage(ctx context.Context, raw []byte) (UsageRecord, bool, error) {
	normalized, err := normalizeUsage(raw)
	if err != nil {
		return UsageRecord{}, false, err
	}
	usageUsername, description, err := a.usageOwnerSnapshot(ctx, normalized.APIKeyHash)
	if err != nil {
		return UsageRecord{}, false, err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO usage_records (
			created_at, timestamp, usage_username, api_key_description, provider, model, endpoint,
			reasoning_effort, source, source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens,
			cached_tokens, cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, now, dbTime(normalized.Timestamp), usageUsername, description, normalized.Provider, normalized.Model, normalized.Endpoint, normalized.ReasoningEffort, normalized.Source, normalized.SourceAccount, normalized.RequestID, normalized.Auth, normalized.AuthIndex, normalized.LatencyMS, normalized.TTFTMS, normalized.Failed, normalized.InputTokens, normalized.OutputTokens, normalized.CachedTokens, normalized.CacheReadTokens, normalized.CacheCreationTokens, normalized.ReasoningTokens, normalized.TotalTokens, normalized.DedupeKey, normalized.RawJSON)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			record, getErr := a.usageRecordByDedupe(ctx, normalized.DedupeKey)
			return record, false, getErr
		}
		return UsageRecord{}, false, err
	}
	id, _ := result.LastInsertId()
	record, err := a.getUsageRecord(ctx, int(id))
	if err != nil {
		return UsageRecord{}, false, err
	}
	if err := a.applyQuotaCharge(ctx, record); err != nil {
		return UsageRecord{}, false, err
	}
	return record, true, nil
}

func (a *App) usageRecordByDedupe(ctx context.Context, dedupeKey string) (UsageRecord, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id, CAST(timestamp AS TEXT), usage_username, api_key_description, provider, model, reasoning_effort, endpoint, source,
		source_account, request_id, auth, auth_index, latency_ms, ttft_ms, failed, input_tokens, output_tokens, cached_tokens,
		cache_read_tokens, cache_creation_tokens, reasoning_tokens, total_tokens, dedupe_key, raw_json FROM usage_records WHERE dedupe_key = ?`, dedupeKey)
	if err != nil {
		return UsageRecord{}, err
	}
	defer rows.Close()
	records, err := scanUsageRecords(rows)
	if err != nil {
		return UsageRecord{}, err
	}
	if len(records) == 0 {
		return UsageRecord{}, notFoundError("usage 记录不存在")
	}
	return records[0], nil
}

type normalizedUsage struct {
	Timestamp           time.Time
	APIKeyHash          string
	Provider            *string
	Model               *string
	Endpoint            *string
	Source              *string
	SourceAccount       *string
	RequestID           *string
	Auth                *string
	AuthIndex           *string
	LatencyMS           *float64
	ReasoningEffort     *string
	TTFTMS              *float64
	Failed              bool
	InputTokens         int
	OutputTokens        int
	CachedTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	ReasoningTokens     int
	TotalTokens         int
	DedupeKey           string
	RawJSON             string
}

func normalizeUsage(raw []byte) (normalizedUsage, error) {
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		parsed = map[string]any{"message": string(raw)}
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return normalizedUsage{}, err
	}
	apiKey := toString(findFirst(parsed, "api_key", "apiKey", "apikey", "key"))
	if apiKey == nil {
		unknown := "unknown"
		apiKey = &unknown
	}
	input := usageToken(parsed, "input_tokens", "prompt_tokens", "promptTokens", "input")
	output := usageToken(parsed, "output_tokens", "completion_tokens", "completionTokens", "output")
	cached := usageToken(parsed, "cached_tokens", "cached_input_tokens", "cached")
	cacheRead := usageToken(parsed, "cache_read_tokens", "cache_read_input_tokens")
	cacheCreation := usageToken(parsed, "cache_creation_tokens", "cache_creation_input_tokens")
	reasoning := usageToken(parsed, "reasoning_tokens", "reasoning")
	total := usageToken(parsed, "total_tokens", "totalTokens", "total")
	if total == 0 {
		total = input + output
		if total == 0 {
			total = cached + reasoning
		}
	}
	source := toString(findFirst(parsed, "source", "origin"))
	sum := sha256.Sum256(canonical)
	return normalizedUsage{
		Timestamp:           parseUsageTimestamp(findFirst(parsed, "timestamp", "time", "created_at", "createdAt", "request_time")),
		APIKeyHash:          hashAPIKey(*apiKey),
		Provider:            toString(findFirst(parsed, "provider", "provider_name")),
		Model:               toString(findFirst(parsed, "model", "model_name")),
		Endpoint:            toString(findFirst(parsed, "endpoint", "path", "route")),
		Source:              source,
		SourceAccount:       sourceAccountFromUsageSource(source),
		RequestID:           toString(findFirst(parsed, "request_id", "requestId", "id")),
		Auth:                authLabel(parsed),
		AuthIndex:           authIndexFromUsagePayload(parsed),
		LatencyMS:           toFloat(findFirst(parsed, "latency_ms", "latency", "duration_ms", "duration")),
		ReasoningEffort:     toString(findFirst(parsed, "reasoning_effort", "reasoningEffort")),
		TTFTMS:              toPositiveFloat(findFirst(parsed, "ttft_ms", "ttftMs")),
		Failed:              isUsageFailed(parsed),
		InputTokens:         input,
		OutputTokens:        output,
		CachedTokens:        cached,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreation,
		ReasoningTokens:     reasoning,
		TotalTokens:         total,
		DedupeKey:           "raw:" + hex.EncodeToString(sum[:]),
		RawJSON:             string(canonical),
	}, nil
}

func usageToken(value any, keys ...string) int {
	if root, ok := value.(map[string]any); ok {
		if tokens, ok := root["tokens"].(map[string]any); ok {
			for _, key := range keys {
				if token, exists := tokens[key]; exists {
					switch token.(type) {
					case float64, string, bool:
						return toInt(token)
					}
				}
			}
		}
	}
	return toInt(findFirst(value, keys...))
}

func (a *App) usageOwnerSnapshot(ctx context.Context, apiKeyHash string) (*string, *string, error) {
	var userID int
	var description sql.NullString
	err := a.db.QueryRowContext(ctx, `SELECT user_id, description FROM user_api_keys WHERE api_key_hash = ?`, apiKeyHash).Scan(&userID, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var username string
	err = a.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ?`, userID).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nullableString(description), nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &username, nullableString(description), nil
}

func findFirst(value any, keys ...string) any {
	keySet := map[string]bool{}
	for _, key := range keys {
		keySet[strings.ToLower(key)] = true
	}
	var walk func(any) any
	walk = func(current any) any {
		switch typed := current.(type) {
		case map[string]any:
			for _, key := range keys {
				if value, ok := typed[key]; ok {
					return value
				}
			}
			for key, child := range typed {
				if keySet[strings.ToLower(key)] {
					return child
				}
				if found := walk(child); found != nil {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != nil {
					return found
				}
			}
		}
		return nil
	}
	return walk(value)
}

func toString(value any) *string {
	switch typed := value.(type) {
	case string:
		normalized := strings.TrimSpace(typed)
		if normalized == "" {
			return nil
		}
		return &normalized
	case float64:
		text := strconv.FormatFloat(typed, 'f', -1, 64)
		return &text
	case bool:
		text := strconv.FormatBool(typed)
		return &text
	default:
		return nil
	}
}

func toFloat(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func toPositiveFloat(value any) *float64 {
	parsed := toFloat(value)
	if parsed == nil || *parsed <= 0 {
		return nil
	}
	return parsed
}

func toInt(value any) int {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return int(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil && parsed > 0 {
			return int(parsed)
		}
	case bool:
		if typed {
			return 1
		}
	}
	return 0
}

func parseUsageTimestamp(value any) time.Time {
	switch typed := value.(type) {
	case float64:
		seconds := typed
		if typed > 10_000_000_000 {
			seconds = typed / 1000
		}
		return time.Unix(int64(seconds), 0).In(appTimeLocation)
	case string:
		if parsed, ok := parseInputTime(typed); ok {
			return parsed
		}
	}
	return time.Now().In(appTimeLocation)
}

func authLabel(value any) *string {
	if authType := toString(findFirst(value, "auth_type")); authType != nil {
		return authType
	}
	return toString(findFirst(value, "auth", "authentication"))
}

func isUsageFailed(value any) bool {
	failed := findFirst(value, "failed", "is_failed", "error")
	switch typed := failed.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "failed" || normalized == "error"
	default:
		if failed != nil {
			return true
		}
	}
	success := findFirst(value, "success", "ok")
	if typed, ok := success.(bool); ok {
		return !typed
	}
	status := toInt(findFirst(value, "status", "status_code", "statusCode"))
	return status >= 400
}
