package database

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrDLQResolved = errors.New("dead letter is already resolved")
)

type SMSMessage struct {
	ID                int64      `json:"id"`
	Provider          string     `json:"provider"`
	ProviderMessageID string     `json:"provider_message_id"`
	Sender            string     `json:"sender"`
	Recipient         string     `json:"recipient"`
	Message           string     `json:"message"`
	ReceivedAt        time.Time  `json:"received_at"`
	ProcessingStatus  string     `json:"processing_status"`
	ProcessedAt       *time.Time `json:"processed_at,omitempty"`
	ProcessingError   *string    `json:"processing_error,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type OutboxEvent struct {
	ID            string          `json:"id"`
	AggregateID   string          `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	Subject       string          `json:"subject"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	PublishedAt   *time.Time      `json:"published_at,omitempty"`
	LastError     *string         `json:"last_error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type DeadLetter struct {
	ID                string          `json:"id"`
	OriginalMessageID *int64          `json:"original_message_id,omitempty"`
	EventID           string          `json:"event_id"`
	Payload           json.RawMessage `json:"payload"`
	Error             string          `json:"error"`
	DeliveryAttempts  int             `json:"delivery_attempts"`
	Status            string          `json:"status"`
	ReplayCount       int             `json:"replay_count"`
	LastReplayError   *string         `json:"last_replay_error,omitempty"`
	LastReplayedAt    *time.Time      `json:"last_replayed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	ResolvedAt        *time.Time      `json:"resolved_at,omitempty"`
}

type IncomingMessage struct {
	ProviderMessageID string
	Sender            string
	Recipient         string
	Message           string
	ReceivedAt        time.Time
}

type IngestResult struct {
	LastReceivedID int64 `json:"last_received_id"`
	Inserted       int   `json:"inserted"`
	Duplicates     int   `json:"duplicates"`
}

type MessageFilter struct {
	Page      int
	PageSize  int
	Sender    string
	From      *time.Time
	To        *time.Time
	MessageID string
}

type OutboxFilter struct {
	Page     int
	PageSize int
	Status   string
}

type DeadLetterFilter struct {
	Page     int
	PageSize int
	Status   string
}

type Page[T any] struct {
	Data       []T   `json:"data"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type Stats struct {
	TotalMessages     int64 `json:"total_messages"`
	ProcessedMessages int64 `json:"processed_messages"`
	PendingOutbox     int64 `json:"pending_outbox_events"`
	FailedOutbox      int64 `json:"failed_outbox_events"`
	DLQCount          int64 `json:"dlq_count"`
}

type Metrics struct {
	Messages MessageMetrics `json:"messages"`
	Outbox   OutboxMetrics  `json:"outbox"`
	DLQ      DLQMetrics     `json:"dlq"`
}

type MessageMetrics struct {
	Total     int64 `json:"total"`
	Received  int64 `json:"received"`
	Processed int64 `json:"processed"`
	Failed    int64 `json:"failed"`
}

type OutboxMetrics struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Published  int64 `json:"published"`
	Failed     int64 `json:"failed"`
}

type DLQMetrics struct {
	Unresolved int64 `json:"unresolved"`
	Resolved   int64 `json:"resolved"`
}

type DeadLetterInput struct {
	OriginalMessageID *int64
	EventID           string
	Payload           json.RawMessage
	Error             string
	DeliveryAttempts  int
	Provider          string
	ProviderMessageID string
}
