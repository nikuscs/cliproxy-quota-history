// Package config holds the plugin settings from plugins.configs.quota-history.
package config

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// PluginID is the CPA plugin ID and the key under plugins.configs. CPA takes
// it from the library file name, such as quota-history-v0.1.0.dylib.
const PluginID = "quota-history"

// Config is the plugin configuration.
type Config struct {
	// PollIntervalSeconds is how often every credential's quota is read.
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	// RetentionDays is how long readings are kept.
	RetentionDays int `yaml:"retention_days"`
	// DataDir overrides the history directory.
	DataDir string `yaml:"data_dir"`
}

// Defaults: a reading every 5 minutes, kept for 30 days like the panel.
const (
	DefaultPollIntervalSeconds = 300
	MinPollIntervalSeconds     = 60
	DefaultRetentionDays       = 30
)

// Parse reads the plugin config YAML and applies the defaults.
func Parse(raw []byte) (Config, error) {
	cfg := Config{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return defaults(Config{}), err
		}
	}
	return defaults(cfg), nil
}

func defaults(cfg Config) Config {
	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = DefaultPollIntervalSeconds
	}
	if cfg.PollIntervalSeconds < MinPollIntervalSeconds {
		cfg.PollIntervalSeconds = MinPollIntervalSeconds
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = DefaultRetentionDays
	}
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	return cfg
}
