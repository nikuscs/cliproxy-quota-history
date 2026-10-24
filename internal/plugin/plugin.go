// Package plugin implements the CPA plugin RPC methods. Package main forwards
// every native call to Handle.
//
// Adapted from Lamplighter (https://github.com/PosvdM/cpa-plugin-lamplighter,
// AGPL-3.0), keeping only quota polling and its history.
package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nikuscs/cliproxy-quota-history/internal/config"
	"github.com/nikuscs/cliproxy-quota-history/internal/host"
	"github.com/nikuscs/cliproxy-quota-history/internal/poller"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	repository  = "https://github.com/nikuscs/cliproxy-quota-history"
	apiBase     = "/v0/management/" + config.PluginID
	stopTimeout = 10 * time.Second
)

// Plugin handles the RPC methods of one loaded plugin instance.
type Plugin struct {
	version string
	poller  *poller.Poller
}

// New creates the plugin. dataDir is the default history directory;
// hostConfigPath is the CPA config file.
func New(h host.Host, version, dataDir, hostConfigPath string) *Plugin {
	return &Plugin{version: version, poller: poller.New(h, hostConfigPath, dataDir)}
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ok(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

// ErrorEnvelope encodes a failed RPC result.
func ErrorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

// Handle runs one RPC method and returns the response envelope. A panic is
// turned into an error envelope so that it cannot crash CPA.
func (p *Plugin) Handle(method string, request []byte) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			out = ErrorEnvelope("plugin_panic", fmt.Sprint(r))
		}
	}()
	result, err := p.handle(method, request)
	if err != nil {
		return ErrorEnvelope("plugin_error", err.Error())
	}
	return result
}

// Shutdown stops polling.
func (p *Plugin) Shutdown() {
	p.poller.Stop(stopTimeout)
}

func (p *Plugin) handle(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var req struct {
			ConfigYAML []byte `json:"config_yaml"`
		}
		if len(request) > 0 {
			if err := json.Unmarshal(request, &req); err != nil {
				return nil, fmt.Errorf("decode lifecycle request: %w", err)
			}
		}
		cfg, _ := config.Parse(req.ConfigYAML)
		p.poller.Configure(cfg)
		p.poller.Start()
		return ok(p.registration())
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		p.poller.Stop(stopTimeout)
		return ok(struct{}{})
	case pluginabi.MethodManagementRegister:
		return ok(managementRegistration())
	case pluginabi.MethodManagementHandle:
		var req pluginapi.ManagementRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, fmt.Errorf("decode management request: %w", err)
		}
		return ok(p.handleManagement(req))
	}
	return ErrorEnvelope("unknown_method", "unknown method: "+method), nil
}

type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  map[string]bool    `json:"capabilities"`
}

func (p *Plugin) registration() registration {
	field := func(name string, kind pluginapi.ConfigFieldType, description string) pluginapi.ConfigField {
		return pluginapi.ConfigField{Name: name, Type: kind, Description: description}
	}
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "Quota History",
			Version:          p.version,
			Author:           "nikuscs",
			GitHubRepository: repository,
			ConfigFields: []pluginapi.ConfigField{
				field("poll_interval_seconds", pluginapi.ConfigFieldTypeInteger, "How often to read every credential's quota, in seconds (default 300, minimum 60)"),
				field("retention_days", pluginapi.ConfigFieldTypeInteger, "Days of readings to keep (default 30)"),
				field("data_dir", pluginapi.ConfigFieldTypeString, "History directory, default plugins/data/quota-history"),
			},
		},
		Capabilities: map[string]bool{"management_api": true},
	}
}

type route struct {
	Method      string
	Path        string
	Description string
}

func managementRegistration() map[string]any {
	return map[string]any{
		"routes": []route{
			{Method: http.MethodGet, Path: apiBase + "/status", Description: "Quota history poller status"},
			{Method: http.MethodGet, Path: apiBase + "/readings", Description: "Stored quota readings since a time (?since=<unix ms>)"},
		},
	}
}

func jsonResponse(status int, value any) pluginapi.ManagementResponse {
	raw, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		raw = []byte(`{"error":"encode response failed"}`)
	}
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}, "Cache-Control": []string{"no-store"}},
		Body:       raw,
	}
}

func (p *Plugin) handleManagement(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	path := strings.TrimRight(req.Path, "/")
	switch strings.ToUpper(req.Method) + " " + path {
	case "GET " + apiBase + "/status":
		return jsonResponse(http.StatusOK, p.poller.Status())
	case "GET " + apiBase + "/readings":
		since, _ := strconv.ParseInt(req.Query.Get("since"), 10, 64)
		readings, err := p.poller.Since(since)
		if err != nil {
			return jsonResponse(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return jsonResponse(http.StatusOK, map[string]any{"readings": readings})
	}
	return jsonResponse(http.StatusNotFound, map[string]string{"error": "unknown route " + req.Method + " " + req.Path})
}
