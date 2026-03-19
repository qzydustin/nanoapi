package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/qzydustin/nanoapi/token"
)

const tokenContextKey = "tokenContext"

// TokenAuthMiddleware validates API tokens on the public proxy routes.
func TokenAuthMiddleware(tokenSvc *token.Service) gin.HandlerFunc {
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
func getTokenContext(c *gin.Context) *token.TokenContext {
	if v, ok := c.Get(tokenContextKey); ok {
		if tc, ok := v.(*token.TokenContext); ok {
			return tc
		}
	}
	return nil
}
