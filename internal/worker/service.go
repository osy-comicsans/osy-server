package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"osyn/internal/messaging"
)

type Consumer interface {
	Fetch(ctx context.Context, batch int) ([]messaging.Delivery, error)
	SetMaxWait(wait time.Duration)
}

type Service struct {
	consumer       Consumer
	handler        *Handler
	concurrency    int
	fetchWait      time.Duration
	processTimeout time.Duration
	logger         *slog.Logger
}

func NewService(consumer Consumer, handler *Handler, concurrency int, fetchWait, processTimeout time.Duration, logger *slog.Logger) *Service {
	if concurrency < 1 {
		concurrency = 1
	}
	if fetchWait <= 0 {
		fetchWait = 2 * time.Second
	}
	if processTimeout <= 0 {
		processTimeout = 25 * time.Second
	}
	consumer.SetMaxWait(fetchWait)
	return &Service{
		consumer:       consumer,
		handler:        handler,
		concurrency:    concurrency,
		fetchWait:      fetchWait,
		processTimeout: processTimeout,
		logger:         logger,
	}
}

func (s *Service) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(s.concurrency)
	for i := 0; i < s.concurrency; i++ {
		go func() {
			defer wg.Done()
			s.consume(ctx)
		}()
	}
	<-ctx.Done()
	wg.Wait()
	return nil
}

func (s *Service) consume(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		deliveries, err := s.consumer.Fetch(ctx, 1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, messaging.ErrNoMessages) {
				continue
			}
			s.logger.Error("worker fetch failed", "error", err)
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		for _, delivery := range deliveries {
			if ctx.Err() != nil {
				return
			}
			processCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.processTimeout)
			s.handler.Handle(processCtx, delivery)
			cancel()
		}
	}
}
