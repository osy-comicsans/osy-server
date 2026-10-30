package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"osyn/internal/accident"
	"osyn/internal/config"
	"osyn/internal/database"
	"osyn/internal/dlq"
	"osyn/internal/messaging"
	"osyn/internal/observability"
	"osyn/internal/retry"
	"osyn/internal/twilio"
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
	if err := cfg.ValidateNotifications(); err != nil {
		return err
	}
	var sender worker.NotificationSender
	if cfg.Twilio.DryRun {
		sender = worker.NewDryRunSender(logger)
	} else {
		sender, err = twilio.NewClient(twilio.Config{
			AccountSID:          cfg.Twilio.AccountSID,
			AuthToken:           cfg.Twilio.AuthToken,
			FromNumber:          cfg.Twilio.FromNumber,
			MessagingServiceSID: cfg.Twilio.MessagingServiceSID,
			BaseURL:             cfg.Twilio.BaseURL,
			HTTPTimeout:         cfg.Twilio.HTTPTimeout,
		})
		if err != nil {
			return err
		}
	}
	var whatsAppSender worker.NotificationSender
	operatorWhatsApp := ""
	if cfg.WhatsApp.Enabled {
		operatorWhatsApp = cfg.WhatsApp.To
		if cfg.WhatsApp.DryRun {
			whatsAppSender = worker.NewWhatsAppSender(worker.NewDryRunSender(logger))
		} else {
			whatsAppClient, err := twilio.NewClient(twilio.Config{
				AccountSID:  cfg.Twilio.AccountSID,
				AuthToken:   cfg.Twilio.AuthToken,
				FromNumber:  cfg.WhatsApp.From,
				BaseURL:     cfg.Twilio.BaseURL,
				HTTPTimeout: cfg.Twilio.HTTPTimeout,
			})
			if err != nil {
				return err
			}
			whatsAppSender = worker.NewWhatsAppSender(whatsAppClient)
		}
	}
	retryPolicy := retry.New(cfg.Retry.InitialDelay, cfg.Retry.MaxDelay, cfg.Retry.MaxAttempts)
	router := dlq.NewRouter(db, natsClient, cfg.NATS.DLQSubject)
	notifier := worker.NewNotifier(db, sender, whatsAppSender, cfg.Notifications.Workers, retryPolicy, logger)
	targets := accident.DefaultTargets(cfg.Notifications.CivilProtectionPhone, cfg.Notifications.GendarmeriePhone, cfg.Notifications.PolicePhone)
	handler := worker.NewHandler(db, router, notifier, targets, operatorWhatsApp, cfg.Notifications.MaxRadiusMeters, cfg.Worker.MaxDeliveries, retryPolicy, logger)
	service := worker.NewService(consumer, handler, cfg.Worker.Concurrency, cfg.Worker.FetchWait, cfg.Worker.ProcessTimeout, logger)
	logger.Info("worker started", "concurrency", cfg.Worker.Concurrency, "durable", cfg.Worker.ConsumerDurable, "notification_workers", cfg.Notifications.Workers, "twilio_dry_run", cfg.Twilio.DryRun, "whatsapp_enabled", cfg.WhatsApp.Enabled)
	if err := service.Run(ctx); err != nil {
		return err
	}
	if err := consumer.Close(); err != nil {
		logger.Warn("failed to close worker consumer", "error", err)
	}
	logger.Info("worker stopped")
	return nil
}
