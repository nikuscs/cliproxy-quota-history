package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// xAI (Grok CLI) endpoint and headers, the same as the panel's quota page.
const (
	xaiBillingWeeklyURL = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	xaiClientVersion    = "1.0.46"
	xaiUserAgent        = "grok-pager/" + xaiClientVersion + " grok-shell/" + xaiClientVersion + " (macos; aarch64)"
)

func fetchXai(ctx context.Context, doer Doer, cred Credential, _ time.Time) ([]Window, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+cred.AccessToken)
	header.Set("x-xai-token-auth", "xai-grok-cli")
	header.Set("x-grok-client-version", xaiClientVersion)
	header.Set("accept", "*/*")
	header.Set("user-agent", xaiUserAgent)
	if cred.UserID != "" {
		header.Set("x-userid", cred.UserID)
	}
	payload, err := send(ctx, doer, cred, Request{Method: http.MethodGet, URL: xaiBillingWeeklyURL, Header: header})
	if err != nil {
		return nil, err
	}
	return parseXaiBilling(payload)
}

// parseXaiBilling reads the weekly credit window of a billing response. Like
// the panel, only a weekly period counts: a monthly figure is a spend cap
// rolling over, not quota coming back. The panel files this window as
// `window`, the ID of a provider's single timeline window.
func parseXaiBilling(payload map[string]any) ([]Window, error) {
	config, _ := payload["config"].(map[string]any)
	if config == nil {
		return nil, errors.New("xai returned no billing config")
	}
	period, _ := field(config, "currentPeriod", "current_period").(map[string]any)
	kind := strings.ToLower(scalarString(period["type"]))
	used, hasUsage := floatValue(field(config, "creditUsagePercent", "credit_usage_percent"))
	products, _ := field(config, "productUsage", "product_usage").([]any)
	weekly := strings.Contains(kind, "weekly") || (kind == "" && (hasUsage || len(products) > 0))
	if !weekly || !hasUsage {
		return nil, errors.New("xai returned no weekly credit usage")
	}
	start := parseTime(field(period, "start"))
	reset := parseTime(field(period, "end"))
	if reset.IsZero() {
		return nil, errors.New("xai returned no weekly reset time")
	}
	periodHours := 168.0
	if !start.IsZero() && reset.After(start) {
		periodHours = reset.Sub(start).Hours()
	}
	return []Window{{ID: "window", Remaining: clampPercent(100 - used), ResetAt: reset, PeriodHours: periodHours}}, nil
}
