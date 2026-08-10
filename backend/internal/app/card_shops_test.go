package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	backendApp "cpa-helper/backend/internal/app"
)

func TestCardShopsProxyReturnsUpstreamShopsForAdmin(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.URL.Path != "/api/shops" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"shops": []map[string]any{
				{
					"id":            "ldxp-test",
					"shopName":      "测试小店",
					"shopUrl":       "https://pay.ldxp.cn/shop/TEST",
					"telegram":      "@test",
					"shopSellCount": 12,
					"productItems": []map[string]any{
						{
							"name":       "Codex 接码",
							"price":      3.5,
							"stockCount": 9,
							"itemUrl":    "https://pay.ldxp.cn/item/test",
							"category":   "codex",
							"group":      "GPT/Codex",
						},
					},
					"updatedAt": "2026-06-03T10:08:01.983Z",
				},
			},
		})
	}))
	defer upstream.Close()
	t.Setenv("CPA_HELPER_CARD_SHOPS_URL", upstream.URL+"/api/shops")

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

	var response struct {
		Shops []struct {
			ID           string `json:"id"`
			ShopName     string `json:"shopName"`
			ProductItems []struct {
				Name       string  `json:"name"`
				Price      float64 `json:"price"`
				StockCount int     `json:"stockCount"`
			} `json:"productItems"`
		} `json:"shops"`
		FetchedAt string `json:"fetched_at"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/card-shops", nil, cookies, &response)

	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
	}
	if response.FetchedAt == "" {
		t.Fatal("fetched_at is empty")
	}
	if len(response.Shops) != 1 || response.Shops[0].ID != "ldxp-test" || response.Shops[0].ShopName != "测试小店" {
		t.Fatalf("shops = %#v, want upstream shop", response.Shops)
	}
	if got := response.Shops[0].ProductItems[0]; got.Name != "Codex 接码" || got.Price != 3.5 || got.StockCount != 9 {
		t.Fatalf("product item = %#v, want upstream product", got)
	}
}

func TestCardShopsProxyNormalizesProductSummaryPreviewItems(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"shops": []map[string]any{
				{
					"id":              "summary-shop",
					"productsInStock": []string{"Codex 摘要商品", "自定义分组商品", "部分字段无效商品", "仅名称商品"},
					"productItems":    []map[string]any{},
					"productSummary": map[string]any{
						"totalCount": 8,
						"groups": []map[string]any{
							{
								"group": "GPT/Codex",
								"previewItems": []any{
									map[string]any{
										"name":       "Codex 摘要商品",
										"price":      3.5,
										"stockCount": 9,
										"salesCount": 12,
										"itemUrl":    "https://pay.ldxp.cn/item/summary",
										"category":   "account",
									},
									map[string]any{
										"name":  "自定义分组商品",
										"group": "Custom",
									},
									map[string]any{
										"name":     "部分字段无效商品",
										"itemUrl":  42,
										"category": false,
										"group":    7,
									},
									map[string]any{"name": 123},
									"invalid-item",
								},
							},
							{
								"group":        "无效分组",
								"previewItems": "invalid-preview-items",
							},
						},
					},
				},
				{
					"id": "legacy-shop",
					"productItems": []map[string]any{
						{"name": "旧版商品", "price": 6.5},
					},
					"productSummary": map[string]any{
						"groups": []map[string]any{
							{
								"group": "摘要分组",
								"previewItems": []map[string]any{
									{"name": "不应覆盖旧版商品"},
								},
							},
						},
					},
				},
				{
					"id":              "fallback-shop",
					"productsInStock": []string{"仅名称商品"},
					"productItems":    []map[string]any{},
					"productSummary":  map[string]any{"groups": "invalid-groups"},
				},
			},
		})
	}))
	defer upstream.Close()
	t.Setenv("CPA_HELPER_CARD_SHOPS_URL", upstream.URL)

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

	var response struct {
		Shops []struct {
			ID           string `json:"id"`
			ProductItems []struct {
				Name       string   `json:"name"`
				Price      *float64 `json:"price"`
				StockCount *int     `json:"stockCount"`
				SalesCount *int     `json:"salesCount"`
				ItemURL    string   `json:"itemUrl"`
				Category   string   `json:"category"`
				Group      string   `json:"group"`
			} `json:"productItems"`
			ProductSummary struct {
				TotalCount int `json:"totalCount"`
			} `json:"productSummary"`
		} `json:"shops"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/card-shops", nil, cookies, &response)

	if len(response.Shops) != 3 {
		t.Fatalf("shops = %#v, want 3 shops", response.Shops)
	}
	summaryShop := response.Shops[0]
	if summaryShop.ProductSummary.TotalCount != 8 {
		t.Fatalf("summary total count = %d, want 8", summaryShop.ProductSummary.TotalCount)
	}
	if len(summaryShop.ProductItems) != 4 {
		t.Fatalf("summary product items = %#v, want 3 valid preview items and 1 name fallback", summaryShop.ProductItems)
	}
	firstItem := summaryShop.ProductItems[0]
	if firstItem.Name != "Codex 摘要商品" || firstItem.Price == nil || *firstItem.Price != 3.5 ||
		firstItem.StockCount == nil || *firstItem.StockCount != 9 ||
		firstItem.SalesCount == nil || *firstItem.SalesCount != 12 ||
		firstItem.ItemURL != "https://pay.ldxp.cn/item/summary" || firstItem.Category != "account" ||
		firstItem.Group != "GPT/Codex" {
		t.Fatalf("summary product item = %#v, want normalized preview details", firstItem)
	}
	if got := summaryShop.ProductItems[1].Group; got != "Custom" {
		t.Fatalf("explicit item group = %q, want Custom", got)
	}
	if got := summaryShop.ProductItems[2]; got.Name != "部分字段无效商品" || got.ItemURL != "" ||
		got.Category != "" || got.Group != "GPT/Codex" {
		t.Fatalf("partially invalid product item = %#v, want sanitized strings and inherited group", got)
	}
	if got := summaryShop.ProductItems[3]; got.Name != "仅名称商品" || got.Price != nil || got.StockCount != nil {
		t.Fatalf("name-only fallback item = %#v, want preserved product name without details", got)
	}
	if got := response.Shops[1].ProductItems; len(got) != 1 || got[0].Name != "旧版商品" {
		t.Fatalf("legacy product items = %#v, want original item", got)
	}
	if got := response.Shops[2].ProductItems; len(got) != 0 {
		t.Fatalf("invalid summary product items = %#v, want empty fallback", got)
	}
}

func TestCardShopsProxyRequiresAdmin(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"shops":[]}`))
	}))
	defer upstream.Close()
	t.Setenv("CPA_HELPER_CARD_SHOPS_URL", upstream.URL)

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

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/card-shops", nil, nil, http.StatusUnauthorized)
	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/card-shops", nil, memberCookies, http.StatusForbidden)
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0 for unauthorized requests", upstreamCalls)
	}
}

func TestCardShopsProxySurfacesUpstreamFailures(t *testing.T) {
	tests := []struct {
		name string
		fn   http.HandlerFunc
	}{
		{
			name: "non-2xx",
			fn: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "failed", http.StatusBadGateway)
			},
		},
		{
			name: "invalid-json",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"shops":`))
			},
		},
		{
			name: "missing-shops",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"items":[]}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
			upstream := httptest.NewServer(tt.fn)
			defer upstream.Close()
			t.Setenv("CPA_HELPER_CARD_SHOPS_URL", upstream.URL)

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

			requestJSONExpectStatus(t, handler, http.MethodGet, "/api/card-shops", nil, cookies, http.StatusBadGateway)
		})
	}
}

func TestCardShopFavoritesAreScopedToCurrentUser(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

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

	var favorites struct {
		ShopKeys []string `json:"shop_keys"`
	}
	requestJSON(t, handler, http.MethodPut, "/api/card-shops/favorites", map[string]any{
		"shop_key": "shop-admin",
		"favorite": true,
	}, adminCookies, &favorites)
	if got, want := favorites.ShopKeys, []string{"shop-admin"}; !stringSlicesEqual(got, want) {
		t.Fatalf("admin favorites = %#v, want %#v", got, want)
	}

	requestJSON(t, handler, http.MethodGet, "/api/card-shops/favorites", nil, memberCookies, &favorites)
	if len(favorites.ShopKeys) != 0 {
		t.Fatalf("member favorites before own update = %#v, want empty", favorites.ShopKeys)
	}

	requestJSON(t, handler, http.MethodPut, "/api/card-shops/favorites", map[string]any{
		"shop_key": "shop-member",
		"favorite": true,
	}, memberCookies, &favorites)
	if got, want := favorites.ShopKeys, []string{"shop-member"}; !stringSlicesEqual(got, want) {
		t.Fatalf("member favorites = %#v, want %#v", got, want)
	}

	requestJSON(t, handler, http.MethodGet, "/api/card-shops/favorites", nil, adminCookies, &favorites)
	if got, want := favorites.ShopKeys, []string{"shop-admin"}; !stringSlicesEqual(got, want) {
		t.Fatalf("admin favorites after member update = %#v, want %#v", got, want)
	}

	requestJSON(t, handler, http.MethodPut, "/api/card-shops/favorites", map[string]any{
		"shop_key": "shop-admin",
		"favorite": false,
	}, adminCookies, &favorites)
	if len(favorites.ShopKeys) != 0 {
		t.Fatalf("admin favorites after removal = %#v, want empty", favorites.ShopKeys)
	}
}

func TestCardShopFavoritesRequireLoginAndValidShopKey(t *testing.T) {
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

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/card-shops/favorites", nil, nil, http.StatusUnauthorized)
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/card-shops/favorites", map[string]any{
		"shop_key": "   ",
		"favorite": true,
	}, cookies, http.StatusUnprocessableEntity)
}

func TestCardShopTagsAreScopedToCurrentUser(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

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

	var tags struct {
		Tags []string `json:"tags"`
	}
	requestJSON(t, handler, http.MethodGet, "/api/card-shops/tags", nil, adminCookies, &tags)
	if len(tags.Tags) != 0 {
		t.Fatalf("initial admin tags = %#v, want empty", tags.Tags)
	}

	requestJSON(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": []string{" Codex ", "PayPal", "实体卡"},
	}, adminCookies, &tags)
	if got, want := tags.Tags, []string{"Codex", "PayPal", "实体卡"}; !stringSlicesEqual(got, want) {
		t.Fatalf("admin tags = %#v, want %#v", got, want)
	}

	requestJSON(t, handler, http.MethodGet, "/api/card-shops/tags", nil, memberCookies, &tags)
	if len(tags.Tags) != 0 {
		t.Fatalf("initial member tags = %#v, want empty", tags.Tags)
	}

	requestJSON(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": []string{"Team"},
	}, memberCookies, &tags)
	if got, want := tags.Tags, []string{"Team"}; !stringSlicesEqual(got, want) {
		t.Fatalf("member tags = %#v, want %#v", got, want)
	}

	requestJSON(t, handler, http.MethodGet, "/api/card-shops/tags", nil, adminCookies, &tags)
	if got, want := tags.Tags, []string{"Codex", "PayPal", "实体卡"}; !stringSlicesEqual(got, want) {
		t.Fatalf("admin tags after member update = %#v, want %#v", got, want)
	}

	requestJSON(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": []string{},
	}, adminCookies, &tags)
	if len(tags.Tags) != 0 {
		t.Fatalf("admin tags after clear = %#v, want empty", tags.Tags)
	}
}

func TestCardShopTagsRequireLoginAndValidTags(t *testing.T) {
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

	requestJSONExpectStatus(t, handler, http.MethodGet, "/api/card-shops/tags", nil, nil, http.StatusUnauthorized)
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": []string{"   "},
	}, cookies, http.StatusUnprocessableEntity)
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": []string{"Plus", "plus"},
	}, cookies, http.StatusUnprocessableEntity)
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": []string{"123456789012345678901234567890123"},
	}, cookies, http.StatusUnprocessableEntity)

	tooManyTags := make([]string, 31)
	for index := range tooManyTags {
		tooManyTags[index] = "tag-" + string(rune('a'+index))
	}
	requestJSONExpectStatus(t, handler, http.MethodPut, "/api/card-shops/tags", map[string]any{
		"tags": tooManyTags,
	}, cookies, http.StatusUnprocessableEntity)
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
