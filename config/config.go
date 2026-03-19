package config

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
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// LoggingConfig controls runtime logging behavior.
type LoggingConfig struct {
	Debug      bool   `yaml:"debug"`
	RequestDir string `yaml:"request_dir"`
}

// StorageConfig holds persistence backend settings.
type StorageConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// TokenConfig declares one static API token loaded from config.
type TokenConfig struct {
	ID  string `yaml:"id"`
	Key string `yaml:"key"`
}

// ProviderConfig describes one upstream AI provider.
type ProviderConfig struct {
	Name        string            `yaml:"name"`
	Protocol    string            `yaml:"protocol"`
	BaseURL     string            `yaml:"base_url"`
	APIKey      string            `yaml:"api_key"`
	Priority    int               `yaml:"priority"`
	Headers     map[string]string `yaml:"headers"`
	ForceStream bool              `yaml:"force_stream"`

	Models   map[string]ModelTargetConfig `yaml:"models"`
	Override ProviderOverride             `yaml:"override"`
}

// ModelTargetConfig describes how one client-facing model maps to a specific
// upstream model and what reasoning features that upstream target supports.
//
// Example:
//
//	models:
//	  claude-opus-4-6:
//	    upstream: bedrock-claude-4-6-opus
//	    reasoning:
//	      allowed_efforts: [low, medium, high]
type ModelTargetConfig struct {
	Upstream  string               `yaml:"upstream"`
	Reasoning *ReasoningCapability `yaml:"reasoning,omitempty"`
}

// ReasoningCapability declares protocol-facing reasoning support for one
// upstream target model. Values here are target-model capabilities, not user intent.
type ReasoningCapability struct {
	AllowedEfforts []string `yaml:"allowed_efforts"`
}

// OverrideParams holds typed request parameter overrides.
// These change request parameters only, never model selection.
type OverrideParams struct {
	MaxTokens   *int               `yaml:"max_tokens"`
	Temperature *float64           `yaml:"temperature"`
	TopP        *float64           `yaml:"top_p"`
	Stop        []string           `yaml:"stop"`
	Reasoning   *ReasoningOverride `yaml:"reasoning"`
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
// Only parameters — never model selection.
type ProviderOverride struct {
	Defaults *OverrideParams `yaml:"defaults"`
	Rules    []OverrideRule  `yaml:"rules"`
}

// ReasoningOverride holds provider-level thinking/reasoning overrides.
type ReasoningOverride struct {
	Mode         *string `yaml:"mode"`
	Effort       *string `yaml:"effort"`
	BudgetTokens *int    `yaml:"budget_tokens"`
}
