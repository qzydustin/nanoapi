package config

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleYAML = `
server:
  host: 0.0.0.0
  port: 8080

logging:
  debug: false
  request_dir: ./logs/requests

storage:
  driver: sqlite
  dsn: ./test.db

tokens:
  - id: default
    name: default
    key: nk_test_token

providers:
  - name: anthropic-main
    protocol: anthropic_messages
    base_url: https://api.anthropic.com
    api_key: anthropic-test-key
    priority: 100
    headers:
      anthropic-version: "2023-06-01"
    force_stream: true
    models:
      gpt-4o-mini:
        upstream: claude-3-7-sonnet-20250219
    override:
      defaults:
        reasoning:
          mode: enabled
          budget_tokens: 4096

  - name: openai-main
    protocol: openai_chat
    base_url: https://api.openai.com
    api_key: openai-test-key
    priority: 90
    models:
      gpt-4o-mini:
        upstream: gpt-4o-mini
        reasoning:
          allowed_efforts:
            - low
            - medium
            - high
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig(t *testing.T) {
	path := writeTempConfig(t, sampleYAML)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("server.host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d, want %d", cfg.Server.Port, 8080)
	}
	if cfg.Logging.RequestDir != "./logs/requests" {
		t.Errorf("logging.request_dir = %q, want %q", cfg.Logging.RequestDir, "./logs/requests")
	}
	if cfg.Storage.Driver != "sqlite" {
		t.Errorf("storage.driver = %q, want %q", cfg.Storage.Driver, "sqlite")
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(cfg.Providers))
	}

	p := cfg.Providers[0]
	if p.Name != "anthropic-main" {
		t.Errorf("providers[0].name = %q", p.Name)
	}
	if p.Protocol != "anthropic_messages" {
		t.Errorf("providers[0].protocol = %q", p.Protocol)
	}
	if p.APIKey != "anthropic-test-key" {
		t.Errorf("providers[0].api_key = %q", p.APIKey)
	}
	if !p.ForceStream {
		t.Error("providers[0].force_stream should be true")
	}
	if v, ok := p.Models["gpt-4o-mini"]; !ok || v.Upstream != "claude-3-7-sonnet-20250219" {
		t.Errorf("providers[0].models[gpt-4o-mini].upstream = %q", v.Upstream)
	}
	if p.Override.Defaults == nil || p.Override.Defaults.Reasoning == nil {
		t.Fatal("providers[0].override.defaults.reasoning should not be nil")
	}
	if p.Override.Defaults.Reasoning.BudgetTokens == nil || *p.Override.Defaults.Reasoning.BudgetTokens != 4096 {
		t.Errorf("providers[0].override.defaults.reasoning.budget_tokens = %v", p.Override.Defaults.Reasoning.BudgetTokens)
	}
	openaiProvider := cfg.Providers[1]
	target, ok := openaiProvider.Models["gpt-4o-mini"]
	if !ok || target.Reasoning == nil {
		t.Fatal("providers[1].models[gpt-4o-mini].reasoning should exist")
	}
	if len(target.Reasoning.AllowedEfforts) != 3 {
		t.Fatalf("allowed_efforts count = %d, want 3", len(target.Reasoning.AllowedEfforts))
	}
	if target.Reasoning.AllowedEfforts[2] != "high" {
		t.Errorf("allowed_efforts[2] = %q, want %q", target.Reasoning.AllowedEfforts[2], "high")
	}
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	yaml := `
server:
  host: 0.0.0.0
  port: 8080
storage:
  driver: sqlite
  dsn: ./test.db
tokens:
  - id: default
    name: default
    key: ${TEST_TOKEN_KEY}
providers:
  - name: p1
    protocol: openai_chat
    base_url: https://api.openai.com
    api_key: ${TEST_PROVIDER_KEY}
    models:
      gpt-4o:
        upstream: gpt-4o
`
	t.Setenv("TEST_TOKEN_KEY", "token-from-env")
	t.Setenv("TEST_PROVIDER_KEY", "provider-key-from-env")
	path := writeTempConfig(t, yaml)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Tokens[0].Key != "token-from-env" {
		t.Errorf("token key = %q, want %q", cfg.Tokens[0].Key, "token-from-env")
	}
	if cfg.Providers[0].APIKey != "provider-key-from-env" {
		t.Errorf("api_key = %q, want %q", cfg.Providers[0].APIKey, "provider-key-from-env")
	}
	if cfg.Logging.RequestDir != "logs/requests" {
		t.Errorf("logging.request_dir default = %q, want %q", cfg.Logging.RequestDir, "logs/requests")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
