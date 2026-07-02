package app

import (
	"context"
	"testing"
	"time"
)

func TestAccountAllocationUsageAttributesAuthAndPool(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	now := dbTime(time.Now())
	userID := insertAccountAllocationTestUser(t, app, "alice", "Alice")
	apiKey := "sk-account-allocation"
	if err := app.upsertUserAPIKey(ctx, userID, hashAPIKey(apiKey), apiKey, "alice-key"); err != nil {
		t.Fatalf("upsert api key: %v", err)
	}
	if _, err := app.db.ExecContext(ctx, `
		INSERT INTO codex_keeper_auth_states (
			auth_name, email, account_type, disabled, created_at, updated_at
		) VALUES
			('auth-alpha', 'alpha@example.com', 'pro', 0, ?, ?),
			('auth-beta', 'beta@example.com', 'plus', 0, ?, ?)
	`, now, now, now, now); err != nil {
		t.Fatalf("insert keeper auth states: %v", err)
	}

	pool, err := app.createAccountPool(ctx, accountPoolPayload{Name: "Pro pool"})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	pool, err = app.replaceAccountPoolMembers(ctx, pool.ID, accountPoolMembersPayload{
		AuthNames: []string{"auth-alpha", "auth-beta"},
	})
	if err != nil {
		t.Fatalf("replace pool members: %v", err)
	}
	if len(pool.Members) != 2 {
		t.Fatalf("pool members = %d, want 2", len(pool.Members))
	}

	enabled := true
	authName := "auth-alpha"
	authAllocation, err := app.createUserAccountAllocation(ctx, accountAllocationPayload{
		UserID:     userID,
		ScopeType:  allocationScopeAuth,
		AuthName:   &authName,
		QuotaType:  allocationQuotaTokens,
		QuotaLimit: 100,
		Period:     allocationPeriodAllTime,
		HardLimit:  true,
		Enabled:    &enabled,
	})
	if err != nil {
		t.Fatalf("create auth allocation: %v", err)
	}
	poolAllocation, err := app.createUserAccountAllocation(ctx, accountAllocationPayload{
		UserID:     userID,
		ScopeType:  allocationScopePool,
		PoolID:     &pool.ID,
		QuotaType:  allocationQuotaRequests,
		QuotaLimit: 1,
		Period:     allocationPeriodAllTime,
		Enabled:    &enabled,
	})
	if err != nil {
		t.Fatalf("create pool allocation: %v", err)
	}

	raw := `{"api_key":"` + apiKey + `","provider":"openai","model":"gpt-test","auth_index":"alpha@example.com","input_tokens":50,"output_tokens":70,"total_tokens":120}`
	if _, created, err := app.saveUsageMessage(ctx, []byte(raw)); err != nil || !created {
		t.Fatalf("save usage created=%v err=%v", created, err)
	}

	_, _, aliases, err := app.accountAllocationAccounts(ctx)
	if err != nil {
		t.Fatalf("account aliases: %v", err)
	}
	allocations, err := app.listUserAccountAllocations(ctx)
	if err != nil {
		t.Fatalf("list allocations: %v", err)
	}
	pools, err := app.listAccountPools(ctx, nil)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	usage, err := app.computeAccountAllocationUsage(ctx, allocations, pools, aliases)
	if err != nil {
		t.Fatalf("compute usage: %v", err)
	}

	authUsage := findAllocationUsage(t, usage, authAllocation.ID)
	if authUsage.UsedValue != 120 || !authUsage.OverQuota || authUsage.WarningLevel != allocationWarningOver {
		t.Fatalf("auth usage = used %.2f over %v level %q, want 120 over_quota", authUsage.UsedValue, authUsage.OverQuota, authUsage.WarningLevel)
	}
	if authUsage.LastAlertAt == nil {
		t.Fatal("auth usage LastAlertAt is nil, want alert timestamp")
	}
	poolUsage := findAllocationUsage(t, usage, poolAllocation.ID)
	if poolUsage.UsedValue != 1 || !poolUsage.OverQuota || len(poolUsage.MatchedAuthNames) != 2 {
		t.Fatalf("pool usage = used %.2f over %v matched %#v, want 1 over two auths", poolUsage.UsedValue, poolUsage.OverQuota, poolUsage.MatchedAuthNames)
	}

	var alertCount int
	if err := app.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM allocation_alert_states
		WHERE allocation_id = ? AND period_key = 'all' AND level = ?
	`, authAllocation.ID, allocationWarningOver).Scan(&alertCount); err != nil {
		t.Fatalf("query alerts: %v", err)
	}
	if alertCount != 1 {
		t.Fatalf("alert count = %d, want 1", alertCount)
	}
}

func insertAccountAllocationTestUser(t *testing.T, app *App, username, nickname string) int {
	t.Helper()
	now := dbTime(time.Now())
	result, err := app.db.Exec(`
		INSERT INTO users (username, is_admin, nickname, created_at, updated_at)
		VALUES (?, 0, ?, ?, ?)
	`, username, nickname, now, now)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func findAllocationUsage(t *testing.T, items []accountAllocationUsage, allocationID int) accountAllocationUsage {
	t.Helper()
	for _, item := range items {
		if item.Allocation.ID == allocationID {
			return item
		}
	}
	t.Fatalf("allocation usage %d not found in %#v", allocationID, items)
	return accountAllocationUsage{}
}
