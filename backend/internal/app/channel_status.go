package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type channelStatusResponse struct {
	Items       []channelStatusItem `json:"items"`
	RefreshedAt string              `json:"refreshed_at"`
}

type channelStatusItem struct {
	ID                     string                          `json:"id"`
	Name                   string                          `json:"name"`
	Email                  *string                         `json:"email"`
	AccountType            *string                         `json:"account_type"`
	Disabled               bool                            `json:"disabled"`
	Priority               *int                            `json:"priority"`
	Status                 string                          `json:"status"`
	StatusLabel            string                          `json:"status_label"`
	StatusDetail           string                          `json:"status_detail"`
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
}

func (a *App) handleChannelStatus(w http.ResponseWriter, r *http.Request) error {
	if err := requireMethod(r, http.MethodGet); err != nil {
		return err
	}
	if _, err := a.readyUser(r.Context(), r); err != nil {
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
	writeJSON(w, http.StatusOK, channelStatusResponse{
		Items:       channelStatusItems(accounts, windowUsages),
		RefreshedAt: apiDateTime(time.Now().In(appTimeLocation)),
	})
	return nil
}

func channelStatusItems(accounts []keeperAccount, windowUsages map[string]keeperQuotaWindowUsagePair) []channelStatusItem {
	items := make([]channelStatusItem, 0, len(accounts))
	for index, account := range accounts {
		usage := windowUsages[account.Name]
		items = append(items, channelStatusItemFrom(index, account, usage))
	}
	return items
}

func channelStatusItemFrom(index int, account keeperAccount, usage keeperQuotaWindowUsagePair) channelStatusItem {
	status, label, detail := channelStatus(account)
	lastError := channelStatusRedactedString(account.LastError, account)
	latestAction := channelStatusRedactedString(account.LatestAction, account)
	return channelStatusItem{
		ID:                     "channel-" + strconv.Itoa(index+1),
		Name:                   "Channel " + strconv.Itoa(index+1),
		Email:                  channelStatusMaskedEmail(account.Email),
		AccountType:            account.AccountType,
		Disabled:               account.Disabled,
		Priority:               keeperDisplayPriority(account.Priority),
		Status:                 status,
		StatusLabel:            label,
		StatusDetail:           channelStatusRedactText(detail, account),
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
		LastError:              lastError,
		LatestAction:           latestAction,
		LastCheckedAt:          apiDateTimePtr(account.LastCheckedAt),
		LastHealthyAt:          apiDateTimePtr(account.LastHealthyAt),
	}
}

func channelStatus(account keeperAccount) (string, string, string) {
	if account.Disabled {
		return "disabled", "Disabled", "This channel is currently disabled."
	}
	if account.LastStatusCode != nil {
		switch *account.LastStatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "unauthorized", "Needs login", "The last check reported an authorization problem."
		}
	}
	if channelStatusQuotaExhausted(account) {
		return "quota_exhausted", "Quota exhausted", "The available quota window appears to be exhausted."
	}
	if account.LastError != nil && strings.TrimSpace(*account.LastError) != "" {
		return "error", "Error", *account.LastError
	}
	if account.LastStatusCode != nil && *account.LastStatusCode >= 400 {
		return "error", "Error", "The last check returned HTTP " + strconv.Itoa(*account.LastStatusCode) + "."
	}
	if account.LastHealthyAt != nil {
		return "healthy", "Healthy", "The channel passed its latest health check."
	}
	if account.LastCheckedAt != nil {
		return "checked", "Checked", "The channel has been checked, but no successful health timestamp is available yet."
	}
	return "unknown", "Unknown", "The channel has not been checked yet."
}

func channelStatusQuotaExhausted(account keeperAccount) bool {
	primaryExhausted := account.PrimaryUsedPercent != nil && *account.PrimaryUsedPercent >= 100
	secondaryKnown := account.SecondaryUsedPercent != nil
	secondaryExhausted := secondaryKnown && *account.SecondaryUsedPercent >= 100
	return primaryExhausted && (!secondaryKnown || secondaryExhausted)
}

func channelStatusMaskedEmail(email *string) *string {
	if email == nil {
		return nil
	}
	value := strings.TrimSpace(*email)
	if value == "" {
		return nil
	}
	at := strings.Index(value, "@")
	if at <= 0 || at == len(value)-1 {
		masked := "***"
		return &masked
	}
	localInitial := firstRuneString(value[:at])
	domainInitial := firstRuneString(value[at+1:])
	masked := localInitial + "***@" + domainInitial + "***"
	return &masked
}

func channelStatusRedactedString(value *string, account keeperAccount) *string {
	if value == nil {
		return nil
	}
	redacted := channelStatusRedactText(*value, account)
	if strings.TrimSpace(redacted) == "" {
		return nil
	}
	return &redacted
}

func channelStatusRedactText(text string, account keeperAccount) string {
	redacted := text
	if strings.TrimSpace(account.Name) != "" {
		redacted = strings.ReplaceAll(redacted, account.Name, "[channel]")
	}
	if account.Email != nil && strings.TrimSpace(*account.Email) != "" {
		redacted = strings.ReplaceAll(redacted, *account.Email, "[email]")
	}
	return redacted
}

func firstRuneString(value string) string {
	for _, r := range strings.TrimSpace(value) {
		return string(r)
	}
	return "*"
}
