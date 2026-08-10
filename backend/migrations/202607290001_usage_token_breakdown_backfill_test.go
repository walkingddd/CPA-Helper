package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	_ "modernc.org/sqlite"
)

func TestUsageTokenBreakdownBackfillRepairsOnlyProvenZeroValues(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "usage-backfill.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE usage_records (
			id INTEGER PRIMARY KEY,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			dedupe_key TEXT NOT NULL,
			raw_json TEXT NOT NULL
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE user_quota_charges (
			id INTEGER PRIMARY KEY,
			usage_record_id INTEGER NOT NULL,
			amount_usd REAL NOT NULL
		)
	`); err != nil {
		t.Fatal(err)
	}

	consistent := `{"token_breakdown":{"input":{"total_tokens":200955},"output":{"total_tokens":286},"total_tokens":201241},"tokens":{"input_tokens":200955,"output_tokens":286,"total_tokens":201241}}`
	actualZero := `{"token_breakdown":{"input":{"total_tokens":0},"output":{"total_tokens":0},"total_tokens":0},"tokens":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`
	inconsistent := `{"token_breakdown":{"input":{"total_tokens":200000},"output":{"total_tokens":286},"total_tokens":200286},"tokens":{"input_tokens":200955,"output_tokens":286,"total_tokens":201241}}`
	missingField := `{"token_breakdown":{"input":{"total_tokens":21},"output":{"total_tokens":9},"total_tokens":30},"tokens":{"input_tokens":21,"total_tokens":30}}`
	negative := `{"token_breakdown":{"input":{"total_tokens":-1},"output":{"total_tokens":9},"total_tokens":8},"tokens":{"input_tokens":-1,"output_tokens":9,"total_tokens":8}}`
	legacy := `{"tokens":{"input_tokens":21,"output_tokens":9,"total_tokens":30}}`
	fixtures := []struct {
		id           int
		inputTokens  int
		outputTokens int
		totalTokens  int
		rawJSON      string
	}{
		{id: 1, inputTokens: 0, outputTokens: 0, totalTokens: 201241, rawJSON: consistent},
		{id: 2, inputTokens: 0, outputTokens: 286, totalTokens: 201241, rawJSON: consistent},
		{id: 3, inputTokens: 200955, outputTokens: 0, totalTokens: 201241, rawJSON: consistent},
		{id: 4, inputTokens: 200955, outputTokens: 286, totalTokens: 201241, rawJSON: consistent},
		{id: 5, inputTokens: 0, outputTokens: 0, totalTokens: 0, rawJSON: actualZero},
		{id: 6, inputTokens: 0, outputTokens: 0, totalTokens: 7, rawJSON: `{invalid`},
		{id: 7, inputTokens: 0, outputTokens: 0, totalTokens: 201241, rawJSON: inconsistent},
		{id: 8, inputTokens: 0, outputTokens: 0, totalTokens: 30, rawJSON: legacy},
		{id: 9, inputTokens: 0, outputTokens: 0, totalTokens: 30, rawJSON: missingField},
		{id: 10, inputTokens: 0, outputTokens: 0, totalTokens: 8, rawJSON: negative},
	}
	for _, fixture := range fixtures {
		if _, err := db.Exec(`
			INSERT INTO usage_records (id, input_tokens, output_tokens, total_tokens, dedupe_key, raw_json)
			VALUES (?, ?, ?, ?, ?, ?)
		`, fixture.id, fixture.inputTokens, fixture.outputTokens, fixture.totalTokens, fixture.id, fixture.rawJSON); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO user_quota_charges (id, usage_record_id, amount_usd) VALUES (1, 1, 12.34)`); err != nil {
		t.Fatal(err)
	}

	if err := upUsageTokenBreakdownBackfill(context.Background(), db); err != nil {
		t.Fatalf("upUsageTokenBreakdownBackfill() failed: %v", err)
	}
	if err := upUsageTokenBreakdownBackfill(context.Background(), db); err != nil {
		t.Fatalf("second upUsageTokenBreakdownBackfill() failed: %v", err)
	}

	wants := map[int][3]int{
		1:  {200955, 286, 201241},
		2:  {200955, 286, 201241},
		3:  {200955, 286, 201241},
		4:  {200955, 286, 201241},
		5:  {0, 0, 0},
		6:  {0, 0, 7},
		7:  {0, 0, 201241},
		8:  {0, 0, 30},
		9:  {0, 0, 30},
		10: {0, 0, 8},
	}
	rows, err := db.Query(`SELECT id, input_tokens, output_tokens, total_tokens, dedupe_key, raw_json FROM usage_records ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, inputTokens, outputTokens, totalTokens int
		var dedupeKey, rawJSON string
		if err := rows.Scan(&id, &inputTokens, &outputTokens, &totalTokens, &dedupeKey, &rawJSON); err != nil {
			t.Fatal(err)
		}
		want := wants[id]
		if inputTokens != want[0] || outputTokens != want[1] || totalTokens != want[2] {
			t.Errorf("row %d tokens = %d/%d/%d, want %d/%d/%d", id, inputTokens, outputTokens, totalTokens, want[0], want[1], want[2])
		}
		if dedupeKey != strconv.Itoa(id) {
			t.Errorf("row %d dedupe_key = %q, want %q", id, dedupeKey, strconv.Itoa(id))
		}
		if rawJSON != fixtures[id-1].rawJSON {
			t.Errorf("row %d raw_json changed", id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var amount float64
	if err := db.QueryRow(`SELECT amount_usd FROM user_quota_charges WHERE id = 1`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 12.34 {
		t.Fatalf("quota charge amount = %v, want 12.34", amount)
	}
}
