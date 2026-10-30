package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"osyn/internal/auth"
	"osyn/internal/config"
	"osyn/internal/database"
	"osyn/internal/dlq"
	apihttp "osyn/internal/http"
	"osyn/internal/http/handlers"
	"osyn/internal/messaging"
	"osyn/internal/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("osyn api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateAuth(); err != nil {
		return err
	}
	logger := observability.NewLogger(cfg.LogLevel, cfg.AppName, "api")
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
	natsClient, err := messaging.Connect(messaging.ConnectConfig{URL: cfg.NATS.URL, Name: cfg.AppName + "-api", Logger: logger})
	if err != nil {
		return err
	}
	defer natsClient.Close()
	if err := natsClient.EnsureStreams(ctx, streamConfig(cfg)); err != nil {
		return err
	}

	metrics := observability.NewMetrics()
	dlqService := dlq.NewService(db, cfg.NATS.SMSSubject)
	handler := handlers.New(db, dlqService, db, natsClient, metrics, logger)
	authService := auth.NewService(db, auth.Config{
		Secret:     []byte(cfg.Auth.TokenSecret),
		AccessTTL:  cfg.Auth.AccessTokenTTL,
		RefreshTTL: cfg.Auth.RefreshTokenTTL,
	})
	server := &http.Server{
		Addr: ":" + cfg.HTTPPort,
		Handler: apihttp.NewRouter(apihttp.Dependencies{
			Handlers: handler,
			Auth:     handlers.NewAuth(authService, logger),
			Users:    authService,
			APIKey:   cfg.APIKey,
			Logger:   logger,
			Metrics:  metrics,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.HTTPTimeout,
		WriteTimeout:      cfg.HTTPTimeout,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		return fmt.Errorf("serve http: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	logger.Info("api stopped")
	return nil
}

func streamConfig(cfg *config.Config) messaging.StreamsConfig {
	return messaging.StreamsConfig{
		SMSStream:       cfg.NATS.SMSStream,
		SMSSubject:      cfg.NATS.SMSSubject,
		DLQStream:       cfg.NATS.DLQStream,
		DLQSubject:      cfg.NATS.DLQSubject,
		MaxAge:          cfg.NATS.StreamMaxAge,
		DuplicateWindow: cfg.NATS.DuplicateWindow,
		MaxMsgs:         cfg.NATS.StreamMaxMsgs,
		MaxBytes:        cfg.NATS.StreamMaxBytes,
	}
}
