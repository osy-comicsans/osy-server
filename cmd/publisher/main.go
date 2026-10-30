package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"osyn/internal/config"
	"osyn/internal/database"
	"osyn/internal/messaging"
	"osyn/internal/observability"
	"osyn/internal/outbox"
	"osyn/internal/retry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("osyn publisher stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.LogLevel, cfg.AppName, "publisher")
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
	natsClient, err := messaging.Connect(messaging.ConnectConfig{URL: cfg.NATS.URL, Name: cfg.AppName + "-publisher", Logger: logger})
	if err != nil {
		return err
	}
	defer natsClient.Close()
	if err := natsClient.EnsureStreams(ctx, messaging.StreamsConfig{
		SMSStream:       cfg.NATS.SMSStream,
		SMSSubject:      cfg.NATS.SMSSubject,
		DLQStream:       cfg.NATS.DLQStream,
		DLQSubject:      cfg.NATS.DLQSubject,
		MaxAge:          cfg.NATS.StreamMaxAge,
		DuplicateWindow: cfg.NATS.DuplicateWindow,
		MaxMsgs:         cfg.NATS.StreamMaxMsgs,
		MaxBytes:        cfg.NATS.StreamMaxBytes,
	}); err != nil {
		return err
	}
	retryPolicy := retry.New(cfg.Retry.InitialDelay, cfg.Retry.MaxDelay, cfg.Retry.MaxAttempts)
	service := outbox.New(
		db,
		natsClient,
		cfg.Outbox.PublishInterval,
		cfg.Outbox.BatchSize,
		cfg.Outbox.Lease,
		cfg.NATS.PublishTimeout,
		cfg.Retry.MaxAttempts,
		retryPolicy,
		logger,
	)
	logger.Info("outbox publisher started", "interval", cfg.Outbox.PublishInterval, "batch_size", cfg.Outbox.BatchSize)
	if err := service.Run(ctx); err != nil {
		return err
	}
	logger.Info("outbox publisher stopped")
	return nil
}
