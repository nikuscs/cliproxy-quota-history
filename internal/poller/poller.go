// Package poller reads every credential's quota on a fixed interval and
// stores the readings.
package poller

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nikuscs/cliproxy-quota-history/internal/config"
	"github.com/nikuscs/cliproxy-quota-history/internal/host"
	"github.com/nikuscs/cliproxy-quota-history/internal/providers"
	"github.com/nikuscs/cliproxy-quota-history/internal/store"
)

const (
	requestTimeout = 20 * time.Second
	// startupRetry is the wait when CPA has not loaded its credentials yet.
	startupRetry = 30 * time.Second
)

var errNoToken = errors.New("credential file has no access_token")

// Status is what /status reports.
type Status struct {
	Running             bool              `json:"running"`
	PollIntervalSeconds int               `json:"poll_interval_seconds"`
	RetentionDays       int               `json:"retention_days"`
	LastPollAtMs        int64             `json:"last_poll_at_ms,omitempty"`
	LastReadings        int               `json:"last_readings"`
	Errors              map[string]string `json:"errors"`
}

// Poller owns the polling loop.
type Poller struct {
	host           host.Host
	egress         *providers.Egress
	hostConfigPath string
	defaultDataDir string

	mu     sync.Mutex
	cfg    config.Config
	store  *store.Store
	status Status
	cancel context.CancelFunc
	done   chan struct{}
}

// New creates a stopped poller.
func New(h host.Host, hostConfigPath, defaultDataDir string) *Poller {
	return &Poller{
		host:           h,
		egress:         &providers.Egress{Host: h, Timeout: requestTimeout},
		hostConfigPath: hostConfigPath,
		defaultDataDir: defaultDataDir,
		cfg:            config.Config{PollIntervalSeconds: config.DefaultPollIntervalSeconds, RetentionDays: config.DefaultRetentionDays},
		status:         Status{Errors: map[string]string{}},
	}
}

// Configure applies a new config; the next round uses it.
func (p *Poller) Configure(cfg config.Config) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg = cfg
	dir := cfg.DataDir
	if dir == "" {
		dir = p.defaultDataDir
	}
	if p.store == nil || p.store.Dir != dir {
		p.store = &store.Store{Dir: dir}
	}
}

// Start begins polling; a second call does nothing.
func (p *Poller) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})
	p.status.Running = true
	go p.loop(ctx, p.done)
}

// Stop ends polling and waits up to timeout for the current round.
func (p *Poller) Stop(timeout time.Duration) {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.status.Running = false
	p.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Status returns a copy of the current status.
func (p *Poller) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status
	status.PollIntervalSeconds = p.cfg.PollIntervalSeconds
	status.RetentionDays = p.cfg.RetentionDays
	status.Errors = make(map[string]string, len(p.status.Errors))
	for name, message := range p.status.Errors {
		status.Errors[name] = message
	}
	return status
}

// Since returns the stored readings at or after sinceMs.
func (p *Poller) Since(sinceMs int64) ([]store.Reading, error) {
	p.mu.Lock()
	st := p.store
	p.mu.Unlock()
	if st == nil {
		return []store.Reading{}, nil
	}
	return st.Since(sinceMs)
}

func (p *Poller) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			p.host.Log("error", "quota poller stopped after a panic", map[string]any{"panic": r})
		}
	}()
	for {
		ready := p.round(ctx)
		p.mu.Lock()
		interval := time.Duration(p.cfg.PollIntervalSeconds) * time.Second
		p.mu.Unlock()
		if !ready {
			interval = startupRetry
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// round reads every supported, enabled credential once and stores the
// result. It returns false when CPA has not loaded its credentials yet: right
// after start, host.auth.list falls back to the files on disk, which have no
// auth_index to read them by.
func (p *Poller) round(ctx context.Context) bool {
	if !p.pluginEnabled() {
		return true
	}
	p.mu.Lock()
	st, retention := p.store, time.Duration(p.cfg.RetentionDays)*24*time.Hour
	p.mu.Unlock()
	if st == nil {
		return true
	}

	auths, err := p.host.AuthList()
	if err != nil {
		p.host.Log("warn", "quota poller could not list credentials", map[string]any{"error": err.Error()})
		p.finish(st, time.Now(), 0, map[string]string{"auth.list": err.Error()})
		return false
	}
	for _, auth := range auths {
		if auth.AuthIndex == "" {
			return false
		}
	}
	now := time.Now()
	failures := map[string]string{}
	var readings []store.Reading
	for _, auth := range auths {
		fetch := providers.Lookup(auth.Provider)
		if fetch == nil {
			fetch = providers.Lookup(auth.Type)
		}
		if fetch == nil || auth.Disabled || ctx.Err() != nil {
			continue
		}
		windows, err := p.read(ctx, fetch, auth.AuthIndex, now)
		if err != nil {
			failures[auth.Name] = err.Error()
			continue
		}
		readings = append(readings, toReadings(auth.Name, auth.AuthIndex, providerOf(auth.Provider, auth.Type), windows, now)...)
	}
	if err := st.Append(readings); err != nil {
		p.host.Log("error", "quota poller could not store readings", map[string]any{"error": err.Error()})
	}
	_ = st.Prune(now, retention)
	p.finish(st, now, len(readings), failures)
	return true
}

// finish records the round in the status and in status.json beside the
// readings, so the poller can be checked on the server without the key.
func (p *Poller) finish(st *store.Store, now time.Time, readings int, failures map[string]string) {
	p.mu.Lock()
	p.status.LastPollAtMs = now.UnixMilli()
	p.status.LastReadings = readings
	p.status.Errors = failures
	p.mu.Unlock()
	if raw, err := json.MarshalIndent(p.Status(), "", "  "); err == nil {
		_ = os.MkdirAll(st.Dir, 0o755)
		_ = os.WriteFile(filepath.Join(st.Dir, "status.json"), raw, 0o644)
	}
}

// read fetches one credential, re-reading its file so a refreshed token is used.
func (p *Poller) read(ctx context.Context, fetch providers.Fetcher, authIndex string, now time.Time) ([]providers.Window, error) {
	raw, err := p.host.AuthGet(authIndex)
	if err != nil {
		return nil, err
	}
	cred := providers.ParseCredential(raw)
	if cred.AccessToken == "" {
		return nil, errNoToken
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return fetch(ctx, p.egress, cred, now)
}

func (p *Poller) pluginEnabled() bool {
	raw, err := os.ReadFile(p.hostConfigPath)
	if err != nil {
		// Without a readable config, trust that CPA only calls enabled plugins.
		return true
	}
	enabled, err := config.PluginEnabled(raw)
	return err != nil || enabled
}

func providerOf(provider, kind string) string {
	if providers.Lookup(provider) != nil {
		return provider
	}
	return kind
}

func toReadings(name, authIndex, provider string, windows []providers.Window, now time.Time) []store.Reading {
	out := make([]store.Reading, 0, len(windows))
	for _, window := range windows {
		reading := store.Reading{
			TS:         now.UnixMilli(),
			Credential: name,
			AuthIndex:  authIndex,
			Provider:   provider,
			Window:     window.ID,
			Remaining:  window.Remaining,
		}
		if !window.ResetAt.IsZero() {
			resetAtMs := window.ResetAt.UnixMilli()
			reading.ResetAtMs = &resetAtMs
		}
		if window.PeriodHours > 0 {
			periodHours := window.PeriodHours
			reading.PeriodHours = &periodHours
		}
		out = append(out, reading)
	}
	return out
}
