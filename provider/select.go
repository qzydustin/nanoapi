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

	best := candidates[0]
	target := best.Models[clientModel]
	upstreamModel := target.Upstream

	return &ProviderSelection{
		Provider:      best,
		Target:        &target,
		UpstreamModel: upstreamModel,
		ForceStream:   best.ForceStream,
		Override:      ResolveOverride(req, best.Override),
	}, nil
}
