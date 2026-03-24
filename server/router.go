package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
)

// NewRouter creates the Gin engine with all routes configured.
func NewRouter(
	tokenSvc *TokenService,
	usageSvc *UsageService,
	selector *Selector,
	executor *execute.Executor,
	logCfg config.LoggingConfig,
	serverCfg config.ServerConfig,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	upstreamTimeout := 300 * time.Second
	if serverCfg.TimeoutSeconds > 0 {
		upstreamTimeout = time.Duration(serverCfg.TimeoutSeconds) * time.Second
	}

	var maxBodyBytes int64 = 10 * 1024 * 1024 // 10 MB
	if serverCfg.MaxBodyBytes > 0 {
		maxBodyBytes = serverCfg.MaxBodyBytes
	}

	// Service-owned API endpoints.
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Public proxy routes — token auth.
	proxy := r.Group("")
	proxy.Use(TokenAuthMiddleware(tokenSvc))
	{
		proxy.POST("/v1/messages",
			ProxyHandler(selector, executor, usageSvc, logCfg, upstreamTimeout, maxBodyBytes),
		)
		proxy.POST("/v1/chat/completions",
			OpenAIProxyHandler(selector, executor, usageSvc, logCfg, upstreamTimeout, maxBodyBytes),
		)
		proxy.GET("/v1/models", OpenAIModelsHandler(selector))
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
