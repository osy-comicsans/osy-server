package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"osyn/internal/messaging"
	"osyn/internal/retry"
)

type Store interface {
	ProcessSMSEvent(ctx context.Context, event messaging.SMSEvent) error
}

type DeadLetterRouter interface {
	Route(ctx context.Context, eventID string, event messaging.SMSEvent, payload []byte, cause error, attempts int) error
}

type Handler struct {
	store         Store
	dlq           DeadLetterRouter
	maxDeliveries int
	retryPolicy   retry.Policy
	logger        *slog.Logger
}

func NewHandler(store Store, dlq DeadLetterRouter, maxDeliveries int, retryPolicy retry.Policy, logger *slog.Logger) *Handler {
	if maxDeliveries < 1 {
		maxDeliveries = 1
	}
	return &Handler{
		store:         store,
		dlq:           dlq,
		maxDeliveries: maxDeliveries,
		retryPolicy:   retryPolicy,
		logger:        logger,
	}
}

func (h *Handler) Handle(ctx context.Context, delivery messaging.Delivery) {
	payload := delivery.Data()
	attempts := int(delivery.Info().Deliveries)
	if attempts < 1 {
		attempts = 1
	}
	eventID := fallbackEventID(delivery, payload)

	event, err := messaging.DecodeSMSEvent(payload)
	if err != nil {
		h.routeToDLQ(ctx, delivery, eventID, messaging.SMSEvent{}, payload, fmt.Errorf("invalid sms event: %w", err), attempts)
		return
	}
	if event.EventID != "" {
		eventID = event.EventID
	}

	err = h.store.ProcessSMSEvent(ctx, event)
	if err == nil {
		if ackErr := delivery.Ack(); ackErr != nil {
			h.logger.Error("failed to acknowledge processed message", "event_id", eventID, "error", ackErr)
			return
		}
		h.logger.Info("sms message processed", "event_id", eventID, "provider_message_id", event.ProviderMessageID)
		return
	}
	if ctx.Err() != nil {
		return
	}
	if retry.IsPermanent(err) || attempts >= h.maxDeliveries {
		h.routeToDLQ(ctx, delivery, eventID, event, payload, err, attempts)
		return
	}

	delay := h.retryPolicy.Delay(attempts)
	if nakErr := delivery.NakWithDelay(delay); nakErr != nil {
		h.logger.Error("failed to schedule message retry", "event_id", eventID, "error", nakErr)
		return
	}
	h.logger.Warn("sms processing failed; retry scheduled", "event_id", eventID, "attempt", attempts, "delay", delay, "error", err)
}

func (h *Handler) routeToDLQ(ctx context.Context, delivery messaging.Delivery, eventID string, event messaging.SMSEvent, payload []byte, cause error, attempts int) {
	if ctx.Err() != nil {
		return
	}
	if err := h.dlq.Route(ctx, eventID, event, payload, cause, attempts); err != nil {
		delay := h.retryPolicy.Delay(attempts)
		if nakErr := delivery.NakWithDelay(delay); nakErr != nil {
			h.logger.Error("failed to retry dead letter routing", "event_id", eventID, "error", nakErr)
			return
		}
		h.logger.Error("dead letter routing failed; retry scheduled", "event_id", eventID, "delay", delay, "error", err)
		return
	}
	if err := delivery.Ack(); err != nil {
		h.logger.Error("failed to acknowledge dead-lettered message", "event_id", eventID, "error", err)
		return
	}
	h.logger.Error("sms message moved to dead letter queue", "event_id", eventID, "attempts", attempts, "error", cause)
}

func fallbackEventID(delivery messaging.Delivery, payload []byte) string {
	info := delivery.Info()
	if info.StreamSequence > 0 {
		return fmt.Sprintf("invalid:%s:%d", info.Consumer, info.StreamSequence)
	}
	sum := sha256.Sum256(payload)
	return "invalid:" + hex.EncodeToString(sum[:])
}
