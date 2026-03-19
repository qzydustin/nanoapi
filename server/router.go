package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
	"github.com/qzydustin/nanoapi/provider"
	"github.com/qzydustin/nanoapi/token"
	"github.com/qzydustin/nanoapi/usage"
)

// NewRouter creates the Gin engine with all routes configured.
func NewRouter(
	tokenSvc *token.Service,
	usageSvc *usage.Service,
	selector *provider.Selector,
	executor *execute.Executor,
	logCfg config.LoggingConfig,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Service-owned API endpoints.
	r.GET("/api/health", HealthHandler())

	// Public proxy routes — token auth.
	proxy := r.Group("")
	proxy.Use(TokenAuthMiddleware(tokenSvc))
	{
		proxy.POST("/v1/chat/completions",
			ProxyHandler(canonical.ProtocolOpenAIChat, selector, executor, usageSvc, logCfg))
		proxy.POST("/v1/messages",
			ProxyHandler(canonical.ProtocolAnthropicMessage, selector, executor, usageSvc, logCfg))
	}

	// Gateway-owned API endpoints — token self-query.
	api := r.Group("/api")
	api.Use(TokenAuthMiddleware(tokenSvc))
	{
		api.GET("/usage", UsageSummaryHandler(usageSvc))
		api.GET("/logs", UsageLogsHandler(usageSvc))
	}

	return r
}

// HealthHandler returns a simple health check endpoint.
func HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
