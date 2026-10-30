package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (db *DB) ListMessages(ctx context.Context, filter MessageFilter) (Page[SMSMessage], error) {
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var conditions []string
	var args []any
	if filter.Sender != "" {
		args = append(args, filter.Sender)
		conditions = append(conditions, fmt.Sprintf("sender = $%d", len(args)))
	}
	if filter.From != nil {
		args = append(args, *filter.From)
		conditions = append(conditions, fmt.Sprintf("received_at >= $%d", len(args)))
	}
	if filter.To != nil {
		args = append(args, *filter.To)
		conditions = append(conditions, fmt.Sprintf("received_at <= $%d", len(args)))
	}
	if filter.MessageID != "" {
		args = append(args, filter.MessageID)
		conditions = append(conditions, fmt.Sprintf("(provider_message_id = $%d OR id::text = $%d)", len(args), len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int64
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM sms_messages"+where, args...).Scan(&total); err != nil {
		return Page[SMSMessage]{}, fmt.Errorf("count messages: %w", err)
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT id, provider, provider_message_id, sender, recipient, message, received_at,
			processing_status, processed_at, processing_error, created_at
		FROM sms_messages%s
		ORDER BY received_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))
	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return Page[SMSMessage]{}, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]SMSMessage, 0, pageSize)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return Page[SMSMessage]{}, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return Page[SMSMessage]{}, fmt.Errorf("iterate messages: %w", err)
	}
	return Page[SMSMessage]{
		Data:       messages,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	}, nil
}

func (db *DB) GetMessage(ctx context.Context, id int64) (SMSMessage, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id, provider, provider_message_id, sender, recipient, message, received_at,
			processing_status, processed_at, processing_error, created_at
		FROM sms_messages
		WHERE id = $1
	`, id)
	message, err := scanMessage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return SMSMessage{}, ErrNotFound
	}
	if err != nil {
		return SMSMessage{}, fmt.Errorf("get message: %w", err)
	}
	return message, nil
}

func (db *DB) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	err := db.Pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM sms_messages),
			(SELECT count(*) FROM sms_messages WHERE processing_status = 'processed'),
			(SELECT count(*) FROM outbox_events WHERE status IN ('pending', 'processing')),
			(SELECT count(*) FROM outbox_events WHERE status = 'failed'),
			(SELECT count(*) FROM dead_letters WHERE status = 'unresolved')
	`).Scan(
		&stats.TotalMessages,
		&stats.ProcessedMessages,
		&stats.PendingOutbox,
		&stats.FailedOutbox,
		&stats.DLQCount,
	)
	if err != nil {
		return Stats{}, fmt.Errorf("load stats: %w", err)
	}
	return stats, nil
}

func (db *DB) OperationalMetrics(ctx context.Context) (Metrics, error) {
	var metrics Metrics
	err := db.Pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE processing_status = 'received'),
			count(*) FILTER (WHERE processing_status = 'processed'),
			count(*) FILTER (WHERE processing_status = 'failed')
		FROM sms_messages
	`).Scan(
		&metrics.Messages.Total,
		&metrics.Messages.Received,
		&metrics.Messages.Processed,
		&metrics.Messages.Failed,
	)
	if err != nil {
		return Metrics{}, fmt.Errorf("load message metrics: %w", err)
	}
	err = db.Pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'processing'),
			count(*) FILTER (WHERE status = 'published'),
			count(*) FILTER (WHERE status = 'failed')
		FROM outbox_events
	`).Scan(
		&metrics.Outbox.Pending,
		&metrics.Outbox.Processing,
		&metrics.Outbox.Published,
		&metrics.Outbox.Failed,
	)
	if err != nil {
		return Metrics{}, fmt.Errorf("load outbox metrics: %w", err)
	}
	err = db.Pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'unresolved'),
			count(*) FILTER (WHERE status = 'resolved')
		FROM dead_letters
	`).Scan(&metrics.DLQ.Unresolved, &metrics.DLQ.Resolved)
	if err != nil {
		return Metrics{}, fmt.Errorf("load dlq metrics: %w", err)
	}
	return metrics, nil
}

type messageScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row messageScanner) (SMSMessage, error) {
	var message SMSMessage
	if err := row.Scan(
		&message.ID,
		&message.Provider,
		&message.ProviderMessageID,
		&message.Sender,
		&message.Recipient,
		&message.Message,
		&message.ReceivedAt,
		&message.ProcessingStatus,
		&message.ProcessedAt,
		&message.ProcessingError,
		&message.CreatedAt,
	); err != nil {
		return SMSMessage{}, err
	}
	return message, nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func totalPages(total int64, pageSize int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}
