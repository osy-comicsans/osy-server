package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"osyn/internal/accident"
	"osyn/internal/database"
	"osyn/internal/retry"
)

type NotificationSender interface {
	Send(ctx context.Context, to, body string) (string, error)
}

type NotificationStore interface {
	MarkEmergencyNotificationSent(ctx context.Context, id, twilioSID string) error
	MarkEmergencyNotificationFailed(ctx context.Context, id string, cause error) error
}

type Notifier struct {
	store          NotificationStore
	sender         NotificationSender
	whatsAppSender NotificationSender
	workers        int
	retryPolicy    retry.Policy
	logger         *slog.Logger
}

func NewNotifier(store NotificationStore, sender, whatsAppSender NotificationSender, workers int, retryPolicy retry.Policy, logger *slog.Logger) *Notifier {
	if workers < 1 {
		workers = 1
	}
	return &Notifier{
		store:          store,
		sender:         sender,
		whatsAppSender: whatsAppSender,
		workers:        workers,
		retryPolicy:    retryPolicy,
		logger:         logger,
	}
}

func (n *Notifier) Notify(ctx context.Context, notifications []database.EmergencyNotification) error {
	if len(notifications) == 0 {
		return nil
	}
	jobs := make(chan database.EmergencyNotification)
	failures := make(chan error, len(notifications))
	var wg sync.WaitGroup
	wg.Add(n.workers)
	for i := 0; i < n.workers; i++ {
		go func() {
			defer wg.Done()
			for notification := range jobs {
				if err := n.deliver(ctx, notification); err != nil {
					failures <- err
				}
			}
		}()
	}

	for _, notification := range notifications {
		select {
		case <-ctx.Done():
			failures <- ctx.Err()
			close(jobs)
			wg.Wait()
			close(failures)
			return errors.Join(collectErrors(failures)...)
		case jobs <- notification:
		}
	}
	close(jobs)
	wg.Wait()
	close(failures)
	return errors.Join(collectErrors(failures)...)
}

func (n *Notifier) deliver(ctx context.Context, notification database.EmergencyNotification) error {
	sender := n.sender
	if notification.Service == accident.OperatorWhatsAppService {
		sender = n.whatsAppSender
	}
	if sender == nil {
		return fmt.Errorf("no sender configured for notification service %s", notification.Service)
	}
	var sid string
	err := n.retryPolicy.Do(ctx, func(ctx context.Context) error {
		var sendErr error
		sid, sendErr = sender.Send(ctx, notification.TargetPhone, notification.Message)
		return sendErr
	})
	if err != nil {
		if markErr := n.store.MarkEmergencyNotificationFailed(ctx, notification.ID, err); markErr != nil {
			return fmt.Errorf("send alert to %s: %w; record failure: %v", notification.Service, err, markErr)
		}
		return fmt.Errorf("send alert to %s: %w", notification.Service, err)
	}
	if err := n.store.MarkEmergencyNotificationSent(ctx, notification.ID, sid); err != nil {
		return fmt.Errorf("record sent alert to %s: %w", notification.Service, err)
	}
	n.logger.Info("emergency notification sent", "recipient_service", notification.Service, "phone", notification.TargetPhone, "sid", sid)
	return nil
}

type DryRunSender struct {
	logger *slog.Logger
}

func NewDryRunSender(logger *slog.Logger) *DryRunSender {
	return &DryRunSender{logger: logger}
}

func (s *DryRunSender) Send(_ context.Context, to, body string) (string, error) {
	s.logger.Info("emergency notification dry run", "to", to, "message", body)
	return "dry-run-" + uuid.NewString(), nil
}

func collectErrors(failures <-chan error) []error {
	errs := make([]error, 0)
	for err := range failures {
		errs = append(errs, err)
	}
	return errs
}
