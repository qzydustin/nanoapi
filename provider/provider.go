package provider

import (
	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/config"
)

// ProviderSelection is the result of matching a CanonicalRequest to a
// configured provider.
type ProviderSelection struct {
	Provider      *config.ProviderConfig
	Target        *config.ModelTargetConfig
	UpstreamModel string
	ForceStream   bool
	Override      config.OverrideParams
}

func mergeOverrideParams(dst *config.OverrideParams, src config.OverrideParams) {
	if src.MaxTokens != nil {
		dst.MaxTokens = src.MaxTokens
	}
	if src.Temperature != nil {
		dst.Temperature = src.Temperature
	}
	if src.TopP != nil {
		dst.TopP = src.TopP
	}
	if len(src.Stop) > 0 {
		dst.Stop = src.Stop
	}
	if src.Reasoning != nil {
		if dst.Reasoning == nil {
			dst.Reasoning = &config.ReasoningOverride{}
		}
		if src.Reasoning.Mode != nil {
			dst.Reasoning.Mode = src.Reasoning.Mode
		}
		if src.Reasoning.Effort != nil {
			dst.Reasoning.Effort = src.Reasoning.Effort
		}
		if src.Reasoning.BudgetTokens != nil {
			dst.Reasoning.BudgetTokens = src.Reasoning.BudgetTokens
		}
	}
}

func matchesOverrideTarget(req *canonical.CanonicalRequest, target config.OverrideTarget) bool {
	if target.ClientModel != nil && req.ClientModel != *target.ClientModel {
		return false
	}
	if target.Stream != nil && req.Stream != *target.Stream {
		return false
	}
	return true
}

// ResolveOverride computes the final parameter overrides for a request.
// Rules are applied in order, and later matching rules override earlier ones.
func ResolveOverride(req *canonical.CanonicalRequest, override config.ProviderOverride) config.OverrideParams {
	var resolved config.OverrideParams
	if override.Defaults != nil {
		mergeOverrideParams(&resolved, *override.Defaults)
	}
	for _, rule := range override.Rules {
		if matchesOverrideTarget(req, rule.Target) {
			mergeOverrideParams(&resolved, rule.Params)
		}
	}
	return resolved
}

// ApplyOverride merges resolved parameter overrides into canonical params.
// Only non-nil override fields replace existing values.
func ApplyOverride(params *canonical.CanonicalParams, override config.OverrideParams) {
	if override.MaxTokens != nil {
		params.MaxTokens = override.MaxTokens
	}
	if override.Temperature != nil {
		params.Temperature = override.Temperature
	}
	if override.TopP != nil {
		params.TopP = override.TopP
	}
	if len(override.Stop) > 0 {
		params.Stop = override.Stop
	}

	if r := override.Reasoning; r != nil {
		if params.Reasoning == nil {
			params.Reasoning = &canonical.CanonicalReasoning{}
		}
		if r.Mode != nil {
			params.Reasoning.Mode = *r.Mode
		}
		if r.Effort != nil {
			params.Reasoning.Effort = r.Effort
		}
		if r.BudgetTokens != nil {
			params.Reasoning.BudgetTokens = r.BudgetTokens
		}
	}
}
