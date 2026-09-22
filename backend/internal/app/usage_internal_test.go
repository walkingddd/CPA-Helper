package app

import (
	"context"
	"database/sql"
	"testing"
)

func TestNormalizeUsagePrefersTopLevelTokenSummary(t *testing.T) {
	raw := []byte(`{
		"provider":"codex",
		"model":"gpt-5.6-sol",
		"token_breakdown":{
			"input":{"cache_read_tokens":198400,"total_tokens":200955,"uncached_tokens":2555},
			"output":{"non_reasoning_tokens":154,"reasoning_tokens":132,"total_tokens":286},
			"total_tokens":201241
		},
		"tokens":{
			"cache_creation_tokens":0,
			"cache_read_tokens":198400,
			"cached_tokens":198400,
			"input_tokens":200955,
			"output_tokens":286,
			"reasoning_tokens":132,
			"total_tokens":201241
		}
	}`)

	for attempt := 0; attempt < 100; attempt++ {
		normalized, err := normalizeUsage(raw)
		if err != nil {
			t.Fatalf("normalizeUsage() failed: %v", err)
		}
		if normalized.InputTokens != 200955 || normalized.OutputTokens != 286 {
			t.Fatalf("normalized input/output = %d/%d, want 200955/286", normalized.InputTokens, normalized.OutputTokens)
		}
		if normalized.CachedTokens != 198400 || normalized.CacheReadTokens != 198400 || normalized.CacheCreationTokens != 0 {
			t.Fatalf("normalized cache tokens = %d/%d/%d, want 198400/198400/0", normalized.CachedTokens, normalized.CacheReadTokens, normalized.CacheCreationTokens)
		}
		if normalized.ReasoningTokens != 132 || normalized.TotalTokens != 201241 {
			t.Fatalf("normalized reasoning/total = %d/%d, want 132/201241", normalized.ReasoningTokens, normalized.TotalTokens)
		}
	}
}

func TestNormalizeUsageKeepsLegacyTokenFallbacks(t *testing.T) {
	raw := []byte(`{
		"provider":"openai",
		"usage":{
			"prompt_tokens":11,
			"completion_tokens":7,
			"cached_input_tokens":5,
			"reasoning":3,
			"total":18
		}
	}`)

	normalized, err := normalizeUsage(raw)
	if err != nil {
		t.Fatalf("normalizeUsage() failed: %v", err)
	}
	if normalized.InputTokens != 11 || normalized.OutputTokens != 7 || normalized.CachedTokens != 5 {
		t.Fatalf("normalized legacy input/output/cache = %d/%d/%d, want 11/7/5", normalized.InputTokens, normalized.OutputTokens, normalized.CachedTokens)
	}
	if normalized.ReasoningTokens != 3 || normalized.TotalTokens != 18 {
		t.Fatalf("normalized legacy reasoning/total = %d/%d, want 3/18", normalized.ReasoningTokens, normalized.TotalTokens)
	}
}

func TestSaveUsageMessageStoresReasoningEffortAndTTFT(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	raw := `{"api_key":"sk-usage-ttft","provider":"openai","model":"gpt-5.5","request_id":"usage-ttft","reasoning_effort":"xhigh","ttft_ms":710,"input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(context.Background(), []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	if record.ReasoningEffort == nil || *record.ReasoningEffort != "xhigh" {
		t.Fatalf("record reasoning_effort = %#v, want xhigh", record.ReasoningEffort)
	}
	if record.TTFTMS == nil || *record.TTFTMS != 710 {
		t.Fatalf("record ttft_ms = %#v, want 710", record.TTFTMS)
	}

	var reasoningEffort sql.NullString
	var ttftMS sql.NullFloat64
	if err := app.db.QueryRow(`SELECT reasoning_effort, ttft_ms FROM usage_records WHERE id = ?`, record.ID).Scan(&reasoningEffort, &ttftMS); err != nil {
		t.Fatal(err)
	}
	if !reasoningEffort.Valid || reasoningEffort.String != "xhigh" || !ttftMS.Valid || ttftMS.Float64 != 710 {
		t.Fatalf("stored reasoning/ttft = %#v/%#v, want xhigh/710", reasoningEffort, ttftMS)
	}
}

func TestSaveUsageMessageIgnoresZeroTTFT(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	raw := `{"api_key":"sk-usage-ttft-zero","provider":"openai","model":"gpt-5.5","request_id":"usage-ttft-zero","ttft_ms":0,"input_tokens":10,"output_tokens":2}`
	record, created, err := app.saveUsageMessage(context.Background(), []byte(raw))
	if err != nil || !created {
		t.Fatalf("saveUsageMessage created=%v err=%v", created, err)
	}
	if record.TTFTMS != nil {
		t.Fatalf("record ttft_ms = %#v, want nil", record.TTFTMS)
	}

	var ttftMS sql.NullFloat64
	if err := app.db.QueryRow(`SELECT ttft_ms FROM usage_records WHERE id = ?`, record.ID).Scan(&ttftMS); err != nil {
		t.Fatal(err)
	}
	if ttftMS.Valid {
		t.Fatalf("stored ttft_ms = %v, want NULL", ttftMS.Float64)
	}
}

func TestWarmPriceMap(t *testing.T) {
	prices := map[[2]string]ModelPrice{
		{"openai", "gpt-5.2"}: {
			Provider:           "openai",
			Model:              "gpt-5.2",
			InputUSDPerMillion: 0.1,
		},
	}
	provider := "codex"
	model := "gpt-5.2"
	records := []UsageRecord{
		{Provider: &provider, Model: &model},
	}
	warmPriceMap(prices, records)
	key := priceKey(provider, model)
	cached, ok := prices[key]
	if !ok {
		t.Fatalf("expected prices to be warmed for codex/gpt-5.2")
	}
	if cached.InputUSDPerMillion != 0.1 {
		t.Fatalf("cached InputUSDPerMillion = %v, want 0.1", cached.InputUSDPerMillion)
	}
}

func TestUsageOptionsFromRecordsMatchesAdmin(t *testing.T) {
	provider := "openai"
	model := "gpt-5.5"
	endpoint := "/v1/chat/completions"
	desc := "dev-key"
	username := "alice"
	source := "user@gmail.com"
	auth := "oauth"

	records := []UsageRecord{
		{
			Provider:           &provider,
			Model:              &model,
			Endpoint:           &endpoint,
			APIKeyDescription:  &desc,
			UsageUsername:      &username,
			Source:             &source,
			Auth:               &auth,
		},
	}
	userLookup := map[string]userInfo{
		"alice": {ID: 42, Name: "Alice In Wonderland"},
	}
	scope := usageAccessScope{IsAdmin: true}
	options := usageOptionsFromRecords(records, userLookup, scope)

	users, ok := options["users"].([]map[string]any)
	if !ok || len(users) != 1 || users[0]["label"] != "Alice In Wonderland" {
		t.Fatalf("options users = %#v, want 1 user Alice", options["users"])
	}
	providers, ok := options["providers"].([]string)
	if !ok || len(providers) != 1 || providers[0] != "openai" {
		t.Fatalf("options providers = %#v, want [openai]", options["providers"])
	}
	models, ok := options["models"].([]string)
	if !ok || len(models) != 1 || models[0] != "gpt-5.5" {
		t.Fatalf("options models = %#v, want [gpt-5.5]", options["models"])
	}
	sources, ok := options["sources"].([]map[string]string)
	if !ok || len(sources) != 1 || sources[0]["label"] != "user@gmail.com" {
		t.Fatalf("options sources = %#v, want 1 source user@gmail.com", options["sources"])
	}
}
