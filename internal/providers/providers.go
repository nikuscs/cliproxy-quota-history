// Package providers reads the quota windows of one credential. The URLs,
// headers and window IDs match the CPA panel's quota page, so the readings
// line up with the history the panel records itself.
//
// Request handling and payload parsing are adapted from Lamplighter
// (https://github.com/PosvdM/cpa-plugin-lamplighter, AGPL-3.0).
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Request is one upstream HTTP request.
type Request struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

// Response is one upstream HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Doer sends an upstream request. proxyURL is the credential's own proxy and
// is empty when the credential has none.
type Doer interface {
	Do(ctx context.Context, proxyURL string, req Request) (Response, error)
}

// ErrUnauthorized means the upstream rejected the access token. CPA refreshes
// tokens in the background, so the caller retries on the next round.
var ErrUnauthorized = errors.New("upstream returned 401, the access token may have just expired")

// Window is one quota window. Remaining is a percentage from 0 to 100.
type Window struct {
	ID          string
	Remaining   float64
	ResetAt     time.Time
	PeriodHours float64
}

// Fetcher reads the windows of one credential.
type Fetcher func(ctx context.Context, doer Doer, cred Credential, now time.Time) ([]Window, error)

var fetchers = map[string]Fetcher{
	"claude": fetchClaude,
	"codex":  fetchCodex,
	"xai":    fetchXai,
}

// Lookup returns the fetcher for a CPA provider key, or nil when the plugin
// does not read that provider.
func Lookup(provider string) Fetcher {
	return fetchers[strings.ToLower(strings.TrimSpace(provider))]
}

const maxErrorBodyLength = 300

func send(ctx context.Context, doer Doer, cred Credential, req Request) (map[string]any, error) {
	resp, err := doer.Do(ctx, cred.ProxyURL, req)
	if err != nil {
		return nil, err
	}
	if resp.Status == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.Status < 200 || resp.Status >= 300 {
		body := strings.TrimSpace(string(resp.Body))
		if len(body) > maxErrorBodyLength {
			body = body[:maxErrorBodyLength]
		}
		return nil, fmt.Errorf("upstream HTTP %d: %s", resp.Status, body)
	}
	decoder := json.NewDecoder(bytes.NewReader(resp.Body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("upstream response is not a JSON object: %w", err)
	}
	return payload, nil
}

func bearer(cred Credential) http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+cred.AccessToken)
	header.Set("Content-Type", "application/json")
	return header
}

// clampPercent limits value to 0–100 and drops float noise.
func clampPercent(value float64) float64 {
	value = math.Round(value*1e6) / 1e6
	return math.Min(100, math.Max(0, value))
}

func floatValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func field(obj map[string]any, names ...string) any {
	for _, name := range names {
		if value, ok := obj[name]; ok && value != nil {
			return value
		}
	}
	return nil
}

// parseTime reads an ISO 8601 timestamp or Unix seconds or milliseconds.
func parseTime(value any) time.Time {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.UTC()
			}
		}
	}
	seconds, ok := floatValue(value)
	if !ok || seconds <= 0 {
		return time.Time{}
	}
	if seconds > 10_000_000_000 {
		seconds /= 1000
	}
	return time.Unix(0, int64(seconds*1e9)).UTC()
}
