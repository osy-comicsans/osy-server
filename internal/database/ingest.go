package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"osyn/internal/messaging"
)

func (db *DB) LastReceivedID(ctx context.Context, provider string) (int64, error) {
	var lastReceivedID int64
	err := db.Pool.QueryRow(ctx, `
		SELECT last_received_id
		FROM ingestion_checkpoints
		WHERE provider = $1
	`, provider).Scan(&lastReceivedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get ingestion checkpoint: %w", err)
	}
	return lastReceivedID, nil
}

func (db *DB) IngestBatch(ctx context.Context, provider, subject string, messages []IncomingMessage) (IngestResult, error) {
	var result IngestResult
	if len(messages) == 0 {
		return result, nil
	}

	var highest int64
	for i, message := range messages {
		id, err := strconv.ParseInt(message.ProviderMessageID, 10, 64)
		if err != nil {
			return result, fmt.Errorf("invalid provider message id %q: %w", message.ProviderMessageID, err)
		}
		if i == 0 || id > highest {
			highest = id
		}
	}
	result.LastReceivedID = highest

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return IngestResult{}, fmt.Errorf("begin ingest transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, message := range messages {
		receivedAt := message.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = time.Now().UTC()
		}
		var messageID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO sms_messages (
				provider, provider_message_id, sender, recipient, message, received_at
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (provider, provider_message_id) DO NOTHING
			RETURNING id
		`, provider, message.ProviderMessageID, message.Sender, message.Recipient, message.Message, receivedAt).Scan(&messageID)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Duplicates++
			continue
		}
		if err != nil {
			return IngestResult{}, fmt.Errorf("insert sms message %s: %w", message.ProviderMessageID, err)
		}

		eventID := uuid.NewString()
		payload, err := json.Marshal(messaging.SMSEvent{
			EventID:           eventID,
			MessageID:         messageID,
			Provider:          provider,
			ProviderMessageID: message.ProviderMessageID,
			Sender:            message.Sender,
			Recipient:         message.Recipient,
			Message:           message.Message,
			ReceivedAt:        receivedAt,
		})
		if err != nil {
			return IngestResult{}, fmt.Errorf("encode outbox payload: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO outbox_events (
				id, aggregate_id, event_type, subject, payload
			) VALUES ($1, $2, 'sms.received', $3, $4)
		`, eventID, message.ProviderMessageID, subject, payload); err != nil {
			return IngestResult{}, fmt.Errorf("insert outbox event: %w", err)
		}
		result.Inserted++
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO ingestion_checkpoints (provider, last_received_id, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (provider) DO UPDATE SET
			last_received_id = GREATEST(ingestion_checkpoints.last_received_id, EXCLUDED.last_received_id),
			updated_at = now()
	`, provider, highest); err != nil {
		return IngestResult{}, fmt.Errorf("update ingestion checkpoint: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, fmt.Errorf("commit ingest transaction: %w", err)
	}
	return result, nil
}
