package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"osyn/internal/accident"
	"osyn/internal/database"
	"osyn/internal/messaging"
	"osyn/internal/retry"
)

type Store interface {
	ProcessAccidentEvent(ctx context.Context, event messaging.SMSEvent, report accident.Report, alerts []accident.Alert) ([]database.EmergencyNotification, error)
}

type DeadLetterRouter interface {
	Route(ctx context.Context, eventID string, event messaging.SMSEvent, payload []byte, cause error, attempts int) error
}

type NotificationDispatcher interface {
	Notify(ctx context.Context, notifications []database.EmergencyNotification) error
}

type Handler struct {
	store           Store
	dlq             DeadLetterRouter
	notifier        NotificationDispatcher
	targets         []accident.Target
	maxRadiusMeters int
	maxDeliveries   int
	retryPolicy     retry.Policy
	logger          *slog.Logger
}

func NewHandler(store Store, dlq DeadLetterRouter, notifier NotificationDispatcher, targets []accident.Target, maxRadiusMeters, maxDeliveries int, retryPolicy retry.Policy, logger *slog.Logger) *Handler {
	if maxDeliveries < 1 {
		maxDeliveries = 1
	}
	if maxRadiusMeters < 1 {
		maxRadiusMeters = 100000
	}
	return &Handler{
		store:           store,
		dlq:             dlq,
		notifier:        notifier,
		targets:         targets,
		maxRadiusMeters: maxRadiusMeters,
		maxDeliveries:   maxDeliveries,
		retryPolicy:     retryPolicy,
		logger:          logger,
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

	report, err := accident.Parse(event.Message, h.maxRadiusMeters)
	if err != nil {
		h.handleFailure(ctx, delivery, eventID, event, payload, retry.Permanent(fmt.Errorf("invalid accident report: %w", err)), attempts)
		return
	}
	alerts := accident.BuildAlerts(report, event.ProviderMessageID, h.targets)
	notifications, err := h.store.ProcessAccidentEvent(ctx, event, report, alerts)
	if err != nil {
		h.handleFailure(ctx, delivery, eventID, event, payload, err, attempts)
		return
	}
	if len(notifications) > 0 && h.notifier != nil {
		if err := h.notifier.Notify(ctx, notifications); err != nil {
			h.handleFailure(ctx, delivery, eventID, event, payload, err, attempts)
			return
		}
	}
	if ackErr := delivery.Ack(); ackErr != nil {
		h.logger.Error("failed to acknowledge processed accident", "event_id", eventID, "error", ackErr)
		return
	}
	h.logger.Info("accident report processed",
		"event_id", eventID,
		"provider_message_id", event.ProviderMessageID,
		"latitude", report.Latitude,
		"longitude", report.Longitude,
		"damage_percentage", report.DamagePercentage,
		"radius_meters", report.RadiusMeters,
		"notifications", len(notifications),
	)
}

func (h *Handler) handleFailure(ctx context.Context, delivery messaging.Delivery, eventID string, event messaging.SMSEvent, payload []byte, cause error, attempts int) {
	if ctx.Err() != nil {
		return
	}
	if retry.IsPermanent(cause) || attempts >= h.maxDeliveries {
		h.routeToDLQ(ctx, delivery, eventID, event, payload, cause, attempts)
		return
	}
	delay := h.retryPolicy.Delay(attempts)
	if nakErr := delivery.NakWithDelay(delay); nakErr != nil {
		h.logger.Error("failed to schedule accident retry", "event_id", eventID, "error", nakErr)
		return
	}
	h.logger.Warn("accident processing failed; retry scheduled", "event_id", eventID, "attempt", attempts, "delay", delay, "error", cause)
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
	h.logger.Error("message moved to dead letter queue", "event_id", eventID, "attempts", attempts, "error", cause)
}

func fallbackEventID(delivery messaging.Delivery, payload []byte) string {
	info := delivery.Info()
	if info.StreamSequence > 0 {
		return fmt.Sprintf("invalid:%s:%d", info.Consumer, info.StreamSequence)
	}
	sum := sha256.Sum256(payload)
	return "invalid:" + hex.EncodeToString(sum[:])
}
