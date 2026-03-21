package server

import (
	"fmt"
	"sort"

	"github.com/qzydustin/nanoapi/codec"
	"github.com/qzydustin/nanoapi/config"
)

// ProviderSelection is the result of matching a Request to a
// configured provider.
type ProviderSelection struct {
	Provider      *config.ProviderConfig
	Target        *config.ModelTargetConfig
	UpstreamModel string
	ForceStream   bool
	Override      config.OverrideParams
}

// Selector finds the best provider for a given request.
type Selector struct {
	providers []config.ProviderConfig
}

// NewSelector creates a Selector from the provider list in config.
func NewSelector(providers []config.ProviderConfig) *Selector {
	return &Selector{providers: providers}
}

// SelectAll returns all matching providers sorted by priority (highest first).
func (s *Selector) SelectAll(req *codec.Request) ([]*ProviderSelection, error) {
	clientModel := req.ClientModel

	var candidates []*config.ProviderConfig
	for i := range s.providers {
		if _, ok := s.providers[i].Models[clientModel]; ok {
			candidates = append(candidates, &s.providers[i])
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no provider supports model %q", clientModel)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority > candidates[j].Priority
	})

	var selections []*ProviderSelection
	for _, p := range candidates {
		target := p.Models[clientModel]
		upstreamModel := target.Upstream

		selections = append(selections, &ProviderSelection{
			Provider:      p,
			Target:        &target,
			UpstreamModel: upstreamModel,
			ForceStream:   p.ForceStream,
			Override:      resolveOverride(req, p.Override),
		})
	}

	return selections, nil
}

func resolveOverride(req *codec.Request, override config.ProviderOverride) config.OverrideParams {
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

func applyOverride(params *codec.Params, override config.OverrideParams) {
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
			params.Reasoning = &codec.Reasoning{}
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

func matchesOverrideTarget(req *codec.Request, target config.OverrideTarget) bool {
	if target.ClientModel != nil && req.ClientModel != *target.ClientModel {
		return false
	}
	if target.Stream != nil && req.Stream != *target.Stream {
		return false
	}
	return true
}
