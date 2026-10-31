package database

import (
	"context"
	"fmt"
)

// SaveDocument replaces the user's document of that type and queues it for
// review. Onboarding becomes complete once every required type is pending or
// verified, and falls back if one is missing or rejected.
func (db *DB) SaveDocument(ctx context.Context, document NewDocument, requiredTypes []string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save document transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_documents (
			user_id, type, source, capture_language, mime_type, size_bytes, file_ciphertext, fields_ciphertext
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, type) DO UPDATE SET
			status = 'pending',
			rejection_reason = NULL,
			source = EXCLUDED.source,
			capture_language = EXCLUDED.capture_language,
			mime_type = EXCLUDED.mime_type,
			size_bytes = EXCLUDED.size_bytes,
			file_ciphertext = EXCLUDED.file_ciphertext,
			fields_ciphertext = EXCLUDED.fields_ciphertext,
			updated_at = now()
	`, document.UserID, document.Type, document.Source, document.CaptureLanguage, document.MimeType,
		document.SizeBytes, document.FileCiphertext, document.FieldsCiphertext); err != nil {
		return fmt.Errorf("save document: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET
			onboarding_status = CASE WHEN (
				SELECT count(*) FROM user_documents
				WHERE user_id = $1 AND type = ANY($2::text[]) AND status IN ('pending', 'verified')
			) = cardinality($2::text[]) THEN 'complete' ELSE 'documents_required' END,
			updated_at = now()
		WHERE id = $1
	`, document.UserID, requiredTypes); err != nil {
		return fmt.Errorf("update onboarding status: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit save document transaction: %w", err)
	}
	return nil
}

func (db *DB) ListDocuments(ctx context.Context, userID string) ([]DocumentStatus, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT type, status, rejection_reason, updated_at
		FROM user_documents
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()
	var documents []DocumentStatus
	for rows.Next() {
		var document DocumentStatus
		if err := rows.Scan(&document.Type, &document.Status, &document.RejectionReason, &document.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}
	return documents, nil
}
