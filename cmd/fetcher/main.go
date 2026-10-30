package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"osyn/internal/africastalking"
	"osyn/internal/config"
	"osyn/internal/database"
	"osyn/internal/fetcher"
	"osyn/internal/observability"
	"osyn/internal/retry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("osyn fetcher stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.LogLevel, cfg.AppName, "fetcher")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx, cfg.MigrationsDir); err != nil {
		return err
	}
	retryPolicy := retry.New(cfg.Retry.InitialDelay, cfg.Retry.MaxDelay, cfg.Retry.MaxAttempts)
	provider, err := africastalking.NewClient(africastalking.Config{
		BaseURL:     cfg.AfricaTalking.BaseURL,
		Username:    cfg.AfricaTalking.Username,
		APIKey:      cfg.AfricaTalking.APIKey,
		HTTPTimeout: cfg.AfricaTalking.HTTPTimeout,
		Retry:       retryPolicy,
	})
	if err != nil {
		return err
	}
	service := fetcher.New(provider, db, cfg.Provider, cfg.NATS.SMSSubject, cfg.Fetcher.Interval, cfg.Fetcher.BatchSize, logger)
	logger.Info("fetcher started", "provider", cfg.Provider, "interval", cfg.Fetcher.Interval)
	if err := service.Run(ctx); err != nil {
		return err
	}
	logger.Info("fetcher stopped")
	return nil
}
