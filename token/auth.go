package token

import (
	"fmt"

	"github.com/qzydustin/nanoapi/config"
)

// TokenContext is the authentication context carried through the request pipeline.
type TokenContext struct {
	TokenID   string
	TokenName string
	Status    string
}

// Service provides static token authentication from config.
type Service struct {
	byKey map[string]*TokenContext
}

// NewService creates a new token Service from config tokens.
func NewService(tokens []config.TokenConfig) *Service {
	byKey := make(map[string]*TokenContext, len(tokens))
	for _, tok := range tokens {
		byKey[tok.Key] = &TokenContext{
			TokenID:   tok.ID,
			TokenName: tok.Name,
			Status:    "active",
		}
	}
	return &Service{byKey: byKey}
}

// Authenticate verifies a raw token against the configured token set.
func (s *Service) Authenticate(rawToken string) (*TokenContext, error) {
	tc, ok := s.byKey[rawToken]
	if !ok {
		return nil, fmt.Errorf("token not found")
	}
	return tc, nil
}
