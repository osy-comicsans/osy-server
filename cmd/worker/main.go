package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"osyn/internal/config"
	"osyn/internal/database"
	"osyn/internal/dlq"
	"osyn/internal/messaging"
	"osyn/internal/observability"
	"osyn/internal/retry"
	"osyn/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("osyn worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.LogLevel, cfg.AppName, "worker")
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
	natsClient, err := messaging.Connect(messaging.ConnectConfig{URL: cfg.NATS.URL, Name: cfg.AppName + "-worker", Logger: logger})
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
	consumer, err := natsClient.NewConsumer(ctx, messaging.ConsumerConfig{
		Stream:        cfg.NATS.SMSStream,
		Subject:       cfg.NATS.SMSSubject,
		Durable:       cfg.Worker.ConsumerDurable,
		AckWait:       cfg.Worker.AckWait,
		MaxDeliver:    cfg.Worker.MaxDeliveries + 1,
		MaxAckPending: cfg.Worker.Concurrency,
	})
	if err != nil {
		return fmt.Errorf("create worker consumer: %w", err)
	}
	retryPolicy := retry.New(cfg.Retry.InitialDelay, cfg.Retry.MaxDelay, cfg.Retry.MaxAttempts)
	router := dlq.NewRouter(db, natsClient, cfg.NATS.DLQSubject)
	handler := worker.NewHandler(db, router, cfg.Worker.MaxDeliveries, retryPolicy, logger)
	service := worker.NewService(consumer, handler, cfg.Worker.Concurrency, cfg.Worker.FetchWait, cfg.Worker.ProcessTimeout, logger)
	logger.Info("worker started", "concurrency", cfg.Worker.Concurrency, "durable", cfg.Worker.ConsumerDurable)
	if err := service.Run(ctx); err != nil {
		return err
	}
	if err := consumer.Close(); err != nil {
		logger.Warn("failed to close worker consumer", "error", err)
	}
	logger.Info("worker stopped")
	return nil
}
