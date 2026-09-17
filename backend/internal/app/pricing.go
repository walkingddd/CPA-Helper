package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultLiteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
const modelBillingUnitToken = "token"
const modelBillingUnitRequest = "request"

type ModelPrice struct {
	ID                         int        `json:"id"`
	Provider                   string     `json:"provider"`
	Model                      string     `json:"model"`
	InputUSDPerMillion         float64    `json:"input_usd_per_million"`
	OutputUSDPerMillion        float64    `json:"output_usd_per_million"`
	CacheReadUSDPerMillion     float64    `json:"cache_read_usd_per_million"`
	CacheCreationUSDPerMillion float64    `json:"cache_creation_usd_per_million"`
	RequestUSD                 *float64   `json:"request_usd"`
	BillingUnit                string     `json:"billing_unit"`
	Source                     string     `json:"source"`
	SourceModel                *string    `json:"source_model"`
	AutoSynced                 bool       `json:"auto_synced"`
	LastSyncedAt               *time.Time `json:"last_synced_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

type modelPricePayload struct {
	Provider                   string   `json:"provider"`
	Model                      string   `json:"model"`
	InputUSDPerMillion         float64  `json:"input_usd_per_million"`
	OutputUSDPerMillion        float64  `json:"output_usd_per_million"`
	CacheReadUSDPerMillion     float64  `json:"cache_read_usd_per_million"`
	CacheCreationUSDPerMillion float64  `json:"cache_creation_usd_per_million"`
	RequestUSD                 *float64 `json:"request_usd"`
}

type modelPriceSyncRequest struct {
	SourceURL *string `json:"source_url"`
}

type liteLLMProxySettingsPayload struct {
	Enabled  *bool   `json:"enabled"`
	ProxyURL *string `json:"proxy_url"`
}

type ModelPriceCatalogItem struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Object            *string                `json:"object"`
	Owner             *string                `json:"owner"`
	Created           *int                   `json:"created"`
	Metadata          map[string]any         `json:"metadata"`
	SuggestedProvider string                 `json:"suggested_provider"`
	Price             *ModelPrice            `json:"price"`
	Sources           []AvailableModelSource `json:"sources"`
}

type ModelPriceCatalogResponse struct {
	HasAPIKeys           bool                     `json:"has_api_keys"`
	APIKeyCount          int                      `json:"api_key_count"`
	QueryableAPIKeyCount int                      `json:"queryable_api_key_count"`
	Models               []ModelPriceCatalogItem  `json:"models"`
	Errors               []AvailableModelKeyError `json:"errors"`
	PricedModels         int                      `json:"priced_models"`
	UnpricedModels       int                      `json:"unpriced_models"`
}

type modelCatalogAPIKey struct {
	UserAPIKey
	UserLabel string
}

func (a *App) handleModelPrices(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	switch r.Method {
	case http.MethodGet:
		prices, err := a.listPrices(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, prices)
		return nil
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/sync/litellm") {
			return a.handleSyncLiteLLMPrices(w, r)
		}
		var payload modelPricePayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		price, err := a.createPrice(r.Context(), payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, price)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleModelPriceByPath(w http.ResponseWriter, r *http.Request) error {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/model-prices/"), "/")
	if path == "sync/litellm" {
		if _, err := a.adminUser(r.Context(), r); err != nil {
			return err
		}
		if err := requireMethod(r, http.MethodPost); err != nil {
			return err
		}
		return a.handleSyncLiteLLMPrices(w, r)
	}
	if path == "litellm-proxy" {
		if _, err := a.adminUser(r.Context(), r); err != nil {
			return err
		}
		return a.handleLiteLLMProxySettings(w, r)
	}
	if path == "catalog" {
		if _, err := a.adminUser(r.Context(), r); err != nil {
			return err
		}
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		response, err := a.modelPriceCatalog(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, response)
		return nil
	}
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	idText := path
	id, err := parseIntPath(idText)
	if err != nil {
		return err
	}
	switch r.Method {
	case http.MethodPut:
		var payload modelPricePayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		price, err := a.updatePrice(r.Context(), id, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, price)
		return nil
	case http.MethodDelete:
		if err := a.deletePrice(r.Context(), id); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleLiteLLMProxySettings(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, liteLLMProxySettingsResponse(cfg.LiteLLMProxy))
		return nil
	case http.MethodPut:
		var payload liteLLMProxySettingsPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		cfg, err := a.loadConfig(r.Context())
		if err != nil {
			return err
		}
		next := cfg.LiteLLMProxy
		if payload.Enabled != nil {
			next.Enabled = *payload.Enabled
		}
		if payload.ProxyURL != nil {
			next.ProxyURL = strings.TrimSpace(*payload.ProxyURL)
		}
		normalized, err := normalizeLiteLLMProxyConfig(next)
		if err != nil {
			return err
		}
		cfg.LiteLLMProxy = normalized
		if err := a.saveConfig(r.Context(), cfg); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, liteLLMProxySettingsResponse(cfg.LiteLLMProxy))
		return nil
	default:
		return methodNotAllowed()
	}
}

func liteLLMProxySettingsResponse(cfg LiteLLMProxyConfig) map[string]any {
	return map[string]any{
		"enabled":   cfg.Enabled,
		"proxy_url": cfg.ProxyURL,
	}
}

func validatePricePayload(payload modelPricePayload) (modelPricePayload, error) {
	payload.Provider = strings.TrimSpace(payload.Provider)
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Provider == "" || payload.Model == "" {
		return payload, validationError("provider/model 不能为空")
	}
	if !finiteNonNegative(payload.InputUSDPerMillion) ||
		!finiteNonNegative(payload.OutputUSDPerMillion) ||
		!finiteNonNegative(payload.CacheReadUSDPerMillion) ||
		!finiteNonNegative(payload.CacheCreationUSDPerMillion) ||
		(payload.RequestUSD != nil && !finiteNonNegative(*payload.RequestUSD)) {
		return payload, validationError("价格不能为负数")
	}
	return payload, nil
}

func (a *App) listPrices(ctx context.Context) ([]ModelPrice, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, provider, model, input_usd_per_million, output_usd_per_million,
		       cache_read_usd_per_million, cache_creation_usd_per_million, request_usd,
		       source, source_model, auto_synced, CAST(last_synced_at AS TEXT), CAST(updated_at AS TEXT)
		FROM model_prices
		ORDER BY auto_synced ASC, lower(provider), lower(model)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPrices(rows)
}

func (a *App) priceMap(ctx context.Context) (map[[2]string]ModelPrice, error) {
	prices, err := a.listPrices(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[[2]string]ModelPrice, len(prices))
	for _, price := range prices {
		result[priceKey(price.Provider, price.Model)] = price
	}
	return result, nil
}

func (a *App) modelPriceCatalog(ctx context.Context) (ModelPriceCatalogResponse, error) {
	bindings, err := a.modelCatalogAPIKeys(ctx)
	if err != nil {
		return ModelPriceCatalogResponse{}, err
	}
	response := ModelPriceCatalogResponse{
		HasAPIKeys:  len(bindings) > 0,
		APIKeyCount: len(bindings),
		Models:      []ModelPriceCatalogItem{},
		Errors:      []AvailableModelKeyError{},
	}
	queryable := make([]modelCatalogAPIKey, 0, len(bindings))
	for _, binding := range bindings {
		if binding.APIKey != nil && strings.TrimSpace(*binding.APIKey) != "" {
			queryable = append(queryable, binding)
		}
	}
	response.QueryableAPIKeyCount = len(queryable)
	if len(queryable) == 0 {
		return response, nil
	}
	cfg, err := a.loadConfig(ctx)
	if err != nil {
		return ModelPriceCatalogResponse{}, err
	}
	prices, err := a.listPrices(ctx)
	if err != nil {
		return ModelPriceCatalogResponse{}, err
	}
	priceLookup := pricesByKey(prices)
	modelsByID := map[string]AvailableModelItem{}
	for _, binding := range queryable {
		source := catalogAvailableModelSource(binding)
		models, err := fetchAvailableModelItems(ctx, cfg, *binding.APIKey)
		if err != nil {
			response.Errors = append(response.Errors, AvailableModelKeyError{
				APIKeyHash:    source.APIKeyHash,
				APIKeyPreview: source.APIKeyPreview,
				Description:   source.Description,
				Message:       err.Error(),
			})
			continue
		}
		for _, raw := range models {
			model := parseAvailableModel(raw, source)
			if model == nil {
				continue
			}
			existing, ok := modelsByID[model.ID]
			if !ok {
				modelsByID[model.ID] = *model
				continue
			}
			mergeAvailableModel(&existing, *model)
			modelsByID[model.ID] = existing
		}
	}
	for _, model := range modelsByID {
		suggestedProvider := suggestedPriceProvider(model)
		price := findCatalogPrice(priceLookup, prices, suggestedProvider, model.Owner, model.ID)
		item := ModelPriceCatalogItem{
			ID:                model.ID,
			Name:              model.Name,
			Object:            model.Object,
			Owner:             model.Owner,
			Created:           model.Created,
			Metadata:          model.Metadata,
			SuggestedProvider: suggestedProvider,
			Price:             price,
			Sources:           model.Sources,
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		if !modelPriceReadyForBilling(item.Price, item.ID) {
			response.UnpricedModels++
		} else {
			response.PricedModels++
		}
		response.Models = append(response.Models, item)
	}
	sort.Slice(response.Models, func(i, j int) bool {
		left, right := response.Models[i], response.Models[j]
		if (left.Price == nil) != (right.Price == nil) {
			return left.Price == nil
		}
		return strings.ToLower(left.ID) < strings.ToLower(right.ID)
	})
	return response, nil
}

func (a *App) modelCatalogAPIKeys(ctx context.Context) ([]modelCatalogAPIKey, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT k.api_key_hash, k.user_id, k.api_key, k.description, CAST(k.created_at AS TEXT), CAST(k.updated_at AS TEXT),
		       u.username, u.nickname
		FROM user_api_keys k
		INNER JOIN users u ON u.id = k.user_id
		WHERE u.disabled_at IS NULL
		ORDER BY lower(u.username), lower(k.description), k.api_key_hash
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []modelCatalogAPIKey
	for rows.Next() {
		var item modelCatalogAPIKey
		var apiKey, createdAt, updatedAt, username, nickname sql.NullString
		if err := rows.Scan(&item.APIKeyHash, &item.UserID, &apiKey, &item.Description, &createdAt, &updatedAt, &username, &nickname); err != nil {
			return nil, err
		}
		item.APIKey = nullableString(apiKey)
		item.CreatedAt = timePtr(createdAt)
		item.UpdatedAt = timePtr(updatedAt)
		item.UserLabel = userLabelFromParts(username, nickname)
		result = append(result, item)
	}
	return result, rows.Err()
}

func pricesByKey(prices []ModelPrice) map[[2]string]ModelPrice {
	result := make(map[[2]string]ModelPrice, len(prices))
	for _, price := range prices {
		result[priceKey(price.Provider, price.Model)] = price
	}
	return result
}

func catalogAvailableModelSource(binding modelCatalogAPIKey) AvailableModelSource {
	source := availableModelSource(binding.UserAPIKey)
	userID := binding.UserID
	source.UserID = &userID
	source.UserLabel = binding.UserLabel
	return source
}

func userLabelFromParts(username, nickname sql.NullString) string {
	label := strings.TrimSpace(nickname.String)
	if label == "" {
		label = strings.TrimSpace(username.String)
	}
	if label == "" {
		label = "未知用户"
	}
	return label
}

func suggestedPriceProvider(model AvailableModelItem) string {
	if model.Owner != nil {
		if owner := strings.TrimSpace(*model.Owner); owner != "" {
			return owner
		}
	}
	if idx := strings.Index(model.ID, "/"); idx > 0 {
		return strings.TrimSpace(model.ID[:idx])
	}
	return ""
}

func findCatalogPrice(prices map[[2]string]ModelPrice, allPrices []ModelPrice, suggestedProvider string, owner *string, modelID string) *ModelPrice {
	providers := []string{}
	if owner != nil {
		providers = append(providers, *owner)
	}
	if strings.TrimSpace(suggestedProvider) != "" {
		providers = append(providers, suggestedProvider)
	}
	modelCandidates := catalogModelCandidates(modelID)
	for _, provider := range providers {
		for _, candidate := range modelCandidates {
			if price := findMatchingPrice(prices, &provider, &candidate); price != nil {
				return price
			}
		}
	}
	var matched *ModelPrice
	for _, price := range allPrices {
		priceModel := strings.ToLower(strings.TrimSpace(price.Model))
		modelMatches := false
		for _, candidate := range modelCandidates {
			if priceModel == strings.ToLower(strings.TrimSpace(candidate)) {
				modelMatches = true
				break
			}
		}
		if !modelMatches {
			continue
		}
		if matched != nil {
			return nil
		}
		candidate := price
		matched = &candidate
	}
	return matched
}

func catalogModelCandidates(modelID string) []string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	candidates := []string{modelID}
	if idx := strings.Index(modelID, "/"); idx > 0 && idx < len(modelID)-1 {
		candidates = append(candidates, strings.TrimSpace(modelID[idx+1:]))
	}
	return candidates
}

func scanPrices(rows *sql.Rows) ([]ModelPrice, error) {
	var prices []ModelPrice
	for rows.Next() {
		var price ModelPrice
		var sourceModel, lastSynced, updatedAt sql.NullString
		var requestUSD sql.NullFloat64
		if err := rows.Scan(&price.ID, &price.Provider, &price.Model, &price.InputUSDPerMillion, &price.OutputUSDPerMillion, &price.CacheReadUSDPerMillion, &price.CacheCreationUSDPerMillion, &requestUSD, &price.Source, &sourceModel, &price.AutoSynced, &lastSynced, &updatedAt); err != nil {
			return nil, err
		}
		if requestUSD.Valid {
			price.RequestUSD = &requestUSD.Float64
		}
		price.BillingUnit = billingUnitForModel(price.Model)
		price.SourceModel = nullableString(sourceModel)
		price.LastSyncedAt = timePtr(lastSynced)
		if parsed, ok := parseDBTime(updatedAt.String); ok {
			price.UpdatedAt = parsed
		}
		prices = append(prices, price)
	}
	return prices, rows.Err()
}

func (a *App) createPrice(ctx context.Context, payload modelPricePayload) (ModelPrice, error) {
	payload, err := validatePricePayload(payload)
	if err != nil {
		return ModelPrice{}, err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO model_prices (
			provider, model, input_usd_per_million, output_usd_per_million,
			cache_read_usd_per_million, cache_creation_usd_per_million, request_usd,
			source, source_model, auto_synced, last_synced_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'manual', NULL, 0, NULL, ?)
	`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CacheReadUSDPerMillion, payload.CacheCreationUSDPerMillion, nullableFloatArg(payload.RequestUSD), now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ModelPrice{}, conflictError("该 provider/model 价格已存在")
		}
		return ModelPrice{}, err
	}
	id, _ := result.LastInsertId()
	return a.getPrice(ctx, int(id))
}

func (a *App) updatePrice(ctx context.Context, id int, payload modelPricePayload) (ModelPrice, error) {
	payload, err := validatePricePayload(payload)
	if err != nil {
		return ModelPrice{}, err
	}
	result, err := a.db.ExecContext(ctx, `
		UPDATE model_prices
		SET provider = ?, model = ?, input_usd_per_million = ?, output_usd_per_million = ?,
		    cache_read_usd_per_million = ?, cache_creation_usd_per_million = ?,
		    request_usd = ?, source = 'manual',
		    source_model = NULL, auto_synced = 0, last_synced_at = NULL, updated_at = ?
		WHERE id = ?
	`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CacheReadUSDPerMillion, payload.CacheCreationUSDPerMillion, nullableFloatArg(payload.RequestUSD), dbTime(time.Now()), id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ModelPrice{}, conflictError("该 provider/model 价格已存在")
		}
		return ModelPrice{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ModelPrice{}, notFoundError("模型价格不存在")
	}
	return a.getPrice(ctx, id)
}

func (a *App) deletePrice(ctx context.Context, id int) error {
	result, err := a.db.ExecContext(ctx, `DELETE FROM model_prices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return notFoundError("模型价格不存在")
	}
	return nil
}

func (a *App) getPrice(ctx context.Context, id int) (ModelPrice, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, provider, model, input_usd_per_million, output_usd_per_million,
		       cache_read_usd_per_million, cache_creation_usd_per_million, request_usd,
		       source, source_model, auto_synced, CAST(last_synced_at AS TEXT), CAST(updated_at AS TEXT)
		FROM model_prices WHERE id = ?
	`, id)
	if err != nil {
		return ModelPrice{}, err
	}
	defer rows.Close()
	prices, err := scanPrices(rows)
	if err != nil {
		return ModelPrice{}, err
	}
	if len(prices) == 0 {
		return ModelPrice{}, notFoundError("模型价格不存在")
	}
	return prices[0], nil
}

func (a *App) handleSyncLiteLLMPrices(w http.ResponseWriter, r *http.Request) error {
	body := readAllAndRestore(r)
	var payload modelPriceSyncRequest
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
	}
	sourceURL := defaultLiteLLMPricingURL
	if payload.SourceURL != nil && strings.TrimSpace(*payload.SourceURL) != "" {
		sourceURL = strings.TrimSpace(*payload.SourceURL)
	}
	if err := ensureHTTPSURL(sourceURL); err != nil {
		return err
	}
	cfg, err := a.loadConfig(r.Context())
	if err != nil {
		return err
	}
	client, err := liteLLMHTTPClient(30*time.Second, cfg.LiteLLMProxy)
	if err != nil {
		return err
	}
	response, rawPayload, err := doJSON(r.Context(), client, http.MethodGet, sourceURL, nil, nil)
	if err != nil {
		return validationError("下载 LiteLLM 价格数据失败")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return validationError(fmt.Sprintf("下载 LiteLLM 价格数据失败：HTTP %d", response.StatusCode))
	}
	var rawData map[string]any
	if err := json.Unmarshal(rawPayload, &rawData); err != nil {
		return validationError("LiteLLM 价格数据不是有效 JSON")
	}
	result, err := a.syncLiteLLMPrices(r.Context(), sourceURL, rawData)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (a *App) syncLiteLLMPrices(ctx context.Context, sourceURL string, rawData map[string]any) (map[string]any, error) {
	now := dbTime(time.Now())
	type litellmPriceRow struct {
		modelName string
		payload   modelPricePayload
	}
	rows := make([]litellmPriceRow, 0, len(rawData))
	skippedInvalid := 0
	for modelName, rawEntry := range rawData {
		if modelName == "sample_spec" {
			skippedInvalid++
			continue
		}
		payload, ok := litellmEntryToPrice(modelName, rawEntry)
		if !ok {
			skippedInvalid++
			continue
		}
		rows = append(rows, litellmPriceRow{modelName: modelName, payload: payload})
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM model_prices WHERE source = 'litellm'`); err != nil {
		return nil, err
	}
	inserted, skippedManual := 0, 0
	for _, row := range rows {
		payload := row.payload
		result, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO model_prices (
				provider, model, input_usd_per_million, output_usd_per_million,
				cache_read_usd_per_million, cache_creation_usd_per_million, request_usd,
				source, source_model, auto_synced, last_synced_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'litellm', ?, 1, ?, ?)
		`, payload.Provider, payload.Model, payload.InputUSDPerMillion, payload.OutputUSDPerMillion, payload.CacheReadUSDPerMillion, payload.CacheCreationUSDPerMillion, nullableFloatArg(payload.RequestUSD), row.modelName, now, now)
		if err != nil {
			return nil, err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			skippedManual++
			continue
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return map[string]any{
		"source_url":      sourceURL,
		"total_entries":   len(rawData),
		"imported":        inserted,
		"created":         inserted,
		"updated":         0,
		"unchanged":       0,
		"skipped_manual":  skippedManual,
		"skipped_invalid": skippedInvalid,
	}, nil
}

func litellmEntryToPrice(modelName string, rawEntry any) (modelPricePayload, bool) {
	entry, ok := rawEntry.(map[string]any)
	if !ok {
		return modelPricePayload{}, false
	}
	provider := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["litellm_provider"])))
	model := strings.TrimSpace(modelName)
	if provider == "" || model == "" || len(provider) > 120 || len(model) > 180 {
		return modelPricePayload{}, false
	}
	payload := modelPricePayload{
		Provider:                   provider,
		Model:                      model,
		InputUSDPerMillion:         usdPerMillion(entry["input_cost_per_token"]),
		OutputUSDPerMillion:        usdPerMillion(entry["output_cost_per_token"]),
		CacheReadUSDPerMillion:     usdPerMillion(entry["cache_read_input_token_cost"]),
		CacheCreationUSDPerMillion: usdPerMillion(entry["cache_creation_input_token_cost"]),
	}
	if payload.InputUSDPerMillion == 0 && payload.OutputUSDPerMillion == 0 && payload.CacheReadUSDPerMillion == 0 && payload.CacheCreationUSDPerMillion == 0 {
		return modelPricePayload{}, false
	}
	return payload, true
}

func usdPerMillion(value any) float64 {
	number, ok := numeric(value)
	if !ok || number < 0 {
		return 0
	}
	return mathRound(number*1_000_000, 12)
}

func numeric(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func mathRound(value float64, places int) float64 {
	factor := 1.0
	for i := 0; i < places; i++ {
		factor *= 10
	}
	if value >= 0 {
		return float64(int64(value*factor+0.5)) / factor
	}
	return float64(int64(value*factor-0.5)) / factor
}

func pricesEqual(item ModelPrice, payload modelPricePayload) bool {
	return item.Provider == payload.Provider &&
		item.Model == payload.Model &&
		item.InputUSDPerMillion == payload.InputUSDPerMillion &&
		item.OutputUSDPerMillion == payload.OutputUSDPerMillion &&
		item.CacheReadUSDPerMillion == payload.CacheReadUSDPerMillion &&
		item.CacheCreationUSDPerMillion == payload.CacheCreationUSDPerMillion &&
		floatPtrEqual(item.RequestUSD, payload.RequestUSD)
}

func priceKey(provider, model string) [2]string {
	return [2]string{strings.ToLower(strings.TrimSpace(provider)), strings.ToLower(strings.TrimSpace(model))}
}

func findMatchingPrice(prices map[[2]string]ModelPrice, provider, model *string) *ModelPrice {
	if provider == nil || model == nil {
		return nil
	}
	providerKey := strings.ToLower(strings.TrimSpace(*provider))
	modelKey := strings.ToLower(strings.TrimSpace(*model))
	if providerKey == "" || modelKey == "" {
		return nil
	}
	candidates := []string{providerKey}
	if providerKey == "codex" {
		candidates = append(candidates, "openai")
	}
	if providerKey == "claude" {
		candidates = append(candidates, "anthropic")
	}
	for _, candidate := range candidates {
		if price, ok := prices[[2]string{candidate, modelKey}]; ok {
			return &price
		}
	}
	return nil
}

func recordCost(record UsageRecord, prices map[[2]string]ModelPrice) (float64, bool) {
	price := findMatchingPrice(prices, record.Provider, record.Model)
	if billingUnitForModelPtr(record.Model) == modelBillingUnitRequest {
		if record.Failed {
			return 0, false
		}
		if price == nil || price.RequestUSD == nil {
			return 0, true
		}
		return mathRound(*price.RequestUSD, 8), false
	}
	if price == nil {
		return 0, usageAggregateTotalTokens(record) > 0
	}
	inputTokens := nonNegativeTokens(record.InputTokens)
	outputTokens := nonNegativeTokens(record.OutputTokens)
	amount := 0.0
	if isClaudeProvider(record.Provider) {
		amount = millionTokenCost(inputTokens, price.InputUSDPerMillion) +
			millionTokenCost(nonNegativeTokens(record.CacheReadTokens), price.CacheReadUSDPerMillion) +
			millionTokenCost(nonNegativeTokens(record.CacheCreationTokens), price.CacheCreationUSDPerMillion) +
			millionTokenCost(outputTokens, price.OutputUSDPerMillion)
	} else {
		cacheRead := boundedTokens(record.CachedTokens, inputTokens)
		normalInput := inputTokens - cacheRead
		amount = millionTokenCost(normalInput, price.InputUSDPerMillion) +
			millionTokenCost(cacheRead, price.CacheReadUSDPerMillion) +
			millionTokenCost(outputTokens, price.OutputUSDPerMillion)
	}
	return mathRound(amount, 8), false
}

func liteLLMHTTPClient(timeout time.Duration, proxyCfg LiteLLMProxyConfig) (*http.Client, error) {
	client := httpClient(timeout)
	if !proxyCfg.Enabled {
		return client, nil
	}
	normalized, err := normalizeLiteLLMProxyConfig(proxyCfg)
	if err != nil {
		return nil, err
	}
	proxyURL, err := url.Parse(normalized.ProxyURL)
	if err != nil {
		return nil, validationError("代理地址必须是有效的 http://、https:// 或 socks5:// 地址")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport has unexpected type")
	}
	cloned := transport.Clone()
	cloned.Proxy = http.ProxyURL(proxyURL)
	client.Transport = cloned
	return client, nil
}

func normalizeLiteLLMProxyConfig(input LiteLLMProxyConfig) (LiteLLMProxyConfig, error) {
	proxyURL, err := normalizeLiteLLMProxyURL(input.ProxyURL)
	if err != nil {
		return LiteLLMProxyConfig{}, err
	}
	if input.Enabled && proxyURL == "" {
		return LiteLLMProxyConfig{}, validationError("启用代理时必须填写代理地址")
	}
	return LiteLLMProxyConfig{Enabled: input.Enabled, ProxyURL: proxyURL}, nil
}

func normalizeLiteLLMProxyURL(value string) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", nil
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" || strings.TrimSpace(parsed.Hostname()) == "" {
		return "", validationError("代理地址必须是有效的 http://、https:// 或 socks5:// 地址")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
		parsed.Scheme = strings.ToLower(parsed.Scheme)
	case "sock5":
		parsed.Scheme = "socks5"
	default:
		return "", validationError("代理地址必须是有效的 http://、https:// 或 socks5:// 地址")
	}
	return parsed.String(), nil
}

func usageAggregateInputTokens(record UsageRecord) int {
	inputTokens := nonNegativeTokens(record.InputTokens)
	if isClaudeProvider(record.Provider) {
		return inputTokens + nonNegativeTokens(record.CacheReadTokens) + nonNegativeTokens(record.CacheCreationTokens)
	}
	return inputTokens
}

// usageCacheHitTokens returns the tokens served from provider prompt cache.
// Claude-style records report cache reads in cache_read_tokens (kept outside
// input_tokens); everything else reports cached_tokens as a subset of input.
func usageCacheHitTokens(record UsageRecord) int {
	if isClaudeProvider(record.Provider) {
		return nonNegativeTokens(record.CacheReadTokens)
	}
	return nonNegativeTokens(record.CachedTokens)
}

func usageAggregateTotalTokens(record UsageRecord) int {
	if isClaudeProvider(record.Provider) {
		return usageAggregateInputTokens(record) + nonNegativeTokens(record.OutputTokens) + nonNegativeTokens(record.ReasoningTokens)
	}
	return nonNegativeTokens(record.TotalTokens)
}

func isClaudeProvider(provider *string) bool {
	if provider == nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(*provider))
	return normalized == "claude" || normalized == "anthropic"
}

func boundedTokens(tokens, max int) int {
	tokens = nonNegativeTokens(tokens)
	max = nonNegativeTokens(max)
	if tokens > max {
		return max
	}
	return tokens
}

func nonNegativeTokens(tokens int) int {
	if tokens < 0 {
		return 0
	}
	return tokens
}

func millionTokenCost(tokens int, usdPerMillion float64) float64 {
	return float64(tokens) / 1_000_000 * usdPerMillion
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func nullableFloatArg(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func floatPtrEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func billingUnitForModel(model string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(model)), "image") {
		return modelBillingUnitRequest
	}
	return modelBillingUnitToken
}

func billingUnitForModelPtr(model *string) string {
	if model == nil {
		return modelBillingUnitToken
	}
	return billingUnitForModel(*model)
}

func modelPriceReadyForBilling(price *ModelPrice, fallbackModel string) bool {
	if price == nil {
		return false
	}
	model := price.Model
	if strings.TrimSpace(model) == "" {
		model = fallbackModel
	}
	if billingUnitForModel(model) == modelBillingUnitRequest {
		return price.RequestUSD != nil
	}
	return true
}
