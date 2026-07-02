package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	allocationScopeAuth = "auth"
	allocationScopePool = "pool"

	allocationQuotaRequests = "requests"
	allocationQuotaTokens   = "tokens"
	allocationQuotaUSD      = "usd"

	allocationPeriodDaily   = "daily"
	allocationPeriodMonthly = "monthly"
	allocationPeriodAllTime = "all_time"

	allocationWarningOK       = "ok"
	allocationWarningDisabled = "disabled"
	allocationWarningNear     = "warning"
	allocationWarningOver     = "over_quota"
)

type accountPoolPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type accountPoolMemberPayload struct {
	AuthName string `json:"auth_name"`
	Weight   int    `json:"weight"`
}

type accountPoolMembersPayload struct {
	Members   []accountPoolMemberPayload `json:"members"`
	AuthNames []string                   `json:"auth_names"`
}

type accountAllocationPayload struct {
	UserID     int     `json:"user_id"`
	ScopeType  string  `json:"scope_type"`
	AuthName   *string `json:"auth_name"`
	PoolID     *int    `json:"pool_id"`
	QuotaType  string  `json:"quota_type"`
	QuotaLimit float64 `json:"quota_limit"`
	Period     string  `json:"period"`
	HardLimit  bool    `json:"hard_limit"`
	Enabled    *bool   `json:"enabled"`
	Note       string  `json:"note"`
}

type accountReference struct {
	AuthName    string  `json:"auth_name"`
	Email       *string `json:"email"`
	AccountType *string `json:"account_type"`
	Disabled    bool    `json:"disabled"`
}

type accountPoolMemberResponse struct {
	AuthName  string            `json:"auth_name"`
	Weight    int               `json:"weight"`
	Account   *accountReference `json:"account"`
	CreatedAt time.Time         `json:"created_at"`
}

type accountPoolResponse struct {
	ID          int                         `json:"id"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Members     []accountPoolMemberResponse `json:"members"`
	CreatedAt   time.Time                   `json:"created_at"`
	UpdatedAt   time.Time                   `json:"updated_at"`
}

type userAccountAllocation struct {
	ID         int       `json:"id"`
	UserID     int       `json:"user_id"`
	Username   string    `json:"username"`
	UserLabel  string    `json:"user_label"`
	ScopeType  string    `json:"scope_type"`
	AuthName   *string   `json:"auth_name"`
	PoolID     *int      `json:"pool_id"`
	PoolName   *string   `json:"pool_name"`
	QuotaType  string    `json:"quota_type"`
	QuotaLimit float64   `json:"quota_limit"`
	Period     string    `json:"period"`
	HardLimit  bool      `json:"hard_limit"`
	Enabled    bool      `json:"enabled"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type accountAllocationUserOption struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
}

type accountAllocationUsage struct {
	Allocation       userAccountAllocation `json:"allocation"`
	PeriodKey        string                `json:"period_key"`
	WindowStart      *time.Time            `json:"window_start"`
	WindowEnd        *time.Time            `json:"window_end"`
	Records          int                   `json:"records"`
	FailedRecords    int                   `json:"failed_records"`
	TotalTokens      int                   `json:"total_tokens"`
	EstimatedCostUSD float64               `json:"estimated_cost_usd"`
	UnpricedRecords  int                   `json:"unpriced_records"`
	UsedValue        float64               `json:"used_value"`
	QuotaLimit       float64               `json:"quota_limit"`
	Remaining        float64               `json:"remaining"`
	UsedPercent      float64               `json:"used_percent"`
	OverQuota        bool                  `json:"over_quota"`
	WarningLevel     string                `json:"warning_level"`
	LastAlertAt      *time.Time            `json:"last_alert_at"`
	MatchedAuthNames []string              `json:"matched_auth_names"`
}

type accountAllocationsOverview struct {
	Accounts    []accountReference            `json:"accounts"`
	Pools       []accountPoolResponse         `json:"pools"`
	Allocations []userAccountAllocation       `json:"allocations"`
	Usage       []accountAllocationUsage      `json:"usage"`
	Users       []accountAllocationUserOption `json:"users"`
	Enforcement map[string]string             `json:"enforcement"`
}

func (a *App) handleAccountAllocations(w http.ResponseWriter, r *http.Request) error {
	if _, err := a.adminUser(r.Context(), r); err != nil {
		return err
	}
	if r.URL.Path == "/api/account-allocations" || r.URL.Path == "/api/account-allocations/" {
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.accountAllocationsOverview(w, r)
	}
	parts := splitPath(r.URL.Path, "/api/account-allocations/")
	if len(parts) == 0 {
		return notFoundError("Not Found")
	}
	switch parts[0] {
	case "overview":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.accountAllocationsOverview(w, r)
	case "usage":
		if err := requireMethod(r, http.MethodGet); err != nil {
			return err
		}
		return a.accountAllocationUsageOnly(w, r)
	case "pools":
		return a.handleAccountAllocationPools(w, r, parts)
	case "allocations":
		return a.handleUserAccountAllocations(w, r, parts)
	default:
		return notFoundError("Not Found")
	}
}

func (a *App) accountAllocationsOverview(w http.ResponseWriter, r *http.Request) error {
	accounts, accountByName, accountAliases, err := a.accountAllocationAccounts(r.Context())
	if err != nil {
		return err
	}
	pools, err := a.listAccountPools(r.Context(), accountByName)
	if err != nil {
		return err
	}
	allocations, err := a.listUserAccountAllocations(r.Context())
	if err != nil {
		return err
	}
	users, err := a.accountAllocationUsers(r.Context())
	if err != nil {
		return err
	}
	usage, err := a.computeAccountAllocationUsage(r.Context(), allocations, pools, accountAliases)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, accountAllocationsOverview{
		Accounts:    accounts,
		Pools:       pools,
		Allocations: allocations,
		Usage:       usage,
		Users:       users,
		Enforcement: map[string]string{
			"mode":    "observe_only",
			"message": "Hard limits are stored as policy intent. Current MVP reports alerts; enforcement can later be synced to CLIProxyAPI or handled by CPA-Helper proxying.",
		},
	})
	return nil
}

func (a *App) accountAllocationUsageOnly(w http.ResponseWriter, r *http.Request) error {
	_, _, accountAliases, err := a.accountAllocationAccounts(r.Context())
	if err != nil {
		return err
	}
	pools, err := a.listAccountPools(r.Context(), nil)
	if err != nil {
		return err
	}
	allocations, err := a.listUserAccountAllocations(r.Context())
	if err != nil {
		return err
	}
	usage, err := a.computeAccountAllocationUsage(r.Context(), allocations, pools, accountAliases)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": usage})
	return nil
}

func (a *App) handleAccountAllocationPools(w http.ResponseWriter, r *http.Request, parts []string) error {
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			_, accountByName, _, err := a.accountAllocationAccounts(r.Context())
			if err != nil {
				return err
			}
			pools, err := a.listAccountPools(r.Context(), accountByName)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, pools)
			return nil
		case http.MethodPost:
			var payload accountPoolPayload
			if err := decodeJSON(r, &payload); err != nil {
				return err
			}
			pool, err := a.createAccountPool(r.Context(), payload)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, pool)
			return nil
		default:
			return methodNotAllowed()
		}
	}
	poolID, err := parseIntPath(parts[1])
	if err != nil {
		return err
	}
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodPut:
			var payload accountPoolPayload
			if err := decodeJSON(r, &payload); err != nil {
				return err
			}
			pool, err := a.updateAccountPool(r.Context(), poolID, payload)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, pool)
			return nil
		case http.MethodDelete:
			if err := a.deleteAccountPool(r.Context(), poolID); err != nil {
				return err
			}
			writeNoContent(w)
			return nil
		default:
			return methodNotAllowed()
		}
	}
	if len(parts) == 3 && parts[2] == "members" {
		if err := requireMethod(r, http.MethodPut); err != nil {
			return err
		}
		var payload accountPoolMembersPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		pool, err := a.replaceAccountPoolMembers(r.Context(), poolID, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, pool)
		return nil
	}
	return notFoundError("Not Found")
}

func (a *App) handleUserAccountAllocations(w http.ResponseWriter, r *http.Request, parts []string) error {
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			allocations, err := a.listUserAccountAllocations(r.Context())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, allocations)
			return nil
		case http.MethodPost:
			var payload accountAllocationPayload
			if err := decodeJSON(r, &payload); err != nil {
				return err
			}
			allocation, err := a.createUserAccountAllocation(r.Context(), payload)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, allocation)
			return nil
		default:
			return methodNotAllowed()
		}
	}
	allocationID, err := parseIntPath(parts[1])
	if err != nil {
		return err
	}
	if len(parts) != 2 {
		return notFoundError("Not Found")
	}
	switch r.Method {
	case http.MethodPut:
		var payload accountAllocationPayload
		if err := decodeJSON(r, &payload); err != nil {
			return err
		}
		allocation, err := a.updateUserAccountAllocation(r.Context(), allocationID, payload)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, allocation)
		return nil
	case http.MethodDelete:
		if err := a.deleteUserAccountAllocation(r.Context(), allocationID); err != nil {
			return err
		}
		writeNoContent(w)
		return nil
	default:
		return methodNotAllowed()
	}
}

func (a *App) accountAllocationAccounts(ctx context.Context) ([]accountReference, map[string]accountReference, map[string]map[string]bool, error) {
	keeperAccounts, err := a.listKeeperAccounts(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	accounts := make([]accountReference, 0, len(keeperAccounts))
	byName := map[string]accountReference{}
	aliasSets := map[string]map[string]bool{}
	for _, account := range keeperAccounts {
		ref := accountReference{
			AuthName:    account.Name,
			Email:       account.Email,
			AccountType: account.AccountType,
			Disabled:    account.Disabled,
		}
		accounts = append(accounts, ref)
		byName[account.Name] = ref
		aliasSets[account.Name] = aliasesForAccountReference(ref)
	}
	sort.Slice(accounts, func(i, j int) bool {
		left := strings.ToLower(accounts[i].AuthName)
		right := strings.ToLower(accounts[j].AuthName)
		if accounts[i].Email != nil && accounts[j].Email != nil {
			left = strings.ToLower(*accounts[i].Email)
			right = strings.ToLower(*accounts[j].Email)
		}
		return left < right
	})
	return accounts, byName, aliasSets, nil
}

func (a *App) listAccountPools(ctx context.Context, accountByName map[string]accountReference) ([]accountPoolResponse, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, name, description, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
		FROM account_pools
		ORDER BY name, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pools := []accountPoolResponse{}
	for rows.Next() {
		pool, err := scanAccountPool(rows)
		if err != nil {
			return nil, err
		}
		pool.Members = []accountPoolMemberResponse{}
		pools = append(pools, pool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return pools, nil
	}
	members, err := a.accountPoolMembers(ctx, accountByName)
	if err != nil {
		return nil, err
	}
	for index := range pools {
		pools[index].Members = members[pools[index].ID]
	}
	return pools, nil
}

func scanAccountPool(scanner interface{ Scan(dest ...any) error }) (accountPoolResponse, error) {
	var pool accountPoolResponse
	var createdAt, updatedAt sql.NullString
	if err := scanner.Scan(&pool.ID, &pool.Name, &pool.Description, &createdAt, &updatedAt); err != nil {
		return accountPoolResponse{}, err
	}
	if parsed, ok := parseDBTime(createdAt.String); ok {
		pool.CreatedAt = parsed
	}
	if parsed, ok := parseDBTime(updatedAt.String); ok {
		pool.UpdatedAt = parsed
	}
	return pool, nil
}

func (a *App) accountPoolMembers(ctx context.Context, accountByName map[string]accountReference) (map[int][]accountPoolMemberResponse, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT pool_id, auth_name, weight, CAST(created_at AS TEXT)
		FROM account_pool_members
		ORDER BY pool_id, auth_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int][]accountPoolMemberResponse{}
	for rows.Next() {
		var poolID int
		var member accountPoolMemberResponse
		var createdAt sql.NullString
		if err := rows.Scan(&poolID, &member.AuthName, &member.Weight, &createdAt); err != nil {
			return nil, err
		}
		if parsed, ok := parseDBTime(createdAt.String); ok {
			member.CreatedAt = parsed
		}
		if accountByName != nil {
			if account, ok := accountByName[member.AuthName]; ok {
				member.Account = &account
			}
		}
		result[poolID] = append(result[poolID], member)
	}
	return result, rows.Err()
}

func (a *App) createAccountPool(ctx context.Context, payload accountPoolPayload) (accountPoolResponse, error) {
	name, description, err := normalizeAccountPoolPayload(payload)
	if err != nil {
		return accountPoolResponse{}, err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO account_pools (name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, name, description, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return accountPoolResponse{}, conflictError("account pool name already exists")
		}
		return accountPoolResponse{}, err
	}
	id, _ := result.LastInsertId()
	return a.getAccountPool(ctx, int(id), nil)
}

func (a *App) updateAccountPool(ctx context.Context, id int, payload accountPoolPayload) (accountPoolResponse, error) {
	name, description, err := normalizeAccountPoolPayload(payload)
	if err != nil {
		return accountPoolResponse{}, err
	}
	result, err := a.db.ExecContext(ctx, `
		UPDATE account_pools
		SET name = ?, description = ?, updated_at = ?
		WHERE id = ?
	`, name, description, dbTime(time.Now()), id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return accountPoolResponse{}, conflictError("account pool name already exists")
		}
		return accountPoolResponse{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return accountPoolResponse{}, notFoundError("account pool not found")
	}
	return a.getAccountPool(ctx, id, nil)
}

func (a *App) deleteAccountPool(ctx context.Context, id int) error {
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_account_allocations WHERE pool_id = ?`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return conflictError("account pool is used by allocations")
	}
	result, err := a.db.ExecContext(ctx, `DELETE FROM account_pools WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return notFoundError("account pool not found")
	}
	return nil
}

func (a *App) getAccountPool(ctx context.Context, id int, accountByName map[string]accountReference) (accountPoolResponse, error) {
	row := a.db.QueryRowContext(ctx, `
		SELECT id, name, description, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
		FROM account_pools
		WHERE id = ?
	`, id)
	pool, err := scanAccountPool(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accountPoolResponse{}, notFoundError("account pool not found")
		}
		return accountPoolResponse{}, err
	}
	members, err := a.accountPoolMembers(ctx, accountByName)
	if err != nil {
		return accountPoolResponse{}, err
	}
	pool.Members = members[id]
	if pool.Members == nil {
		pool.Members = []accountPoolMemberResponse{}
	}
	return pool, nil
}

func normalizeAccountPoolPayload(payload accountPoolPayload) (string, string, error) {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return "", "", validationError("account pool name is required")
	}
	if len(name) > 160 {
		return "", "", validationError("account pool name is too long")
	}
	description := strings.TrimSpace(payload.Description)
	if len(description) > 2000 {
		return "", "", validationError("account pool description is too long")
	}
	return name, description, nil
}

func (a *App) replaceAccountPoolMembers(ctx context.Context, poolID int, payload accountPoolMembersPayload) (accountPoolResponse, error) {
	if _, err := a.getAccountPool(ctx, poolID, nil); err != nil {
		return accountPoolResponse{}, err
	}
	members, err := normalizeAccountPoolMembersPayload(payload)
	if err != nil {
		return accountPoolResponse{}, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return accountPoolResponse{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_pool_members WHERE pool_id = ?`, poolID); err != nil {
		return accountPoolResponse{}, err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO account_pool_members (pool_id, auth_name, weight, created_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return accountPoolResponse{}, err
	}
	defer stmt.Close()
	now := dbTime(time.Now())
	for _, member := range members {
		if _, err := stmt.ExecContext(ctx, poolID, member.AuthName, member.Weight, now); err != nil {
			return accountPoolResponse{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_pools SET updated_at = ? WHERE id = ?`, now, poolID); err != nil {
		return accountPoolResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return accountPoolResponse{}, err
	}
	committed = true
	_, accountByName, _, err := a.accountAllocationAccounts(ctx)
	if err != nil {
		return accountPoolResponse{}, err
	}
	return a.getAccountPool(ctx, poolID, accountByName)
}

func normalizeAccountPoolMembersPayload(payload accountPoolMembersPayload) ([]accountPoolMemberPayload, error) {
	if len(payload.AuthNames) > 0 && len(payload.Members) == 0 {
		for _, authName := range payload.AuthNames {
			payload.Members = append(payload.Members, accountPoolMemberPayload{AuthName: authName, Weight: 1})
		}
	}
	seen := map[string]bool{}
	members := make([]accountPoolMemberPayload, 0, len(payload.Members))
	for _, member := range payload.Members {
		authName := strings.TrimSpace(member.AuthName)
		if authName == "" {
			return nil, validationError("account pool member auth_name is required")
		}
		key := normalizedAccountAlias(authName)
		if seen[key] {
			continue
		}
		seen[key] = true
		weight := member.Weight
		if weight <= 0 {
			weight = 1
		}
		if weight > 1000 {
			return nil, validationError("account pool member weight is too large")
		}
		members = append(members, accountPoolMemberPayload{AuthName: authName, Weight: weight})
	}
	return members, nil
}

func (a *App) listUserAccountAllocations(ctx context.Context) ([]userAccountAllocation, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT a.id, a.user_id, COALESCE(u.username, ''), COALESCE(u.nickname, ''),
		       CAST(u.disabled_at AS TEXT), a.scope_type, a.auth_name, a.pool_id, p.name,
		       a.quota_type, a.quota_limit, a.period, a.hard_limit, a.enabled, a.note,
		       CAST(a.created_at AS TEXT), CAST(a.updated_at AS TEXT)
		FROM user_account_allocations a
		LEFT JOIN users u ON u.id = a.user_id
		LEFT JOIN account_pools p ON p.id = a.pool_id
		ORDER BY a.enabled DESC, COALESCE(u.username, ''), a.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allocations := []userAccountAllocation{}
	for rows.Next() {
		allocation, err := scanUserAccountAllocation(rows)
		if err != nil {
			return nil, err
		}
		allocations = append(allocations, allocation)
	}
	return allocations, rows.Err()
}

func (a *App) getUserAccountAllocation(ctx context.Context, id int) (userAccountAllocation, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT a.id, a.user_id, COALESCE(u.username, ''), COALESCE(u.nickname, ''),
		       CAST(u.disabled_at AS TEXT), a.scope_type, a.auth_name, a.pool_id, p.name,
		       a.quota_type, a.quota_limit, a.period, a.hard_limit, a.enabled, a.note,
		       CAST(a.created_at AS TEXT), CAST(a.updated_at AS TEXT)
		FROM user_account_allocations a
		LEFT JOIN users u ON u.id = a.user_id
		LEFT JOIN account_pools p ON p.id = a.pool_id
		WHERE a.id = ?
	`, id)
	if err != nil {
		return userAccountAllocation{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return userAccountAllocation{}, notFoundError("account allocation not found")
	}
	allocation, err := scanUserAccountAllocation(rows)
	if err != nil {
		return userAccountAllocation{}, err
	}
	return allocation, rows.Err()
}

func scanUserAccountAllocation(scanner interface{ Scan(dest ...any) error }) (userAccountAllocation, error) {
	var allocation userAccountAllocation
	var nickname, disabledAt, authName, poolName, createdAt, updatedAt sql.NullString
	var poolID sql.NullInt64
	if err := scanner.Scan(
		&allocation.ID, &allocation.UserID, &allocation.Username, &nickname,
		&disabledAt, &allocation.ScopeType, &authName, &poolID, &poolName,
		&allocation.QuotaType, &allocation.QuotaLimit, &allocation.Period,
		&allocation.HardLimit, &allocation.Enabled, &allocation.Note,
		&createdAt, &updatedAt,
	); err != nil {
		return userAccountAllocation{}, err
	}
	allocation.AuthName = nullableString(authName)
	if poolID.Valid {
		value := int(poolID.Int64)
		allocation.PoolID = &value
	}
	allocation.PoolName = nullableString(poolName)
	allocation.UserLabel = strings.TrimSpace(nickname.String)
	if allocation.UserLabel == "" {
		allocation.UserLabel = strings.TrimSpace(allocation.Username)
	}
	if allocation.UserLabel == "" {
		allocation.UserLabel = "User #" + strconv.Itoa(allocation.UserID)
	}
	if disabledAt.Valid && strings.TrimSpace(disabledAt.String) != "" {
		allocation.UserLabel += " (disabled)"
	}
	if parsed, ok := parseDBTime(createdAt.String); ok {
		allocation.CreatedAt = parsed
	}
	if parsed, ok := parseDBTime(updatedAt.String); ok {
		allocation.UpdatedAt = parsed
	}
	return allocation, nil
}

func (a *App) createUserAccountAllocation(ctx context.Context, payload accountAllocationPayload) (userAccountAllocation, error) {
	normalized, err := a.normalizeAccountAllocationPayload(ctx, payload)
	if err != nil {
		return userAccountAllocation{}, err
	}
	now := dbTime(time.Now())
	result, err := a.db.ExecContext(ctx, `
		INSERT INTO user_account_allocations (
			user_id, scope_type, auth_name, pool_id, quota_type, quota_limit,
			period, hard_limit, enabled, note, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalized.UserID, normalized.ScopeType, nullableStringArg(normalized.AuthName), nullableIntArg(normalized.PoolID), normalized.QuotaType, normalized.QuotaLimit, normalized.Period, normalized.HardLimit, normalized.enabledValue(), normalized.Note, now, now)
	if err != nil {
		return userAccountAllocation{}, err
	}
	id, _ := result.LastInsertId()
	return a.getUserAccountAllocation(ctx, int(id))
}

func (a *App) updateUserAccountAllocation(ctx context.Context, id int, payload accountAllocationPayload) (userAccountAllocation, error) {
	if _, err := a.getUserAccountAllocation(ctx, id); err != nil {
		return userAccountAllocation{}, err
	}
	normalized, err := a.normalizeAccountAllocationPayload(ctx, payload)
	if err != nil {
		return userAccountAllocation{}, err
	}
	result, err := a.db.ExecContext(ctx, `
		UPDATE user_account_allocations
		SET user_id = ?, scope_type = ?, auth_name = ?, pool_id = ?, quota_type = ?,
		    quota_limit = ?, period = ?, hard_limit = ?, enabled = ?, note = ?, updated_at = ?
		WHERE id = ?
	`, normalized.UserID, normalized.ScopeType, nullableStringArg(normalized.AuthName), nullableIntArg(normalized.PoolID), normalized.QuotaType, normalized.QuotaLimit, normalized.Period, normalized.HardLimit, normalized.enabledValue(), normalized.Note, dbTime(time.Now()), id)
	if err != nil {
		return userAccountAllocation{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return userAccountAllocation{}, notFoundError("account allocation not found")
	}
	return a.getUserAccountAllocation(ctx, id)
}

func (a *App) deleteUserAccountAllocation(ctx context.Context, id int) error {
	result, err := a.db.ExecContext(ctx, `DELETE FROM user_account_allocations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return notFoundError("account allocation not found")
	}
	return nil
}

func (payload accountAllocationPayload) enabledValue() bool {
	if payload.Enabled == nil {
		return true
	}
	return *payload.Enabled
}

func (a *App) normalizeAccountAllocationPayload(ctx context.Context, payload accountAllocationPayload) (accountAllocationPayload, error) {
	if payload.UserID <= 0 {
		return payload, validationError("user_id is required")
	}
	if _, err := a.getUser(ctx, payload.UserID); err != nil {
		return payload, err
	}
	scopeType := strings.TrimSpace(payload.ScopeType)
	if scopeType != allocationScopeAuth && scopeType != allocationScopePool {
		return payload, validationError("scope_type must be auth or pool")
	}
	quotaType := strings.TrimSpace(payload.QuotaType)
	if quotaType != allocationQuotaRequests && quotaType != allocationQuotaTokens && quotaType != allocationQuotaUSD {
		return payload, validationError("quota_type must be requests, tokens, or usd")
	}
	period := strings.TrimSpace(payload.Period)
	if period != allocationPeriodDaily && period != allocationPeriodMonthly && period != allocationPeriodAllTime {
		return payload, validationError("period must be daily, monthly, or all_time")
	}
	if math.IsNaN(payload.QuotaLimit) || math.IsInf(payload.QuotaLimit, 0) || payload.QuotaLimit <= 0 {
		return payload, validationError("quota_limit must be greater than 0")
	}
	payload.QuotaLimit = mathRound(payload.QuotaLimit, 8)
	payload.ScopeType = scopeType
	payload.QuotaType = quotaType
	payload.Period = period
	payload.Note = strings.TrimSpace(payload.Note)
	if len(payload.Note) > 2000 {
		return payload, validationError("allocation note is too long")
	}
	if scopeType == allocationScopeAuth {
		if payload.AuthName == nil || strings.TrimSpace(*payload.AuthName) == "" {
			return payload, validationError("auth_name is required for auth allocation")
		}
		authName := strings.TrimSpace(*payload.AuthName)
		payload.AuthName = &authName
		payload.PoolID = nil
		return payload, nil
	}
	if payload.PoolID == nil || *payload.PoolID <= 0 {
		return payload, validationError("pool_id is required for pool allocation")
	}
	if _, err := a.getAccountPool(ctx, *payload.PoolID, nil); err != nil {
		return payload, err
	}
	payload.AuthName = nil
	return payload, nil
}

func (a *App) accountAllocationUsers(ctx context.Context) ([]accountAllocationUserOption, error) {
	users, err := a.allUsers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]accountAllocationUserOption, 0, len(users))
	for _, user := range users {
		result = append(result, accountAllocationUserOption{
			ID:       user.ID,
			Username: user.Username,
			Label:    displayUserName(user),
			Disabled: user.DisabledAt != nil,
		})
	}
	return result, nil
}

func (a *App) computeAccountAllocationUsage(ctx context.Context, allocations []userAccountAllocation, pools []accountPoolResponse, accountAliases map[string]map[string]bool) ([]accountAllocationUsage, error) {
	prices, err := a.priceMap(ctx)
	if err != nil {
		return nil, err
	}
	poolMembers := map[int][]accountPoolMemberResponse{}
	for _, pool := range pools {
		poolMembers[pool.ID] = pool.Members
	}
	result := make([]accountAllocationUsage, 0, len(allocations))
	for _, allocation := range allocations {
		usage, err := a.computeSingleAccountAllocationUsage(ctx, allocation, poolMembers, accountAliases, prices)
		if err != nil {
			return nil, err
		}
		result = append(result, usage)
	}
	sort.Slice(result, func(i, j int) bool {
		leftLevel := allocationWarningRank(result[i].WarningLevel)
		rightLevel := allocationWarningRank(result[j].WarningLevel)
		if leftLevel != rightLevel {
			return leftLevel > rightLevel
		}
		return result[i].UsedPercent > result[j].UsedPercent
	})
	return result, nil
}

func (a *App) computeSingleAccountAllocationUsage(ctx context.Context, allocation userAccountAllocation, poolMembers map[int][]accountPoolMemberResponse, accountAliases map[string]map[string]bool, prices map[[2]string]ModelPrice) (accountAllocationUsage, error) {
	windowStart, windowEnd, periodKey := allocationPeriodWindow(allocation.Period, time.Now())
	filters := UsageFilters{
		UsageUsername: &allocation.Username,
		Start:         windowStart,
		End:           windowEnd,
	}
	records, err := a.filteredUsageRecords(ctx, filters, "")
	if err != nil {
		return accountAllocationUsage{}, err
	}
	aliases, authNames := allocationTargetAliases(allocation, poolMembers, accountAliases)
	matched := make([]UsageRecord, 0, len(records))
	for _, record := range records {
		if usageRecordMatchesAccountAliases(record, aliases) {
			matched = append(matched, record)
		}
	}
	usage := accountAllocationUsage{
		Allocation:       allocation,
		PeriodKey:        periodKey,
		WindowStart:      windowStart,
		WindowEnd:        windowEnd,
		QuotaLimit:       allocation.QuotaLimit,
		MatchedAuthNames: authNames,
	}
	for _, record := range matched {
		usage.Records++
		if record.Failed {
			usage.FailedRecords++
		}
		usage.TotalTokens += usageAggregateTotalTokens(record)
		amount, unpriced := recordCost(record, prices)
		usage.EstimatedCostUSD = mathRound(usage.EstimatedCostUSD+amount, 8)
		if unpriced {
			usage.UnpricedRecords++
		}
	}
	switch allocation.QuotaType {
	case allocationQuotaRequests:
		usage.UsedValue = float64(usage.Records)
	case allocationQuotaTokens:
		usage.UsedValue = float64(usage.TotalTokens)
	default:
		usage.UsedValue = usage.EstimatedCostUSD
	}
	usage.UsedValue = mathRound(usage.UsedValue, 8)
	usage.Remaining = mathRound(math.Max(allocation.QuotaLimit-usage.UsedValue, 0), 8)
	usage.UsedPercent = mathRound((usage.UsedValue/allocation.QuotaLimit)*100, 2)
	usage.OverQuota = usage.UsedValue >= allocation.QuotaLimit
	usage.WarningLevel = allocationWarningLevel(allocation.Enabled, usage.UsedPercent, usage.OverQuota)
	if allocation.Enabled && (usage.WarningLevel == allocationWarningNear || usage.WarningLevel == allocationWarningOver) {
		alertAt, err := a.touchAllocationAlert(ctx, allocation.ID, periodKey, usage.WarningLevel)
		if err != nil {
			return accountAllocationUsage{}, err
		}
		usage.LastAlertAt = alertAt
	} else {
		alertAt, err := a.latestAllocationAlert(ctx, allocation.ID, periodKey)
		if err != nil {
			return accountAllocationUsage{}, err
		}
		usage.LastAlertAt = alertAt
	}
	return usage, nil
}

func allocationPeriodWindow(period string, now time.Time) (*time.Time, *time.Time, string) {
	localNow := now.In(appTimeLocation)
	switch period {
	case allocationPeriodDaily:
		start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, appTimeLocation)
		end := start.Add(24 * time.Hour)
		return &start, &end, start.Format("2006-01-02")
	case allocationPeriodMonthly:
		start := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, appTimeLocation)
		end := start.AddDate(0, 1, 0)
		return &start, &end, start.Format("2006-01")
	default:
		return nil, nil, "all"
	}
}

func allocationTargetAliases(allocation userAccountAllocation, poolMembers map[int][]accountPoolMemberResponse, accountAliases map[string]map[string]bool) (map[string]bool, []string) {
	aliases := map[string]bool{}
	authNames := []string{}
	addAuthName := func(authName string) {
		authName = strings.TrimSpace(authName)
		if authName == "" {
			return
		}
		if !stringInSlice(authNames, authName) {
			authNames = append(authNames, authName)
		}
		addAlias(aliases, authName)
		for alias := range accountAliases[authName] {
			aliases[alias] = true
		}
	}
	if allocation.ScopeType == allocationScopeAuth {
		if allocation.AuthName != nil {
			addAuthName(*allocation.AuthName)
		}
	} else if allocation.PoolID != nil {
		for _, member := range poolMembers[*allocation.PoolID] {
			addAuthName(member.AuthName)
		}
	}
	sort.Strings(authNames)
	return aliases, authNames
}

func aliasesForAccountReference(account accountReference) map[string]bool {
	aliases := map[string]bool{}
	addAlias(aliases, account.AuthName)
	if account.Email != nil {
		addAlias(aliases, *account.Email)
	}
	return aliases
}

func addAlias(aliases map[string]bool, value string) {
	normalized := normalizedAccountAlias(value)
	if normalized != "" {
		aliases[normalized] = true
	}
}

func normalizedAccountAlias(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func usageRecordMatchesAccountAliases(record UsageRecord, aliases map[string]bool) bool {
	if len(aliases) == 0 {
		return false
	}
	for _, candidate := range usageRecordAccountCandidates(record) {
		if aliases[normalizedAccountAlias(candidate)] {
			return true
		}
	}
	return false
}

func usageRecordAccountCandidates(record UsageRecord) []string {
	candidates := []string{}
	add := func(value *string) {
		if value != nil && strings.TrimSpace(*value) != "" {
			candidates = append(candidates, *value)
		}
	}
	add(record.AuthIndex)
	add(record.SourceAccount)
	add(record.Source)
	var parsed any
	if json.Unmarshal([]byte(record.RawJSON), &parsed) == nil {
		for _, key := range []string{"auth_name", "authName", "auth_index", "authIndex", "index", "account_id", "accountId", "email", "account_email", "accountEmail", "user_email", "userEmail", "source"} {
			add(toString(findFirst(parsed, key)))
		}
	}
	return candidates
}

func allocationWarningLevel(enabled bool, usedPercent float64, overQuota bool) string {
	if !enabled {
		return allocationWarningDisabled
	}
	if overQuota {
		return allocationWarningOver
	}
	if usedPercent >= 80 {
		return allocationWarningNear
	}
	return allocationWarningOK
}

func allocationWarningRank(level string) int {
	switch level {
	case allocationWarningOver:
		return 3
	case allocationWarningNear:
		return 2
	case allocationWarningDisabled:
		return 0
	default:
		return 1
	}
}

func (a *App) touchAllocationAlert(ctx context.Context, allocationID int, periodKey, level string) (*time.Time, error) {
	now := dbTime(time.Now())
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO allocation_alert_states (
			allocation_id, period_key, level, first_triggered_at, last_triggered_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(allocation_id, period_key, level) DO UPDATE SET
			last_triggered_at = excluded.last_triggered_at
	`, allocationID, periodKey, level, now, now)
	if err != nil {
		return nil, err
	}
	parsed, _ := parseDBTime(now)
	return &parsed, nil
}

func (a *App) latestAllocationAlert(ctx context.Context, allocationID int, periodKey string) (*time.Time, error) {
	var lastTriggered sql.NullString
	err := a.db.QueryRowContext(ctx, `
		SELECT CAST(MAX(last_triggered_at) AS TEXT)
		FROM allocation_alert_states
		WHERE allocation_id = ? AND period_key = ?
	`, allocationID, periodKey).Scan(&lastTriggered)
	if err != nil {
		return nil, err
	}
	return timePtr(lastTriggered), nil
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringInSlice(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
