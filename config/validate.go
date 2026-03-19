package config

import (
	"fmt"
	"strings"
)

func validateOverrideParams(prefix string, params OverrideParams) error {
	if r := params.Reasoning; r != nil {
		if r.Mode != nil && *r.Mode == "disabled" && r.BudgetTokens != nil {
			return fmt.Errorf("%s: reasoning mode \"disabled\" cannot be combined with budget_tokens", prefix)
		}
	}
	return nil
}

// Validate checks a loaded Config for consistency and required fields.
// It returns the first error found.
func Validate(cfg *Config) error {
	// --- server ---
	if cfg.Server.Host == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Logging.RequestDir) == "" {
		return fmt.Errorf("logging.request_dir must not be empty")
	}
	// --- storage ---
	if cfg.Storage.Driver == "" {
		return fmt.Errorf("storage.driver must not be empty")
	}
	if cfg.Storage.Driver != "sqlite" {
		return fmt.Errorf("storage.driver must be \"sqlite\" (got %q)", cfg.Storage.Driver)
	}
	if cfg.Storage.DSN == "" {
		return fmt.Errorf("storage.dsn must not be empty")
	}

	// --- tokens ---
	if len(cfg.Tokens) == 0 {
		return fmt.Errorf("at least one token must be configured")
	}
	seenTokenIDs := make(map[string]struct{})
	for i, t := range cfg.Tokens {
		prefix := fmt.Sprintf("tokens[%d]", i)
		if strings.TrimSpace(t.ID) == "" {
			return fmt.Errorf("%s: id must not be empty", prefix)
		}
		if strings.TrimSpace(t.Key) == "" {
			return fmt.Errorf("%s: key must not be empty", prefix)
		}
		if _, ok := seenTokenIDs[t.ID]; ok {
			return fmt.Errorf("%s: duplicate id %q", prefix, t.ID)
		}
		seenTokenIDs[t.ID] = struct{}{}
	}

	// --- providers ---
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}

	// Track highest priority per client model for ambiguity detection.
	type modelPriority struct {
		provider string
		priority int
	}
	bestPerModel := make(map[string]modelPriority)

	for i, p := range cfg.Providers {
		prefix := fmt.Sprintf("providers[%d] (%s)", i, p.Name)

		if p.Name == "" {
			return fmt.Errorf("%s: name must not be empty", prefix)
		}
		if p.Protocol != "openai_chat" && p.Protocol != "anthropic_messages" {
			return fmt.Errorf("%s: protocol must be \"openai_chat\" or \"anthropic_messages\" (got %q)", prefix, p.Protocol)
		}
		if p.SearchMode != "" && p.SearchMode != "openai" && p.SearchMode != "openwebui" {
			return fmt.Errorf("%s: search_mode must be \"openai\" or \"openwebui\" when set (got %q)", prefix, p.SearchMode)
		}
		if p.BaseURL == "" {
			return fmt.Errorf("%s: base_url must not be empty", prefix)
		}
		if strings.TrimSpace(p.APIKey) == "" {
			return fmt.Errorf("%s: api_key must not be empty", prefix)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("%s: models must not be empty", prefix)
		}
		for clientModel, target := range p.Models {
			if strings.TrimSpace(clientModel) == "" {
				return fmt.Errorf("%s: model key must not be empty", prefix)
			}
			if strings.TrimSpace(target.Upstream) == "" {
				return fmt.Errorf("%s: model value for %q must not be empty", prefix, clientModel)
			}
			if target.Reasoning != nil {
				seenEfforts := make(map[string]struct{})
				for idx, effort := range target.Reasoning.AllowedEfforts {
					normalized := strings.ToLower(strings.TrimSpace(effort))
					if normalized == "" {
						return fmt.Errorf("%s: models[%q].reasoning.allowed_efforts[%d] must not be empty", prefix, clientModel, idx)
					}
					if _, ok := seenEfforts[normalized]; ok {
						return fmt.Errorf("%s: models[%q].reasoning has duplicate effort %q", prefix, clientModel, normalized)
					}
					seenEfforts[normalized] = struct{}{}
				}
			}

			// Priority ambiguity check.
			if prev, ok := bestPerModel[clientModel]; ok {
				if p.Priority == prev.priority {
					return fmt.Errorf(
						"ambiguous priority: client model %q is served by providers %q and %q at the same priority %d",
						clientModel, prev.provider, p.Name, p.Priority,
					)
				}
			}
			if prev, ok := bestPerModel[clientModel]; !ok || p.Priority > prev.priority {
				bestPerModel[clientModel] = modelPriority{provider: p.Name, priority: p.Priority}
			}
		}

		if p.Override.Defaults != nil {
			if err := validateOverrideParams(prefix+" defaults", *p.Override.Defaults); err != nil {
				return err
			}
		}
		for j, rule := range p.Override.Rules {
			rulePrefix := fmt.Sprintf("%s rules[%d]", prefix, j)
			if rule.Target.ClientModel == nil && rule.Target.Stream == nil {
				return fmt.Errorf("%s: target must include at least one condition", rulePrefix)
			}
			if rule.Target.ClientModel != nil && strings.TrimSpace(*rule.Target.ClientModel) == "" {
				return fmt.Errorf("%s: target.client_model must not be empty", rulePrefix)
			}
			if err := validateOverrideParams(rulePrefix, rule.Params); err != nil {
				return err
			}
		}
	}

	return nil
}
