package codec

import (
	"strings"

	"github.com/qzydustin/nanoapi/config"
)

func normalizeEffortValue(effort string) string {
	return strings.ToLower(strings.TrimSpace(effort))
}

func allowedEffortsMap(capability *config.ReasoningCapability) map[string]struct{} {
	if capability == nil {
		return nil
	}
	out := make(map[string]struct{}, len(capability.AllowedEfforts))
	for _, effort := range capability.AllowedEfforts {
		normalized := normalizeEffortValue(effort)
		if normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	return out
}

func supportsEffort(allowed map[string]struct{}, effort string) bool {
	if allowed == nil {
		return false
	}
	_, ok := allowed[normalizeEffortValue(effort)]
	return ok
}

func mapOpenAIEffort(effort string, capability *config.ReasoningCapability) (string, bool) {
	effort = normalizeEffortValue(effort)
	if effort == "" {
		return "", false
	}

	allowed := allowedEffortsMap(capability)
	if len(allowed) == 0 {
		switch effort {
		case "minimal":
			return "low", true
		case "max", "xhigh":
			return "high", true
		default:
			return effort, true
		}
	}

	if supportsEffort(allowed, effort) {
		return effort, true
	}
	switch effort {
	case "minimal":
		if supportsEffort(allowed, "low") {
			return "low", true
		}
	case "auto":
		if supportsEffort(allowed, "medium") {
			return "medium", true
		}
	case "max", "xhigh":
		if supportsEffort(allowed, "high") {
			return "high", true
		}
	}
	return "", false
}

func mapOpenAIBudgetToEffort(budget int, capability *config.ReasoningCapability) (string, bool) {
	switch {
	case budget <= 0:
		return "", false
	case budget <= 1024:
		return mapOpenAIEffort("low", capability)
	case budget <= 8192:
		return mapOpenAIEffort("medium", capability)
	default:
		return mapOpenAIEffort("high", capability)
	}
}

func mapOpenAIDisabledEffort(capability *config.ReasoningCapability) (string, bool) {
	for _, candidate := range []string{"none", "minimal", "low"} {
		if effort, ok := mapOpenAIEffort(candidate, capability); ok {
			return effort, true
		}
	}
	return "", false
}

// DebugOpenAIReasoning returns the effective OpenAI effort for logging.
func DebugOpenAIReasoning(r *Reasoning, capability *config.ReasoningCapability) (string, bool) {
	if r == nil {
		return "", false
	}
	if r.Mode == "disabled" {
		return mapOpenAIDisabledEffort(capability)
	}
	if r.Effort != nil {
		return mapOpenAIEffort(*r.Effort, capability)
	}
	if r.BudgetTokens != nil {
		return mapOpenAIBudgetToEffort(*r.BudgetTokens, capability)
	}
	if r.Mode != "" {
		return mapOpenAIEffort("auto", capability)
	}
	return "", false
}
