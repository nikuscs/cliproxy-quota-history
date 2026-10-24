package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Claude endpoint and headers, the same as the panel's quota page.
const (
	claudeUsageURL  = "https://api.anthropic.com/api/oauth/usage"
	claudeUserAgent = "claude-cli/2.1.280 (external, cli)"
)

// claudeWindows maps oauth/usage keys to the panel's window IDs. Claude states
// no period, so it comes from the key: five_hour is 5 hours, the rest a week.
var claudeWindows = []struct {
	key, id     string
	periodHours float64
}{
	{"five_hour", "five-hour", 5},
	{"seven_day", "seven-day", 168},
	{"seven_day_oauth_apps", "seven-day-oauth-apps", 168},
	{"seven_day_opus", "seven-day-opus", 168},
	{"seven_day_sonnet", "seven-day-sonnet", 168},
	{"seven_day_cowork", "seven-day-cowork", 168},
	{"iguana_necktie", "seven-day-fable", 168},
}

func fetchClaude(ctx context.Context, doer Doer, cred Credential, _ time.Time) ([]Window, error) {
	header := bearer(cred)
	header.Set("User-Agent", claudeUserAgent)
	header.Set("anthropic-beta", "oauth-2025-04-20")
	payload, err := send(ctx, doer, cred, Request{Method: http.MethodGet, URL: claudeUsageURL, Header: header})
	if err != nil {
		return nil, err
	}
	return parseClaudeUsage(payload)
}

// parseClaudeUsage reads the windows of an oauth/usage response. The Fable
// window comes from the `limits` list when present, like on the panel; a
// dollar-denominated iguana_necktie is a credit pool, not a window.
func parseClaudeUsage(payload map[string]any) ([]Window, error) {
	fable := findFableLimit(payload)
	var windows []Window
	for _, spec := range claudeWindows {
		item, _ := payload[spec.key].(map[string]any)
		if item == nil {
			continue
		}
		if spec.key == "iguana_necktie" && (fable != nil || isDollarWindow(item)) {
			continue
		}
		used, ok := floatValue(item["utilization"])
		if !ok {
			continue
		}
		windows = append(windows, Window{
			ID:          spec.id,
			Remaining:   clampPercent(100 - used),
			ResetAt:     parseTime(item["resets_at"]),
			PeriodHours: spec.periodHours,
		})
	}
	if fable != nil {
		if used, ok := floatValue(fable["percent"]); ok {
			windows = append(windows, Window{
				ID:          "seven-day-fable",
				Remaining:   clampPercent(100 - used),
				ResetAt:     parseTime(fable["resets_at"]),
				PeriodHours: 168,
			})
		}
	}
	if len(windows) == 0 {
		return nil, errors.New("claude returned no known quota window")
	}
	return windows, nil
}

func isDollarWindow(item map[string]any) bool {
	return field(item, "limit_dollars", "used_dollars", "remaining_dollars") != nil
}

func findFableLimit(payload map[string]any) map[string]any {
	limits, _ := payload["limits"].([]any)
	var candidates []map[string]any
	for _, raw := range limits {
		item, _ := raw.(map[string]any)
		if item == nil || item["percent"] == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(item["kind"])))
		scope, _ := item["scope"].(map[string]any)
		model, _ := scope["model"].(map[string]any)
		name := strings.ToLower(scalarString(model["display_name"]))
		if kind == "weekly_scoped" && (name == "fable" || name == "fable 5") {
			candidates = append(candidates, item)
		}
	}
	for _, item := range candidates {
		if active, _ := item["is_active"].(bool); active {
			return item
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}
