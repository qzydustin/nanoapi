package codec

import (
	"strings"

	"github.com/qzydustin/nanoapi/config"
)

// BuildOpenAIReasoning resolves the effective OpenAI reasoning_effort from
// Anthropic thinking parameters and the upstream model's capabilities.
// Returns ("", false) when the field should be omitted (e.g. disabled mode),
// which tells the upstream to run without thinking.
func BuildOpenAIReasoning(r *Reasoning, capability *config.ReasoningCapability) (string, bool) {
	// nil / no capability / disabled / no effort → omit reasoning_effort.
	if r == nil || r.Mode == "disabled" || r.Effort == nil ||
		capability == nil || len(capability.AllowedEfforts) == 0 {
		return "", false
	}

	effort := strings.ToLower(strings.TrimSpace(*r.Effort))

	// Apply effort_map if configured (e.g. "max" → "high").
	if mapped, ok := capability.EffortMap[effort]; ok {
		effort = mapped
	}

	// Check against allowed list.
	for _, a := range capability.AllowedEfforts {
		if strings.EqualFold(a, effort) {
			return effort, true
		}
	}

	// Fallback to medium if the effort is not in the allowed list.
	return "medium", true
}
