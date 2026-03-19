package provider

import (
	"testing"

	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/config"
)

func makeProviders() []config.ProviderConfig {
	return []config.ProviderConfig{
		{
			Name:     "anthropic-main",
			Protocol: "anthropic_messages",
			BaseURL:  "https://api.anthropic.com",
			APIKey:   "anthropic-test-key",
			Priority: 100,
			Models: map[string]config.ModelTargetConfig{
				"gpt-4o-mini": {Upstream: "claude-3-7-sonnet-20250219"},
				"gpt-4o":      {Upstream: "claude-3-7-sonnet-20250219"},
			},
			ForceStream: true,
			Override: config.ProviderOverride{
				Defaults: &config.OverrideParams{
					Reasoning: &config.ReasoningOverride{
						Mode:         strPtr("enabled"),
						BudgetTokens: intPtr(4096),
					},
				},
			},
		},
		{
			Name:     "openai-main",
			Protocol: "openai_chat",
			BaseURL:  "https://api.openai.com",
			APIKey:   "openai-test-key",
			Priority: 90,
			Models: map[string]config.ModelTargetConfig{
				"gpt-4o-mini": {Upstream: "gpt-4o-mini"},
				"gpt-5-mini":  {Upstream: "gpt-5-mini"},
			},
		},
	}
}

func strPtr(s string) *string   { return &s }
func intPtr(i int) *int         { return &i }
func f64Ptr(f float64) *float64 { return &f }

func TestSelect_HighestPriority(t *testing.T) {
	sel := NewSelector(makeProviders())
	req := &canonical.CanonicalRequest{ClientModel: "gpt-4o-mini"}

	result, err := sel.Select(req)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Provider.Name != "anthropic-main" {
		t.Errorf("provider = %q, want anthropic-main", result.Provider.Name)
	}
	if result.UpstreamModel != "claude-3-7-sonnet-20250219" {
		t.Errorf("upstream_model = %q", result.UpstreamModel)
	}
	if !result.ForceStream {
		t.Error("force_stream should be true")
	}
}

func TestSelect_OnlyOneProvider(t *testing.T) {
	sel := NewSelector(makeProviders())
	req := &canonical.CanonicalRequest{ClientModel: "gpt-5-mini"}

	result, err := sel.Select(req)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Provider.Name != "openai-main" {
		t.Errorf("provider = %q, want openai-main", result.Provider.Name)
	}
	if result.UpstreamModel != "gpt-5-mini" {
		t.Errorf("upstream_model = %q", result.UpstreamModel)
	}
}

func TestSelect_NoMatch(t *testing.T) {
	sel := NewSelector(makeProviders())
	req := &canonical.CanonicalRequest{ClientModel: "nonexistent-model"}

	_, err := sel.Select(req)
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestSelect_ForceStreamFalse(t *testing.T) {
	sel := NewSelector(makeProviders())
	req := &canonical.CanonicalRequest{ClientModel: "gpt-5-mini"}

	result, err := sel.Select(req)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.ForceStream {
		t.Error("force_stream should be false for openai-main")
	}
}

func TestApplyOverride_AllFields(t *testing.T) {
	params := &canonical.CanonicalParams{
		MaxTokens:   intPtr(2048),
		Temperature: f64Ptr(0.7),
	}
	override := config.OverrideParams{
		MaxTokens:   intPtr(8192),
		Temperature: f64Ptr(0.2),
		TopP:        f64Ptr(0.95),
		Stop:        []string{"</end>"},
		Reasoning: &config.ReasoningOverride{
			Mode:         strPtr("enabled"),
			BudgetTokens: intPtr(4096),
		},
	}

	ApplyOverride(params, override)

	if *params.MaxTokens != 8192 {
		t.Errorf("max_tokens = %d", *params.MaxTokens)
	}
	if *params.Temperature != 0.2 {
		t.Errorf("temperature = %f", *params.Temperature)
	}
	if params.TopP == nil || *params.TopP != 0.95 {
		t.Errorf("top_p = %v", params.TopP)
	}
	if len(params.Stop) != 1 || params.Stop[0] != "</end>" {
		t.Errorf("stop = %v", params.Stop)
	}
	if params.Reasoning == nil || params.Reasoning.Mode != "enabled" {
		t.Errorf("reasoning = %+v", params.Reasoning)
	}
	if *params.Reasoning.BudgetTokens != 4096 {
		t.Errorf("budget_tokens = %d", *params.Reasoning.BudgetTokens)
	}
}

func TestApplyOverride_Empty(t *testing.T) {
	params := &canonical.CanonicalParams{
		MaxTokens:   intPtr(2048),
		Temperature: f64Ptr(0.7),
	}
	ApplyOverride(params, config.OverrideParams{})

	if *params.MaxTokens != 2048 {
		t.Errorf("max_tokens should be unchanged, got %d", *params.MaxTokens)
	}
	if *params.Temperature != 0.7 {
		t.Errorf("temperature should be unchanged")
	}
}

// ---------------------------------------------------------------------------
// Same-priority ambiguity
// ---------------------------------------------------------------------------

func TestSelect_SamePriorityAmbiguity(t *testing.T) {
	providers := []config.ProviderConfig{
		{
			Name: "provider-a", Protocol: "openai_chat", Priority: 100,
			Models: map[string]config.ModelTargetConfig{"gpt-4o": {Upstream: "gpt-4o"}},
		},
		{
			Name: "provider-b", Protocol: "anthropic_messages", Priority: 100,
			Models: map[string]config.ModelTargetConfig{"gpt-4o": {Upstream: "claude-3-7-sonnet-20250219"}},
		},
	}
	sel := NewSelector(providers)
	req := &canonical.CanonicalRequest{ClientModel: "gpt-4o"}

	// Both have priority 100. Selection should succeed (picks first by sort stability).
	result, err := sel.Select(req)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	// Just verify we get a result — the exact winner is sort-order dependent.
	if result.Provider == nil {
		t.Error("provider should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Override propagation from selection
// ---------------------------------------------------------------------------

func TestSelect_OverridePropagation(t *testing.T) {
	sel := NewSelector(makeProviders())
	req := &canonical.CanonicalRequest{ClientModel: "gpt-4o-mini"}

	result, err := sel.Select(req)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Override.Reasoning == nil {
		t.Fatal("override.reasoning should not be nil")
	}
	if *result.Override.Reasoning.Mode != "enabled" {
		t.Errorf("override.reasoning.mode = %q", *result.Override.Reasoning.Mode)
	}
	if *result.Override.Reasoning.BudgetTokens != 4096 {
		t.Errorf("override.reasoning.budget_tokens = %d", *result.Override.Reasoning.BudgetTokens)
	}
}

func TestResolveOverride_DefaultsAndRules(t *testing.T) {
	req := &canonical.CanonicalRequest{
		ClientModel: "claude-opus-4-6",
		Stream:      true,
	}
	model := "claude-opus-4-6"
	streamTrue := true
	override := config.ProviderOverride{
		Defaults: &config.OverrideParams{
			MaxTokens: intPtr(2048),
			Reasoning: &config.ReasoningOverride{
				Effort: strPtr("medium"),
			},
		},
		Rules: []config.OverrideRule{
			{
				Target: config.OverrideTarget{ClientModel: &model},
				Params: config.OverrideParams{
					Reasoning: &config.ReasoningOverride{
						Effort: strPtr("high"),
					},
				},
			},
			{
				Target: config.OverrideTarget{Stream: &streamTrue},
				Params: config.OverrideParams{
					Reasoning: &config.ReasoningOverride{
						Effort: strPtr("xhigh"),
					},
				},
			},
		},
	}

	resolved := ResolveOverride(req, override)
	if resolved.MaxTokens == nil || *resolved.MaxTokens != 2048 {
		t.Fatalf("max_tokens = %+v", resolved.MaxTokens)
	}
	if resolved.Reasoning == nil || resolved.Reasoning.Effort == nil || *resolved.Reasoning.Effort != "xhigh" {
		t.Fatalf("reasoning.effort = %+v", resolved.Reasoning)
	}
}

func TestResolveOverride_DefaultsOnly(t *testing.T) {
	req := &canonical.CanonicalRequest{ClientModel: "gpt-4o"}
	override := config.ProviderOverride{
		Defaults: &config.OverrideParams{
			MaxTokens:   intPtr(1024),
			Temperature: f64Ptr(0.7),
		},
	}

	resolved := ResolveOverride(req, override)
	if resolved.MaxTokens == nil || *resolved.MaxTokens != 1024 {
		t.Fatalf("max_tokens = %+v", resolved.MaxTokens)
	}
	if resolved.Temperature == nil || *resolved.Temperature != 0.7 {
		t.Fatalf("temperature = %+v", resolved.Temperature)
	}
}

// ---------------------------------------------------------------------------
// Model → upstream model resolution
// ---------------------------------------------------------------------------

func TestSelect_ModelResolution(t *testing.T) {
	sel := NewSelector(makeProviders())

	tests := []struct {
		clientModel  string
		wantUpstream string
		wantProvider string
	}{
		{"gpt-4o", "claude-3-7-sonnet-20250219", "anthropic-main"},
		{"gpt-5-mini", "gpt-5-mini", "openai-main"},
	}
	for _, tt := range tests {
		req := &canonical.CanonicalRequest{ClientModel: tt.clientModel}
		result, err := sel.Select(req)
		if err != nil {
			t.Fatalf("select(%q): %v", tt.clientModel, err)
		}
		if result.UpstreamModel != tt.wantUpstream {
			t.Errorf("select(%q).upstream = %q, want %q", tt.clientModel, result.UpstreamModel, tt.wantUpstream)
		}
		if result.Provider.Name != tt.wantProvider {
			t.Errorf("select(%q).provider = %q, want %q", tt.clientModel, result.Provider.Name, tt.wantProvider)
		}
	}
}
