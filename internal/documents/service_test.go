package documents

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"slices"
	"testing"

	"osyn/internal/database"
)

type captureStore struct {
	saved    database.NewDocument
	required []string
}

func (s *captureStore) GetUser(_ context.Context, id string) (database.User, error) {
	return database.User{ID: id, OnboardingStatus: "documents_required"}, nil
}

func (s *captureStore) SaveDocument(_ context.Context, document database.NewDocument, requiredTypes []string) error {
	s.saved = document
	s.required = requiredTypes
	return nil
}

func (s *captureStore) ListDocuments(context.Context, string) ([]database.DocumentStatus, error) {
	return nil, nil
}

func TestUploadStoresEncryptedDocument(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	store := &captureStore{}
	service, err := NewService(store, key)
	if err != nil {
		t.Fatal(err)
	}
	file := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("licence-image")...)
	fields := `{"licenseNumber":"A1234567"}`
	if _, err := service.Upload(context.Background(), "user-1", Upload{
		Type:            "driver_license",
		Source:          "scan",
		CaptureLanguage: "fr",
		Fields:          fields,
		File:            file,
	}); err != nil {
		t.Fatal(err)
	}

	saved := store.saved
	if saved.MimeType != "image/jpeg" || saved.SizeBytes != len(file) || !slices.Equal(store.required, Types) {
		t.Fatalf("unexpected document metadata: %+v, required %v", saved, store.required)
	}
	if bytes.Contains(saved.FileCiphertext, []byte("licence-image")) || bytes.Contains(saved.FieldsCiphertext, []byte("A1234567")) {
		t.Fatal("document stored in plaintext")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		ciphertext []byte
		context    string
		want       []byte
	}{
		{name: "file", ciphertext: saved.FileCiphertext, context: "user-1/driver_license/file", want: file},
		{name: "fields", ciphertext: saved.FieldsCiphertext, context: "user-1/driver_license/fields", want: []byte(fields)},
		{name: "file moved to another user", ciphertext: saved.FileCiphertext, context: "user-2/driver_license/file"},
		{name: "file moved to the fields column", ciphertext: saved.FileCiphertext, context: "user-1/driver_license/fields"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plaintext, err := aead.Open(nil, nil, test.ciphertext, []byte(test.context))
			if test.want == nil {
				if err == nil {
					t.Fatal("decrypted with the wrong context")
				}
				return
			}
			if err != nil || !bytes.Equal(plaintext, test.want) {
				t.Fatalf("got %q, error %v", plaintext, err)
			}
		})
	}
}
