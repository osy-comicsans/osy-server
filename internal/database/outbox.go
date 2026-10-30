package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (db *DB) ClaimOutboxEvents(ctx context.Context, limit int, lease time.Duration) ([]OutboxEvent, error) {
	if limit < 1 {
		limit = 1
	}
	leaseUntil := time.Now().Add(lease)
	rows, err := db.Pool.Query(ctx, `
		WITH claimed AS (
			SELECT id
			FROM outbox_events
			WHERE (status = 'pending' AND next_attempt_at <= now())
			   OR (status = 'processing' AND locked_until IS NOT NULL AND locked_until < now())
			ORDER BY next_attempt_at, created_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events event
		SET status = 'processing', locked_until = $2, updated_at = now()
		FROM claimed
		WHERE event.id = claimed.id
		RETURNING
			event.id::text, event.aggregate_id, event.event_type, event.subject,
			event.payload, event.status, event.attempts, event.next_attempt_at,
			event.published_at, event.last_error, event.created_at, event.updated_at
	`, limit, leaseUntil)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]OutboxEvent, 0, limit)
	for rows.Next() {
		event, err := scanOutboxEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed outbox events: %w", err)
	}
	return events, nil
}

func (db *DB) MarkOutboxPublished(ctx context.Context, id string) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'published',
			published_at = now(),
			locked_until = NULL,
			last_error = NULL,
			updated_at = now()
		WHERE id = $1 AND status = 'processing'
	`, id)
	if err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) MarkOutboxFailed(ctx context.Context, id string, maxAttempts int, nextAttemptAt time.Time, cause error) (string, int, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var status string
	var attempts int
	err := db.Pool.QueryRow(ctx, `
		UPDATE outbox_events
		SET attempts = attempts + 1,
			status = CASE WHEN attempts + 1 >= $2 THEN 'failed' ELSE 'pending' END,
			next_attempt_at = $3,
			locked_until = NULL,
			last_error = $4,
			updated_at = now()
		WHERE id = $1 AND status = 'processing'
		RETURNING status, attempts
	`, id, maxAttempts, nextAttemptAt, cause.Error()).Scan(&status, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	if err != nil {
		return "", 0, fmt.Errorf("mark outbox event failed: %w", err)
	}
	return status, attempts, nil
}

func (db *DB) ListOutboxEvents(ctx context.Context, filter OutboxFilter) (Page[OutboxEvent], error) {
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
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events"+where, args...).Scan(&total); err != nil {
		return Page[OutboxEvent]{}, fmt.Errorf("count outbox events: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT id::text, aggregate_id, event_type, subject, payload, status, attempts,
			next_attempt_at, published_at, last_error, created_at, updated_at
		FROM outbox_events%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))
	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return Page[OutboxEvent]{}, fmt.Errorf("list outbox events: %w", err)
	}
	defer rows.Close()
	events := make([]OutboxEvent, 0, pageSize)
	for rows.Next() {
		event, err := scanOutboxEvent(rows)
		if err != nil {
			return Page[OutboxEvent]{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return Page[OutboxEvent]{}, fmt.Errorf("iterate outbox events: %w", err)
	}
	return Page[OutboxEvent]{
		Data:       events,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	}, nil
}

func (db *DB) GetOutboxEvent(ctx context.Context, id string) (OutboxEvent, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id::text, aggregate_id, event_type, subject, payload, status, attempts,
			next_attempt_at, published_at, last_error, created_at, updated_at
		FROM outbox_events
		WHERE id = $1
	`, id)
	event, err := scanOutboxEvent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboxEvent{}, ErrNotFound
	}
	return event, err
}

type outboxScanner interface {
	Scan(dest ...any) error
}

func scanOutboxEvent(row outboxScanner) (OutboxEvent, error) {
	var event OutboxEvent
	var payload []byte
	if err := row.Scan(
		&event.ID,
		&event.AggregateID,
		&event.EventType,
		&event.Subject,
		&payload,
		&event.Status,
		&event.Attempts,
		&event.NextAttemptAt,
		&event.PublishedAt,
		&event.LastError,
		&event.CreatedAt,
		&event.UpdatedAt,
	); err != nil {
		return OutboxEvent{}, err
	}
	event.Payload = payload
	return event, nil
}
