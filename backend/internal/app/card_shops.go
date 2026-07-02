package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultCardShopsURL     = "https://ldxp.qizhang.org/api/shops"
	cardShopsTimeout        = 10 * time.Second
	maxCardShopTags         = 30
	maxCardShopTagRuneCount = 32
)

type cardShopsResponse struct {
	Shops     []map[string]any `json:"shops"`
	FetchedAt string           `json:"fetched_at"`
}

type cardShopFavoritesResponse struct {
	ShopKeys []string `json:"shop_keys"`
}

type cardShopFavoriteUpdateRequest struct {
	ShopKey  string `json:"shop_key"`
	Favorite bool   `json:"favorite"`
}

type cardShopTagsResponse struct {
	Tags []string `json:"tags"`
}

type cardShopTagsUpdateRequest struct {
	Tags []string `json:"tags"`
}

func (a *App) handleCardShops(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}

	shops, err := fetchCardShops(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, cardShopsResponse{
		Shops:     shops,
		FetchedAt: apiDateTime(time.Now()),
	})
	return nil
}

func (a *App) handleCardShopFavorites(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}

	switch r.Method {
	case http.MethodGet:
		keys, err := a.cardShopFavoriteKeys(r.Context(), user.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopFavoritesResponse{ShopKeys: keys})
		return nil
	case http.MethodPut:
		var payload cardShopFavoriteUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		shopKey, err := normalizeCardShopFavoriteKey(payload.ShopKey)
		if err != nil {
			return err
		}
		if err := a.setCardShopFavorite(r.Context(), user.ID, shopKey, payload.Favorite); err != nil {
			return err
		}
		keys, err := a.cardShopFavoriteKeys(r.Context(), user.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopFavoritesResponse{ShopKeys: keys})
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) handleCardShopTags(w http.ResponseWriter, r *http.Request) error {
	user, err := a.readyUser(r.Context(), r)
	if err != nil {
		return err
	}

	switch r.Method {
	case http.MethodGet:
		tags, err := a.cardShopTags(r.Context(), user.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopTagsResponse{Tags: tags})
		return nil
	case http.MethodPut:
		var payload cardShopTagsUpdateRequest
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		tags, err := normalizeCardShopTags(payload.Tags)
		if err != nil {
			return err
		}
		if err := a.replaceCardShopTags(r.Context(), user.ID, tags); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, cardShopTagsResponse{Tags: tags})
		return nil
	default:
		return methodNotAllowed()
	}
}

func fetchCardShops(ctx context.Context) ([]map[string]any, error) {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	response, payload, err := doJSON(ctx, httpClient(cardShopsTimeout), http.MethodGet, cardShopsSourceURL(), headers, nil)
	if err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据源暂时不可用")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据源暂时不可用")
	}

	var raw struct {
		Shops json.RawMessage `json:"shops"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据格式无效")
	}
	if len(raw.Shops) == 0 {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据格式无效")
	}
	var shops []map[string]any
	shopDecoder := json.NewDecoder(bytes.NewReader(raw.Shops))
	shopDecoder.UseNumber()
	if err := shopDecoder.Decode(&shops); err != nil {
		return nil, appError("upstream_error", http.StatusBadGateway, "卡网收录数据格式无效")
	}
	if shops == nil {
		shops = []map[string]any{}
	}
	normalizeCardShopProductItems(shops)
	return shops, nil
}

func cardShopsSourceURL() string {
	if value := strings.TrimSpace(os.Getenv("CPA_HELPER_CARD_SHOPS_URL")); value != "" {
		return value
	}
	return defaultCardShopsURL
}

func normalizeCardShopProductItems(shops []map[string]any) {
	for _, shop := range shops {
		if lenCardShopItems(shop["productItems"]) > 0 {
			continue
		}
		if previewItems := cardShopSummaryPreviewItems(shop); len(previewItems) > 0 {
			shop["productItems"] = previewItems
		}
	}
}

func lenCardShopItems(value any) int {
	switch items := value.(type) {
	case []any:
		return len(items)
	case []map[string]any:
		return len(items)
	default:
		return 0
	}
}

func cardShopSummaryPreviewItems(shop map[string]any) []map[string]any {
	summary, ok := shop["productSummary"].(map[string]any)
	if !ok {
		return nil
	}
	groups, ok := summary["groups"].([]any)
	if !ok {
		return nil
	}

	items := []map[string]any{}
	seen := map[string]struct{}{}
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if !ok {
			continue
		}
		groupName := cardShopStringField(group, "group")
		previewItems, ok := group["previewItems"].([]any)
		if !ok {
			continue
		}
		for _, previewValue := range previewItems {
			preview, ok := previewValue.(map[string]any)
			if !ok {
				continue
			}
			item := copyCardShopMap(preview)
			if groupName != "" && cardShopStringField(item, "group") == "" {
				item["group"] = groupName
			}
			key := cardShopProductKey(item, groupName)
			if key != "" {
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
			}
			items = append(items, item)
		}
	}
	return items
}

func copyCardShopMap(source map[string]any) map[string]any {
	target := make(map[string]any, len(source)+1)
	for key, value := range source {
		target[key] = value
	}
	return target
}

func cardShopStringField(source map[string]any, key string) string {
	value, ok := source[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func cardShopProductKey(item map[string]any, groupName string) string {
	if itemURL := cardShopStringField(item, "itemUrl"); itemURL != "" {
		return "url:" + itemURL
	}
	if name := cardShopStringField(item, "name"); name != "" {
		return "name:" + groupName + ":" + name
	}
	return ""
}

func normalizeCardShopFavoriteKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", validationError("店铺标识不能为空")
	}
	if len([]rune(key)) > 1024 {
		return "", validationError("店铺标识过长")
	}
	return key, nil
}

func normalizeCardShopTags(values []string) ([]string, error) {
	if len(values) > maxCardShopTags {
		return nil, validationError("快速搜索标签不能超过 30 个")
	}

	tags := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag == "" {
			return nil, validationError("快速搜索标签不能为空")
		}
		if len([]rune(tag)) > maxCardShopTagRuneCount {
			return nil, validationError("快速搜索标签不能超过 32 个字符")
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			return nil, validationError("快速搜索标签不能重复")
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (a *App) cardShopFavoriteKeys(ctx context.Context, userID int) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT shop_key
		FROM user_card_shop_favorites
		WHERE user_id = ?
		ORDER BY created_at ASC, shop_key ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

func (a *App) setCardShopFavorite(ctx context.Context, userID int, shopKey string, favorite bool) error {
	if favorite {
		now := dbTime(time.Now())
		_, err := a.db.ExecContext(ctx, `
			INSERT INTO user_card_shop_favorites (user_id, shop_key, created_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, shop_key) DO UPDATE SET updated_at = excluded.updated_at
		`, userID, shopKey, now, now)
		return err
	}

	_, err := a.db.ExecContext(ctx, `
		DELETE FROM user_card_shop_favorites
		WHERE user_id = ? AND shop_key = ?
	`, userID, shopKey)
	return err
}

func (a *App) cardShopTags(ctx context.Context, userID int) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT tag
		FROM user_card_shop_tags
		WHERE user_id = ?
		ORDER BY position ASC, tag ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tags, nil
}

func (a *App) replaceCardShopTags(ctx context.Context, userID int, tags []string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_card_shop_tags WHERE user_id = ?`, userID); err != nil {
		return err
	}

	now := dbTime(time.Now())
	for index, tag := range tags {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_card_shop_tags (user_id, tag, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, userID, tag, index, now, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}
