package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"osyn/internal/accident"
	"osyn/internal/messaging"
)

func (db *DB) ProcessAccidentEvent(ctx context.Context, event messaging.SMSEvent, report accident.Report, alerts []accident.Alert) ([]EmergencyNotification, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin accident processing transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	receivedAt := event.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	var smsMessageID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO sms_messages (
			provider, provider_message_id, sender, recipient, message, received_at,
			processing_status, processed_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'processed', now())
		ON CONFLICT (provider, provider_message_id) DO UPDATE SET
			processing_status = 'processed',
			processed_at = COALESCE(sms_messages.processed_at, now()),
			processing_error = NULL
		RETURNING id
	`, event.Provider, event.ProviderMessageID, event.Sender, event.Recipient, event.Message, receivedAt).Scan(&smsMessageID); err != nil {
		return nil, fmt.Errorf("persist accident message: %w", err)
	}

	var reportID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO accident_reports (
			event_id, sms_message_id, provider_message_id, sender,
			latitude, longitude, damage_percentage, radius_meters, raw_message
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (event_id) DO UPDATE SET
			sms_message_id = EXCLUDED.sms_message_id,
			latitude = EXCLUDED.latitude,
			longitude = EXCLUDED.longitude,
			damage_percentage = EXCLUDED.damage_percentage,
			radius_meters = EXCLUDED.radius_meters,
			raw_message = EXCLUDED.raw_message
		RETURNING id
	`, event.EventID, smsMessageID, event.ProviderMessageID, event.Sender,
		report.Latitude, report.Longitude, report.DamagePercentage, report.RadiusMeters, event.Message).Scan(&reportID); err != nil {
		return nil, fmt.Errorf("persist accident report: %w", err)
	}

	for _, alert := range alerts {
		if _, err := tx.Exec(ctx, `
			INSERT INTO emergency_notifications (
				id, accident_report_id, service, target_phone, message
			) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (accident_report_id, service) DO NOTHING
		`, uuid.NewString(), reportID, alert.Service, alert.Phone, alert.Message); err != nil {
			return nil, fmt.Errorf("create emergency notification: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, provider, provider_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING
	`, event.EventID, event.Provider, event.ProviderMessageID); err != nil {
		return nil, fmt.Errorf("record accident event: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, accident_report_id, service, target_phone, message, status,
			attempts, last_error, twilio_sid, sent_at, created_at, updated_at
		FROM emergency_notifications
		WHERE accident_report_id = $1 AND status <> 'sent'
		ORDER BY created_at
	`, reportID)
	if err != nil {
		return nil, fmt.Errorf("load pending emergency notifications: %w", err)
	}
	notifications := make([]EmergencyNotification, 0, len(alerts))
	for rows.Next() {
		notification, err := scanEmergencyNotification(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan emergency notification: %w", err)
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate emergency notifications: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit accident processing: %w", err)
	}
	return notifications, nil
}

func (db *DB) MarkEmergencyNotificationSent(ctx context.Context, id, twilioSID string) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE emergency_notifications
		SET status = 'sent',
			attempts = attempts + 1,
			last_error = NULL,
			twilio_sid = $2,
			sent_at = now(),
			updated_at = now()
		WHERE id = $1
	`, id, twilioSID)
	if err != nil {
		return fmt.Errorf("mark emergency notification sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) MarkEmergencyNotificationFailed(ctx context.Context, id string, cause error) error {
	if _, err := db.Pool.Exec(ctx, `
		UPDATE emergency_notifications
		SET status = 'failed',
			attempts = attempts + 1,
			last_error = $2,
			updated_at = now()
		WHERE id = $1
	`, id, cause.Error()); err != nil {
		return fmt.Errorf("mark emergency notification failed: %w", err)
	}
	return nil
}

type notificationScanner interface {
	Scan(dest ...any) error
}

func scanEmergencyNotification(row notificationScanner) (EmergencyNotification, error) {
	var notification EmergencyNotification
	if err := row.Scan(
		&notification.ID,
		&notification.AccidentReportID,
		&notification.Service,
		&notification.TargetPhone,
		&notification.Message,
		&notification.Status,
		&notification.Attempts,
		&notification.LastError,
		&notification.TwilioSID,
		&notification.SentAt,
		&notification.CreatedAt,
		&notification.UpdatedAt,
	); err != nil {
		return EmergencyNotification{}, err
	}
	return notification, nil
}
