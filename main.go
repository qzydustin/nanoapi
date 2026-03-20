package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
	"github.com/qzydustin/nanoapi/provider"
	"github.com/qzydustin/nanoapi/server"
	"github.com/qzydustin/nanoapi/storage"
	"github.com/qzydustin/nanoapi/token"
	"github.com/qzydustin/nanoapi/usage"
)

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	// Load and validate config.
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if err := config.Validate(cfg); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	// Initialize storage.
	db, err := storage.NewDB(cfg.Storage)
	if err != nil {
		log.Fatalf("failed to initialize storage: %v", err)
	}
	if err := db.Migrate(&usage.UsageRecord{}); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// Initialize services.
	tokenSvc := token.NewService(cfg.Tokens)

	usageStore := usage.NewSQLiteStore(db.Gorm)
	usageSvc := usage.NewService(usageStore)

	selector := provider.NewSelector(cfg.Providers)
	executor := execute.NewExecutor()

	// Build router.
	router := server.NewRouter(tokenSvc, usageSvc, selector, executor, cfg.Logging, cfg.Server)

	// Start server.
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		slog.Info("nanoapi starting", "address", addr, "providers", len(cfg.Providers))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	slog.Info("server stopped")
}
