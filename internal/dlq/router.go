package dlq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"osyn/internal/database"
	"osyn/internal/messaging"
)

type RouterStore interface {
	PersistDeadLetter(ctx context.Context, input database.DeadLetterInput) (database.DeadLetter, error)
}

type Publisher interface {
	Publish(ctx context.Context, subject, messageID string, data []byte) error
}

type Router struct {
	store     RouterStore
	publisher Publisher
	subject   string
}

func NewRouter(store RouterStore, publisher Publisher, subject string) *Router {
	return &Router{store: store, publisher: publisher, subject: subject}
}

func (r *Router) Route(ctx context.Context, eventID string, event messaging.SMSEvent, payload []byte, cause error, attempts int) error {
	if eventID == "" {
		eventID = event.EventID
	}
	if event.EventID == "" {
		event.EventID = eventID
	}
	safe := validPayload(payload)
	var originalMessageID *int64
	if event.MessageID > 0 {
		messageID := event.MessageID
		originalMessageID = &messageID
	}
	errorMessage := "unknown error"
	if cause != nil {
		errorMessage = cause.Error()
	}
	letter, err := r.store.PersistDeadLetter(ctx, database.DeadLetterInput{
		OriginalMessageID: originalMessageID,
		EventID:           eventID,
		Payload:           safe,
		Error:             errorMessage,
		DeliveryAttempts:  attempts,
		Provider:          event.Provider,
		ProviderMessageID: event.ProviderMessageID,
	})
	if err != nil {
		return fmt.Errorf("persist dead letter: %w", err)
	}
	deadLetterEvent, err := json.Marshal(messaging.DeadLetterEvent{
		DeadLetterID:      letter.ID,
		EventID:           letter.EventID,
		OriginalMessageID: letter.OriginalMessageID,
		Error:             letter.Error,
		DeliveryAttempts:  letter.DeliveryAttempts,
		Payload:           json.RawMessage(letter.Payload),
		OccurredAt:        time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("encode dead letter event: %w", err)
	}
	if err := r.publisher.Publish(ctx, r.subject, letter.ID, deadLetterEvent); err != nil {
		return fmt.Errorf("publish dead letter: %w", err)
	}
	return nil
}

func validPayload(payload []byte) []byte {
	if len(payload) > 0 && json.Valid(payload) {
		return payload
	}
	wrapped, err := json.Marshal(map[string]string{"raw": string(payload)})
	if err != nil {
		return []byte("{}")
	}
	return wrapped
}
