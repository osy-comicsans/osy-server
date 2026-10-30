package messaging

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type SMSEvent struct {
	EventID           string    `json:"event_id"`
	ReplayOf          string    `json:"replay_of,omitempty"`
	MessageID         int64     `json:"message_id,omitempty"`
	Provider          string    `json:"provider"`
	ProviderMessageID string    `json:"provider_message_id"`
	Sender            string    `json:"sender"`
	Recipient         string    `json:"recipient"`
	Message           string    `json:"message"`
	ReceivedAt        time.Time `json:"received_at"`
}

type DeadLetterEvent struct {
	DeadLetterID      string          `json:"dead_letter_id"`
	EventID           string          `json:"event_id"`
	OriginalMessageID *int64          `json:"original_message_id,omitempty"`
	Error             string          `json:"error"`
	DeliveryAttempts  int             `json:"delivery_attempts"`
	Payload           json.RawMessage `json:"payload"`
	OccurredAt        time.Time       `json:"occurred_at"`
}

func DecodeSMSEvent(data []byte) (SMSEvent, error) {
	var event SMSEvent
	if len(data) == 0 {
		return event, errors.New("empty event payload")
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return event, err
	}
	if err := ValidateSMSEvent(event); err != nil {
		return event, err
	}
	return event, nil
}

func ValidateSMSEvent(event SMSEvent) error {
	if strings.TrimSpace(event.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(event.ProviderMessageID) == "" {
		return errors.New("provider_message_id is required")
	}
	if strings.TrimSpace(event.EventID) == "" {
		return errors.New("event_id is required")
	}
	if event.ReceivedAt.IsZero() {
		return errors.New("received_at is required")
	}
	return nil
}
