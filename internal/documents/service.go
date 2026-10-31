package documents

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"time"

	"osyn/internal/database"
)

type Store interface {
	GetUser(ctx context.Context, id string) (database.User, error)
	SaveDocument(ctx context.Context, document database.NewDocument, requiredTypes []string) error
	ListDocuments(ctx context.Context, userID string) ([]database.DocumentStatus, error)
}

type Document struct {
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	RejectionReason *string    `json:"rejectionReason"`
	UpdatedAt       *time.Time `json:"updatedAt"`
}

type Overview struct {
	Documents        []Document `json:"documents"`
	OnboardingStatus string     `json:"onboardingStatus"`
}

type Service struct {
	store Store
	aead  cipher.AEAD
}

// NewService encrypts documents with AES-256-GCM, so key must be 32 bytes.
func NewService(store Store, key []byte) (*Service, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create document cipher: %w", err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("create document cipher: %w", err)
	}
	return &Service{store: store, aead: aead}, nil
}

func (s *Service) Upload(ctx context.Context, userID string, upload Upload) (Overview, error) {
	if err := s.store.SaveDocument(ctx, database.NewDocument{
		UserID:           userID,
		Type:             upload.Type,
		Source:           upload.Source,
		CaptureLanguage:  upload.CaptureLanguage,
		MimeType:         upload.MimeType(),
		SizeBytes:        len(upload.File),
		FileCiphertext:   s.aead.Seal(nil, nil, upload.File, associatedData(userID, upload.Type, "file")),
		FieldsCiphertext: s.aead.Seal(nil, nil, []byte(upload.Fields), associatedData(userID, upload.Type, "fields")),
	}, Types); err != nil {
		return Overview{}, err
	}
	return s.Overview(ctx, userID)
}

func (s *Service) Overview(ctx context.Context, userID string) (Overview, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	stored, err := s.store.ListDocuments(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	documents := make([]Document, 0, len(Types))
	for _, documentType := range Types {
		document := Document{Type: documentType, Status: "missing"}
		for _, row := range stored {
			if row.Type == documentType {
				updatedAt := row.UpdatedAt
				document = Document{Type: row.Type, Status: row.Status, RejectionReason: row.RejectionReason, UpdatedAt: &updatedAt}
			}
		}
		documents = append(documents, document)
	}
	return Overview{Documents: documents, OnboardingStatus: user.OnboardingStatus}, nil
}

// Binding each ciphertext to its owner, type, and column stops a stored
// value from decrypting after being copied to another row or column.
func associatedData(userID, documentType, column string) []byte {
	return []byte(userID + "/" + documentType + "/" + column)
}
