package providers

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Codex endpoint and headers, the same as the panel's quota page.
const (
	codexUsageURL  = "https://chatgpt.com/backend-api/wham/usage"
	codexUserAgent = "codex-tui/0.149.1 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.149.1)"
)

const (
	fiveHourSeconds = 18000
	weekSeconds     = 604800
	minMonthSeconds = 28 * 24 * 3600
	maxMonthSeconds = 31 * 24 * 3600
)

func fetchCodex(ctx context.Context, doer Doer, cred Credential, now time.Time) ([]Window, error) {
	header := bearer(cred)
	header.Set("User-Agent", codexUserAgent)
	if cred.AccountID != "" {
		header.Set("Chatgpt-Account-Id", cred.AccountID)
	}
	payload, err := send(ctx, doer, cred, Request{Method: http.MethodGet, URL: codexUsageURL, Header: header})
	if err != nil {
		return nil, err
	}
	return parseCodexUsage(payload, now)
}

// parseCodexUsage reads the main rate limit of a wham/usage response into the
// panel's `five-hour` and `weekly` (or `monthly`) windows. Windows are told
// apart by their length; without one, primary is the 5-hour window and
// secondary the longer one.
func parseCodexUsage(payload map[string]any, now time.Time) ([]Window, error) {
	rateLimit, _ := field(payload, "rate_limit", "rateLimit").(map[string]any)
	if rateLimit == nil {
		return nil, errors.New("codex returned no rate_limit")
	}
	primary, _ := field(rateLimit, "primary_window", "primaryWindow").(map[string]any)
	secondary, _ := field(rateLimit, "secondary_window", "secondaryWindow").(map[string]any)
	limitReached, _ := field(rateLimit, "limit_reached", "limitReached").(bool)
	if allowed, ok := rateLimit["allowed"].(bool); ok && !allowed {
		limitReached = true
	}

	// Indexes into candidates; -1 when the window is missing.
	candidates := []map[string]any{primary, secondary}
	fiveHour, longer := -1, -1
	for i, window := range candidates {
		seconds := windowSeconds(window)
		switch {
		case window == nil:
		case seconds == fiveHourSeconds && fiveHour < 0:
			fiveHour = i
		case (seconds == weekSeconds || isMonth(seconds)) && longer < 0:
			longer = i
		}
	}
	if fiveHour < 0 && primary != nil && longer != 0 {
		fiveHour = 0
	}
	if longer < 0 && secondary != nil && fiveHour != 1 {
		longer = 1
	}
	pick := func(index int) map[string]any {
		if index < 0 {
			return nil
		}
		return candidates[index]
	}

	var windows []Window
	if w, ok := codexWindow(pick(fiveHour), "five-hour", limitReached, now); ok {
		windows = append(windows, w)
	}
	longerID := "weekly"
	if isMonth(windowSeconds(pick(longer))) {
		longerID = "monthly"
	}
	if w, ok := codexWindow(pick(longer), longerID, limitReached, now); ok {
		windows = append(windows, w)
	}
	if len(windows) == 0 {
		return nil, errors.New("codex returned no known quota window")
	}
	return windows, nil
}

func windowSeconds(window map[string]any) float64 {
	seconds, _ := floatValue(field(window, "limit_window_seconds", "limitWindowSeconds"))
	return seconds
}

func isMonth(seconds float64) bool {
	return seconds >= minMonthSeconds && seconds <= maxMonthSeconds
}

func codexWindow(window map[string]any, id string, limitReached bool, now time.Time) (Window, bool) {
	if window == nil {
		return Window{}, false
	}
	reset := parseTime(field(window, "reset_at", "resetAt"))
	if reset.IsZero() {
		if after, ok := floatValue(field(window, "reset_after_seconds", "resetAfterSeconds")); ok {
			reset = now.Add(time.Duration(after * float64(time.Second))).UTC()
		}
	}
	used, ok := floatValue(field(window, "used_percent", "usedPercent"))
	if !ok {
		if !limitReached || reset.IsZero() {
			return Window{}, false
		}
		used = 100
	}
	return Window{
		ID:          id,
		Remaining:   clampPercent(100 - used),
		ResetAt:     reset,
		PeriodHours: windowSeconds(window) / 3600,
	}, true
}
