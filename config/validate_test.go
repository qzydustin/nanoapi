package config

import (
	"strings"
	"testing"
)

func intPtr(i int) *int { return &i }

func validConfig() *Config {
	mode := "enabled"
	budget := 4096
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Logging: LoggingConfig{
			RequestDir: "logs/requests",
		},
		Storage: StorageConfig{
			Driver: "sqlite",
			DSN:    "./test.db",
		},
		Tokens: []TokenConfig{
			{ID: "tok_default", Key: "nk_test_token"},
		},
		Providers: []ProviderConfig{
			{
				Name:     "p1",
				Protocol: "openai_chat",
				BaseURL:  "https://api.openai.com",
				APIKey:   "openai-test-key",
				Priority: 100,
				Models:   map[string]ModelTargetConfig{"gpt-4o": {Upstream: "gpt-4o"}},
			},
			{
				Name:     "p2",
				Protocol: "anthropic_messages",
				BaseURL:  "https://api.anthropic.com",
				APIKey:   "anthropic-test-key",
				Priority: 90,
				Models:   map[string]ModelTargetConfig{"gpt-4o": {Upstream: "claude-3-7-sonnet-20250219"}},
				Override: ProviderOverride{
					Defaults: &OverrideParams{
						Reasoning: &ReasoningOverride{
							Mode:         &mode,
							BudgetTokens: &budget,
						},
					},
				},
			},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := Validate(validConfig()); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_ServerErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		substr string
	}{
		{"empty host", func(c *Config) { c.Server.Host = "" }, "server.host"},
		{"bad port", func(c *Config) { c.Server.Port = 0 }, "server.port"},
		{"empty request dir", func(c *Config) { c.Logging.RequestDir = " " }, "logging.request_dir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Errorf("error %q should contain %q", err, tt.substr)
			}
		})
	}
}

func TestValidate_StorageErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		substr string
	}{
		{"empty driver", func(c *Config) { c.Storage.Driver = "" }, "storage.driver"},
		{"unsupported driver", func(c *Config) { c.Storage.Driver = "postgres" }, "storage.driver"},
		{"empty dsn", func(c *Config) { c.Storage.DSN = "" }, "storage.dsn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Errorf("error %q should contain %q", err, tt.substr)
			}
		})
	}
}

func TestValidate_ProviderErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		substr string
	}{
		{"no providers", func(c *Config) { c.Providers = nil }, "at least one provider"},
		{"empty name", func(c *Config) { c.Providers[0].Name = "" }, "name must not be empty"},
		{"bad protocol", func(c *Config) { c.Providers[0].Protocol = "grpc" }, "protocol must be"},
		{"bad search_mode", func(c *Config) { c.Providers[0].SearchMode = "custom" }, "search_mode must be"},
		{"empty base_url", func(c *Config) { c.Providers[0].BaseURL = "" }, "base_url"},
		{"missing api key", func(c *Config) { c.Providers[0].APIKey = "" }, "api_key must not be empty"},
		{"empty models", func(c *Config) { c.Providers[0].Models = nil }, "models must not be empty"},
		{"empty model value", func(c *Config) {
			c.Providers[0].Models = map[string]ModelTargetConfig{"gpt-4o": {Upstream: "  "}}
		}, "model value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Errorf("error %q should contain %q", err, tt.substr)
			}
		})
	}
}

func TestValidate_TokenErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		substr string
	}{
		{"no tokens", func(c *Config) { c.Tokens = nil }, "at least one token"},
		{"empty token id", func(c *Config) { c.Tokens[0].ID = "" }, "id must not be empty"},
		{"empty token key", func(c *Config) { c.Tokens[0].Key = "" }, "key must not be empty"},
		{"duplicate token id", func(c *Config) {
			c.Tokens = append(c.Tokens, TokenConfig{ID: "tok_default", Key: "nk_other"})
		}, "duplicate id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Errorf("error %q should contain %q", err, tt.substr)
			}
		})
	}
}

func TestValidate_PriorityAmbiguity(t *testing.T) {
	cfg := validConfig()
	// Both providers serve "gpt-4o" — set same priority.
	cfg.Providers[0].Priority = 100
	cfg.Providers[1].Priority = 100

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous priority") {
		t.Errorf("error %q should contain 'ambiguous priority'", err)
	}
}

func TestValidate_ReasoningContradiction(t *testing.T) {
	cfg := validConfig()
	disabled := "disabled"
	budget := 4096
	cfg.Providers[0].Override.Defaults = &OverrideParams{
		Reasoning: &ReasoningOverride{
			Mode:         &disabled,
			BudgetTokens: &budget,
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected reasoning contradiction error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error %q should mention disabled", err)
	}
}

func TestValidate_OverrideRuleEmptyTarget(t *testing.T) {
	cfg := validConfig()
	cfg.Providers[0].Override.Rules = []OverrideRule{
		{
			Params: OverrideParams{
				MaxTokens: intPtr(1024),
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected empty target error")
	}
	if !strings.Contains(err.Error(), "target must include at least one condition") {
		t.Errorf("error %q should mention target conditions", err)
	}
}

func TestValidate_OverrideRuleEmptyClientModel(t *testing.T) {
	cfg := validConfig()
	empty := " "
	cfg.Providers[0].Override.Rules = []OverrideRule{
		{
			Target: OverrideTarget{ClientModel: &empty},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected empty client_model error")
	}
	if !strings.Contains(err.Error(), "target.client_model") {
		t.Errorf("error %q should mention target.client_model", err)
	}
}

func TestValidate_OverrideRuleReasoningContradiction(t *testing.T) {
	cfg := validConfig()
	model := "gpt-4o"
	disabled := "disabled"
	budget := 1024
	cfg.Providers[0].Override.Rules = []OverrideRule{
		{
			Target: OverrideTarget{ClientModel: &model},
			Params: OverrideParams{
				Reasoning: &ReasoningOverride{
					Mode:         &disabled,
					BudgetTokens: &budget,
				},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected reasoning contradiction error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error %q should mention disabled", err)
	}
}
