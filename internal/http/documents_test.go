package http_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"osyn/internal/database"
	"osyn/internal/documents"
)

var (
	jpegFile = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 64)...)
	pdfFile  = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n")
)

func (s *fakeStore) SaveDocument(_ context.Context, document database.NewDocument, requiredTypes []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.documents == nil {
		s.documents = make(map[string]map[string]database.DocumentStatus)
	}
	if s.documents[document.UserID] == nil {
		s.documents[document.UserID] = make(map[string]database.DocumentStatus)
	}
	s.documents[document.UserID][document.Type] = database.DocumentStatus{Type: document.Type, Status: "pending", UpdatedAt: time.Now()}
	status := "complete"
	for _, required := range requiredTypes {
		if stored, ok := s.documents[document.UserID][required]; !ok || stored.Status == "rejected" {
			status = "documents_required"
		}
	}
	for email, stored := range s.users {
		if stored.user.ID == document.UserID {
			stored.user.OnboardingStatus = status
			s.users[email] = stored
		}
	}
	return nil
}

func (s *fakeStore) ListDocuments(_ context.Context, userID string) ([]database.DocumentStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stored []database.DocumentStatus
	for _, document := range s.documents[userID] {
		stored = append(stored, document)
	}
	return stored, nil
}

func signedIn(t *testing.T, server *httptest.Server) string {
	t.Helper()
	registered := call(t, server, http.MethodPost, "/api/v1/auth/register", "", registration())
	accessToken, _ := registered.body["accessToken"].(string)
	if accessToken == "" {
		t.Fatalf("register: status %d, body %v", registered.status, registered.body)
	}
	return accessToken
}

func documentForm(documentType string) map[string]string {
	return map[string]string{
		"type":            documentType,
		"source":          "scan",
		"captureLanguage": "fr",
		"fields":          `{"licenseNumber":"A1234567","lastName":"Benali"}`,
	}
}

func uploadDocument(t *testing.T, server *httptest.Server, token string, values map[string]string, file []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range values {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		part, err := form.CreateFormFile("file", "document")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/me/documents", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	return send(t, server, request)
}

func statuses(t *testing.T, got response) []string {
	t.Helper()
	listed, _ := got.body["documents"].([]any)
	var result []string
	for _, item := range listed {
		document, _ := item.(map[string]any)
		result = append(result, document["type"].(string)+"="+document["status"].(string))
	}
	return result
}

func TestDocumentOnboarding(t *testing.T) {
	server := newServer(t)
	token := signedIn(t, server)

	steps := []struct {
		name       string
		upload     string
		file       []byte
		wantStatus []string
		wantStage  string
	}{
		{name: "nothing uploaded", wantStatus: []string{"driver_license=missing", "vehicle_registration=missing"}, wantStage: "documents_required"},
		{name: "licence uploaded", upload: "driver_license", file: jpegFile, wantStatus: []string{"driver_license=pending", "vehicle_registration=missing"}, wantStage: "documents_required"},
		{name: "registration uploaded", upload: "vehicle_registration", file: pdfFile, wantStatus: []string{"driver_license=pending", "vehicle_registration=pending"}, wantStage: "complete"},
	}
	for _, step := range steps {
		got := call(t, server, http.MethodGet, "/api/v1/me/documents", token, nil)
		wantCode := http.StatusOK
		if step.upload != "" {
			got = uploadDocument(t, server, token, documentForm(step.upload), step.file)
			wantCode = http.StatusCreated
		}
		if got.status != wantCode {
			t.Fatalf("%s: status %d, body %v", step.name, got.status, got.body)
		}
		if got := strings.Join(statuses(t, got), ","); got != strings.Join(step.wantStatus, ",") {
			t.Fatalf("%s: documents %s, want %v", step.name, got, step.wantStatus)
		}
		if got.body["onboardingStatus"] != step.wantStage {
			t.Fatalf("%s: onboarding %v, want %s", step.name, got.body["onboardingStatus"], step.wantStage)
		}
	}

	me := call(t, server, http.MethodGet, "/api/v1/me", token, nil)
	if me.body["onboardingStatus"] != "complete" {
		t.Fatalf("me: onboarding %v", me.body["onboardingStatus"])
	}
}

func TestDocumentUploadValidation(t *testing.T) {
	tests := []struct {
		name   string
		change map[string]string
		file   []byte
		want   string
	}{
		{name: "unknown type", change: map[string]string{"type": "passport"}, file: jpegFile, want: "type"},
		{name: "unknown source", change: map[string]string{"source": "camera"}, file: jpegFile, want: "source"},
		{name: "unsupported language", change: map[string]string{"captureLanguage": "en"}, file: jpegFile, want: "captureLanguage"},
		{name: "fields not JSON", change: map[string]string{"fields": "licence A1234567"}, file: jpegFile, want: "fields"},
		{name: "fields with a number", change: map[string]string{"fields": `{"nin":123}`}, file: jpegFile, want: "fields"},
		{name: "fields with an invalid key", change: map[string]string{"fields": `{"licence number":"A1"}`}, file: jpegFile, want: "fields"},
		{name: "fields value too long", change: map[string]string{"fields": `{"nin":"` + strings.Repeat("1", 201) + `"}`}, file: jpegFile, want: "fields"},
		{name: "text file", file: []byte("not a document"), want: "file"},
		{name: "missing file", want: "file"},
		{name: "file over 10 MB", file: append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, documents.MaxFileBytes)...), want: "file"},
		{name: "body over the form limit", file: make([]byte, documents.MaxFileBytes+2<<20), want: "file"},
	}
	server := newServer(t)
	token := signedIn(t, server)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			form := documentForm("driver_license")
			for key, value := range test.change {
				form[key] = value
			}
			got := uploadDocument(t, server, token, form, test.file)
			if got.status != http.StatusUnprocessableEntity || got.errorField("code") != "validation_failed" {
				t.Fatalf("status %d, body %v", got.status, got.body)
			}
			if fields := got.fields(); len(fields) != 1 || fields[test.want] == nil {
				t.Fatalf("got fields %v, want only %s", fields, test.want)
			}
		})
	}
}

func TestDocumentUploadNeedsMultipart(t *testing.T) {
	server := newServer(t)
	token := signedIn(t, server)
	got := call(t, server, http.MethodPost, "/api/v1/me/documents", token, map[string]string{"type": "driver_license"})
	if got.status != http.StatusBadRequest || got.errorField("code") != "invalid_request" {
		t.Fatalf("status %d, body %v", got.status, got.body)
	}
}
