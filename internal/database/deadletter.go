package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"osyn/internal/messaging"
)

func (db *DB) PersistDeadLetter(ctx context.Context, input DeadLetterInput) (DeadLetter, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return DeadLetter{}, fmt.Errorf("begin dead letter transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	originalMessageID := input.OriginalMessageID
	if originalMessageID == nil && input.ProviderMessageID != "" {
		var messageID int64
		err := tx.QueryRow(ctx, `
			SELECT id FROM sms_messages WHERE provider = $1 AND provider_message_id = $2
		`, input.Provider, input.ProviderMessageID).Scan(&messageID)
		if err == nil {
			originalMessageID = &messageID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return DeadLetter{}, fmt.Errorf("find dead letter message: %w", err)
		}
	}

	payload := input.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO dead_letters (
			id, original_message_id, event_id, payload, error, delivery_attempts
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (event_id) DO UPDATE SET
			error = EXCLUDED.error,
			delivery_attempts = GREATEST(dead_letters.delivery_attempts, EXCLUDED.delivery_attempts)
		RETURNING id::text, original_message_id, event_id, payload, error, delivery_attempts,
			status, replay_count, last_replay_error, last_replayed_at, created_at, resolved_at
	`, uuid.NewString(), originalMessageID, input.EventID, payload, input.Error, input.DeliveryAttempts)
	deadLetter, err := scanDeadLetter(row)
	if err != nil {
		return DeadLetter{}, fmt.Errorf("persist dead letter: %w", err)
	}

	if input.Provider != "" && input.ProviderMessageID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE sms_messages
			SET processing_status = 'failed',
				processing_error = $3,
				processed_at = now()
			WHERE provider = $1 AND provider_message_id = $2
		`, input.Provider, input.ProviderMessageID, input.Error); err != nil {
			return DeadLetter{}, fmt.Errorf("mark message processing failed: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return DeadLetter{}, fmt.Errorf("commit dead letter transaction: %w", err)
	}
	return deadLetter, nil
}

func (db *DB) ListDeadLetters(ctx context.Context, filter DeadLetterFilter) (Page[DeadLetter], error) {
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var conditions []string
	var args []any
	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int64
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM dead_letters"+where, args...).Scan(&total); err != nil {
		return Page[DeadLetter]{}, fmt.Errorf("count dead letters: %w", err)
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT id::text, original_message_id, event_id, payload, error, delivery_attempts,
			status, replay_count, last_replay_error, last_replayed_at, created_at, resolved_at
		FROM dead_letters%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))
	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return Page[DeadLetter]{}, fmt.Errorf("list dead letters: %w", err)
	}
	defer rows.Close()
	letters := make([]DeadLetter, 0, pageSize)
	for rows.Next() {
		letter, err := scanDeadLetter(rows)
		if err != nil {
			return Page[DeadLetter]{}, fmt.Errorf("scan dead letter: %w", err)
		}
		letters = append(letters, letter)
	}
	if err := rows.Err(); err != nil {
		return Page[DeadLetter]{}, fmt.Errorf("iterate dead letters: %w", err)
	}
	return Page[DeadLetter]{
		Data:       letters,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	}, nil
}

func (db *DB) GetDeadLetter(ctx context.Context, id string) (DeadLetter, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id::text, original_message_id, event_id, payload, error, delivery_attempts,
			status, replay_count, last_replay_error, last_replayed_at, created_at, resolved_at
		FROM dead_letters
		WHERE id = $1
	`, id)
	letter, err := scanDeadLetter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeadLetter{}, ErrNotFound
	}
	if err != nil {
		return DeadLetter{}, fmt.Errorf("get dead letter: %w", err)
	}
	return letter, nil
}

func (db *DB) ResolveDeadLetter(ctx context.Context, id string) (DeadLetter, error) {
	row := db.Pool.QueryRow(ctx, `
		UPDATE dead_letters
		SET status = 'resolved', resolved_at = now()
		WHERE id = $1 AND status = 'unresolved'
		RETURNING id::text, original_message_id, event_id, payload, error, delivery_attempts,
			status, replay_count, last_replay_error, last_replayed_at, created_at, resolved_at
	`, id)
	letter, err := scanDeadLetter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := db.GetDeadLetter(ctx, id)
		if getErr != nil {
			return DeadLetter{}, getErr
		}
		if existing.Status == "resolved" {
			return existing, nil
		}
		return DeadLetter{}, ErrNotFound
	}
	if err != nil {
		return DeadLetter{}, fmt.Errorf("resolve dead letter: %w", err)
	}
	return letter, nil
}

func (db *DB) ReplayDeadLetter(ctx context.Context, id, subject string) (string, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin replay transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		SELECT id::text, original_message_id, event_id, payload, error, delivery_attempts,
			status, replay_count, last_replay_error, last_replayed_at, created_at, resolved_at
		FROM dead_letters
		WHERE id = $1
		FOR UPDATE
	`, id)
	letter, err := scanDeadLetter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get dead letter for replay: %w", err)
	}
	if letter.Status == "resolved" {
		return "", ErrDLQResolved
	}

	var event messaging.SMSEvent
	if err := json.Unmarshal(letter.Payload, &event); err != nil {
		return "", fmt.Errorf("decode dead letter payload: %w", err)
	}
	eventID := uuid.NewString()
	event.EventID = eventID
	event.ReplayOf = letter.EventID
	if event.MessageID == 0 && letter.OriginalMessageID != nil {
		event.MessageID = *letter.OriginalMessageID
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("encode replay payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (
			id, aggregate_id, event_type, subject, payload
		) VALUES ($1, $2, 'sms.replay', $3, $4)
	`, eventID, event.ProviderMessageID, subject, payload); err != nil {
		return "", fmt.Errorf("insert replay outbox event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE dead_letters
		SET replay_count = replay_count + 1,
			last_replay_error = NULL,
			last_replayed_at = now()
		WHERE id = $1
	`, id); err != nil {
		return "", fmt.Errorf("record replay attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit replay transaction: %w", err)
	}
	return eventID, nil
}

func (db *DB) RecordReplayFailure(ctx context.Context, id string, cause error) error {
	if _, err := db.Pool.Exec(ctx, `
		UPDATE dead_letters
		SET replay_count = replay_count + 1,
			last_replay_error = $2,
			last_replayed_at = now()
		WHERE id = $1
	`, id, cause.Error()); err != nil {
		return fmt.Errorf("record replay failure: %w", err)
	}
	return nil
}

type deadLetterScanner interface {
	Scan(dest ...any) error
}

func scanDeadLetter(row deadLetterScanner) (DeadLetter, error) {
	var letter DeadLetter
	var payload []byte
	if err := row.Scan(
		&letter.ID,
		&letter.OriginalMessageID,
		&letter.EventID,
		&payload,
		&letter.Error,
		&letter.DeliveryAttempts,
		&letter.Status,
		&letter.ReplayCount,
		&letter.LastReplayError,
		&letter.LastReplayedAt,
		&letter.CreatedAt,
		&letter.ResolvedAt,
	); err != nil {
		return DeadLetter{}, err
	}
	letter.Payload = payload
	return letter, nil
}
