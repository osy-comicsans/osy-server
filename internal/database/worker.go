package database

import (
	"context"
	"fmt"
	"time"

	"osyn/internal/messaging"
)

func (db *DB) ProcessSMSEvent(ctx context.Context, event messaging.SMSEvent) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin processing transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var alreadyProcessed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM processed_events WHERE event_id = $1)
	`, event.EventID).Scan(&alreadyProcessed); err != nil {
		return fmt.Errorf("check processed event: %w", err)
	}
	if alreadyProcessed {
		return tx.Commit(ctx)
	}

	receivedAt := event.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sms_messages (
			provider, provider_message_id, sender, recipient, message, received_at,
			processing_status, processed_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'processed', now())
		ON CONFLICT (provider, provider_message_id) DO UPDATE SET
			processing_status = 'processed',
			processed_at = COALESCE(sms_messages.processed_at, now()),
			processing_error = NULL
	`, event.Provider, event.ProviderMessageID, event.Sender, event.Recipient, event.Message, receivedAt); err != nil {
		return fmt.Errorf("persist processing result: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, provider, provider_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING
	`, event.EventID, event.Provider, event.ProviderMessageID); err != nil {
		return fmt.Errorf("record processed event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit processing transaction: %w", err)
	}
	return nil
}
