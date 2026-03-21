package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadConfig reads and parses a YAML configuration file.
// Environment variable references like ${VAR} in string values are expanded.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	if cfg.Logging.RequestDir == "" {
		cfg.Logging.RequestDir = "logs/requests"
	}

	return &cfg, nil
}

// Config is the top-level runtime configuration for nanoapi.
type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Logging   LoggingConfig    `yaml:"logging"`
	Storage   StorageConfig    `yaml:"storage"`
	Tokens    []TokenConfig    `yaml:"tokens"`
	Providers []ProviderConfig `yaml:"providers"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	MaxBodyBytes   int64  `yaml:"max_body_bytes"`
}

// LoggingConfig controls runtime logging behavior.
type LoggingConfig struct {
	Debug      bool   `yaml:"debug"`
	RequestDir string `yaml:"request_dir"`
}

type StorageConfig struct {
	Path string `yaml:"path"`
}

// TokenConfig declares one static API token loaded from config.
type TokenConfig struct {
	ID  string `yaml:"id"`
	Key string `yaml:"key"`
}

// ProviderConfig describes one upstream AI provider (always OpenAI-compatible).
type ProviderConfig struct {
	Name        string            `yaml:"name"`
	BaseURL     string            `yaml:"base_url"`
	APIKey      string            `yaml:"api_key"`
	Priority    int               `yaml:"priority"`
	Headers     map[string]string `yaml:"headers"`
	ForceStream bool              `yaml:"force_stream"`

	Models   map[string]ModelTargetConfig `yaml:"models"`
	Override ProviderOverride             `yaml:"override"`
}

// ModelTargetConfig describes how one client-facing model maps to an upstream model.
type ModelTargetConfig struct {
	Upstream  string               `yaml:"upstream"`
	Reasoning *ReasoningCapability `yaml:"reasoning,omitempty"`
}

// ReasoningCapability declares reasoning support for an upstream model.
type ReasoningCapability struct {
	AllowedEfforts []string          `yaml:"allowed_efforts"`
	EffortMap      map[string]string `yaml:"effort_map,omitempty"`
}

// OverrideParams holds request parameter overrides.
type OverrideParams struct {
	MaxTokens       *int     `yaml:"max_tokens"`
	Temperature     *float64 `yaml:"temperature"`
	TopP            *float64 `yaml:"top_p"`
	Stop            []string `yaml:"stop"`
	ReasoningEffort *string  `yaml:"reasoning_effort"`
}

// OverrideTarget declares which requests a rule applies to.
type OverrideTarget struct {
	ClientModel *string `yaml:"client_model"`
	Stream      *bool   `yaml:"stream"`
}

// OverrideRule applies parameter overrides when target conditions match.
type OverrideRule struct {
	Target OverrideTarget `yaml:"target"`
	Params OverrideParams `yaml:"params"`
}

// ProviderOverride holds provider-level parameter overrides.
type ProviderOverride struct {
	Defaults *OverrideParams `yaml:"defaults"`
	Rules    []OverrideRule  `yaml:"rules"`
}
