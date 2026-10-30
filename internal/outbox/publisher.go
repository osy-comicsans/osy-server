package outbox

import (
	"context"
	"log/slog"
	"time"

	"osyn/internal/database"
	"osyn/internal/retry"
)

type Store interface {
	ClaimOutboxEvents(ctx context.Context, limit int, lease time.Duration) ([]database.OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, id string) error
	MarkOutboxFailed(ctx context.Context, id string, maxAttempts int, nextAttemptAt time.Time, cause error) (string, int, error)
}

type Publisher interface {
	Publish(ctx context.Context, subject, messageID string, data []byte) error
}

type Service struct {
	store          Store
	publisher      Publisher
	interval       time.Duration
	batchSize      int
	lease          time.Duration
	publishTimeout time.Duration
	maxAttempts    int
	retryPolicy    retry.Policy
	logger         *slog.Logger
}

func New(store Store, publisher Publisher, interval time.Duration, batchSize int, lease, publishTimeout time.Duration, maxAttempts int, retryPolicy retry.Policy, logger *slog.Logger) *Service {
	if interval <= 0 {
		interval = time.Second
	}
	if batchSize < 1 {
		batchSize = 1
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	if publishTimeout <= 0 {
		publishTimeout = 10 * time.Second
	}
	return &Service{
		store:          store,
		publisher:      publisher,
		interval:       interval,
		batchSize:      batchSize,
		lease:          lease,
		publishTimeout: publishTimeout,
		maxAttempts:    maxAttempts,
		retryPolicy:    retryPolicy,
		logger:         logger,
	}
}

func (s *Service) Run(ctx context.Context) error {
	s.publishOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.publishOnce(ctx)
		}
	}
}

func (s *Service) PublishOnce(ctx context.Context) (int, int, error) {
	events, err := s.store.ClaimOutboxEvents(ctx, s.batchSize, s.lease)
	if err != nil {
		return 0, 0, err
	}
	published := 0
	failed := 0
	for _, event := range events {
		if ctx.Err() != nil {
			return published, failed, ctx.Err()
		}
		publishCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
		publishErr := s.publisher.Publish(publishCtx, event.Subject, event.ID, event.Payload)
		cancel()
		if publishErr == nil {
			if err := s.store.MarkOutboxPublished(ctx, event.ID); err != nil {
				s.logger.Error("failed to mark outbox event published", "event_id", event.ID, "error", err)
				continue
			}
			published++
			continue
		}

		nextAttemptAt := time.Now().Add(s.retryPolicy.Delay(event.Attempts + 1))
		status, attempts, err := s.store.MarkOutboxFailed(ctx, event.ID, s.maxAttempts, nextAttemptAt, publishErr)
		if err != nil {
			s.logger.Error("failed to schedule outbox retry", "event_id", event.ID, "error", err)
			continue
		}
		failed++
		if status == "failed" {
			s.logger.Error("outbox event exhausted publish attempts", "event_id", event.ID, "attempts", attempts, "error", publishErr)
		} else {
			s.logger.Warn("outbox publish failed", "event_id", event.ID, "attempts", attempts, "next_attempt_at", nextAttemptAt, "error", publishErr)
		}
	}
	return published, failed, nil
}

func (s *Service) publishOnce(ctx context.Context) {
	published, failed, err := s.PublishOnce(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("outbox publish cycle failed", "error", err)
		}
		return
	}
	if published > 0 || failed > 0 {
		s.logger.Info("outbox publish cycle completed", "published", published, "failed", failed)
	}
}
