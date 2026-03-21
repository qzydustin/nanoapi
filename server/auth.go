package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/qzydustin/nanoapi/config"
)

const tokenContextKey = "tokenContext"

// TokenContext is the authentication context carried through the request pipeline.
type TokenContext struct {
	TokenID string
}

// TokenService provides static token authentication from config.
type TokenService struct {
	byKey map[string]*TokenContext
}

// NewTokenService creates a new TokenService from config tokens.
func NewTokenService(tokens []config.TokenConfig) *TokenService {
	byKey := make(map[string]*TokenContext, len(tokens))
	for _, tok := range tokens {
		byKey[tok.Key] = &TokenContext{
			TokenID: tok.ID,
		}
	}
	return &TokenService{byKey: byKey}
}

// Authenticate verifies a raw token against the configured token set.
func (s *TokenService) Authenticate(rawToken string) (*TokenContext, error) {
	tc, ok := s.byKey[rawToken]
	if !ok {
		return nil, fmt.Errorf("token not found")
	}
	return tc, nil
}

// TokenAuthMiddleware validates API tokens on the public proxy routes.
func TokenAuthMiddleware(tokenSvc *TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		raw := ""
		if strings.HasPrefix(auth, "Bearer ") {
			raw = strings.TrimPrefix(auth, "Bearer ")
		} else if strings.HasPrefix(auth, "bearer ") {
			raw = strings.TrimPrefix(auth, "bearer ")
		} else if key := c.GetHeader("x-api-key"); key != "" {
			raw = key
		}

		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "missing API token", "type": "authentication_error"},
			})
			return
		}

		ctx, err := tokenSvc.Authenticate(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": err.Error(), "type": "authentication_error"},
			})
			return
		}

		c.Set(tokenContextKey, ctx)
		c.Next()
	}
}

// getTokenContext retrieves the TokenContext from the gin context.
func getTokenContext(c *gin.Context) *TokenContext {
	if v, ok := c.Get(tokenContextKey); ok {
		if tc, ok := v.(*TokenContext); ok {
			return tc
		}
	}
	return nil
}
