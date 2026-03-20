package provider

import (
	"fmt"
	"sort"

	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/config"
)

// Selector finds the best provider for a given canonical request.
type Selector struct {
	providers []config.ProviderConfig
}

// NewSelector creates a Selector from the provider list in config.
func NewSelector(providers []config.ProviderConfig) *Selector {
	return &Selector{providers: providers}
}

// Select picks the highest-priority provider that supports the requested
// client model, resolves the upstream model, and returns a ProviderSelection.
func (s *Selector) Select(req *canonical.CanonicalRequest) (*ProviderSelection, error) {
	selections, err := s.SelectAll(req)
	if err != nil {
		return nil, err
	}
	return selections[0], nil
}

// SelectAll returns all matching providers sorted by priority (highest first).
// This is used for fallback: if the first provider fails, the caller can try
// the next one.
func (s *Selector) SelectAll(req *canonical.CanonicalRequest) ([]*ProviderSelection, error) {
	clientModel := req.ClientModel

	// Collect candidate providers.
	var candidates []*config.ProviderConfig
	for i := range s.providers {
		if _, ok := s.providers[i].Models[clientModel]; ok {
			candidates = append(candidates, &s.providers[i])
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no provider supports model %q", clientModel)
	}

	// Sort by priority descending.
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
			Override:      ResolveOverride(req, p.Override),
		})
	}

	return selections, nil
}
