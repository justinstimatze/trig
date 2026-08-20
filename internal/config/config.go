// Package config loads trig's personal API keys and per-user PostHog
// settings. Env vars win; a local JSON config file is the fallback — see
// DESIGN.md's "Auth" section for why this is a personal-key CLI and not an
// OAuth app.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds everything trig needs to call PostHog and Linear on this
// user's behalf.
type Config struct {
	PostHogAPIKey    string `json:"posthog_api_key"`
	PostHogHost      string `json:"posthog_host"`
	PostHogProjectID string `json:"posthog_project_id"`
	LinearAPIKey     string `json:"linear_api_key"`
}

// defaultHost is PostHog Cloud's default region — what a new account gets
// unless it opted into EU hosting. Overridable; never assume a specific
// project's deployment (DESIGN.md: "not something trig should hardcode").
const defaultHost = "us.posthog.com"

// Load reads TRIG_POSTHOG_API_KEY / TRIG_POSTHOG_HOST / TRIG_POSTHOG_PROJECT_ID
// / TRIG_LINEAR_API_KEY from the environment, falling back to
// $XDG_CONFIG_HOME/trig/config.json (or its OS equivalent) for whichever of
// those is unset.
func Load() (*Config, error) {
	cfg := &Config{
		PostHogAPIKey:    os.Getenv("TRIG_POSTHOG_API_KEY"),
		PostHogHost:      os.Getenv("TRIG_POSTHOG_HOST"),
		PostHogProjectID: os.Getenv("TRIG_POSTHOG_PROJECT_ID"),
		LinearAPIKey:     os.Getenv("TRIG_LINEAR_API_KEY"),
	}

	if cfg.PostHogAPIKey == "" || cfg.PostHogHost == "" || cfg.PostHogProjectID == "" || cfg.LinearAPIKey == "" {
		if file, err := loadFile(); err == nil {
			if cfg.PostHogAPIKey == "" {
				cfg.PostHogAPIKey = file.PostHogAPIKey
			}
			if cfg.PostHogHost == "" {
				cfg.PostHogHost = file.PostHogHost
			}
			if cfg.PostHogProjectID == "" {
				cfg.PostHogProjectID = file.PostHogProjectID
			}
			if cfg.LinearAPIKey == "" {
				cfg.LinearAPIKey = file.LinearAPIKey
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	if cfg.PostHogHost == "" {
		cfg.PostHogHost = defaultHost
	}
	if cfg.PostHogAPIKey == "" {
		return nil, fmt.Errorf("no PostHog API key: set TRIG_POSTHOG_API_KEY or posthog_api_key in %s", configPath())
	}
	if cfg.PostHogProjectID == "" {
		return nil, fmt.Errorf("no PostHog project ID: set TRIG_POSTHOG_PROJECT_ID or posthog_project_id in %s", configPath())
	}
	if cfg.LinearAPIKey == "" {
		return nil, fmt.Errorf("no Linear API key: set TRIG_LINEAR_API_KEY or linear_api_key in %s", configPath())
	}
	return cfg, nil
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "trig", "config.json")
}

func loadFile() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath(), err)
	}
	return &cfg, nil
}
