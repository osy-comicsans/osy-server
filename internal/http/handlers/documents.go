package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"osyn/internal/auth"
	"osyn/internal/database"
	"osyn/internal/documents"
)

const (
	maxDocumentFormBytes  = documents.MaxFileBytes + 1<<20
	maxDocumentValueBytes = 64 << 10
)

type DocumentService interface {
	Overview(ctx context.Context, userID string) (documents.Overview, error)
	Upload(ctx context.Context, userID string, upload documents.Upload) (documents.Overview, error)
}

type Documents struct {
	service DocumentService
	logger  *slog.Logger
}

func NewDocuments(service DocumentService, logger *slog.Logger) *Documents {
	return &Documents{service: service, logger: logger}
}

func (d *Documents) List(w http.ResponseWriter, r *http.Request) {
	overview, err := d.service.Overview(r.Context(), auth.UserID(r.Context()))
	d.respond(w, http.StatusOK, overview, err, "list documents")
}

func (d *Documents) Upload(w http.ResponseWriter, r *http.Request) {
	upload, err := readUpload(w, r)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeFieldErrors(w, http.StatusUnprocessableEntity, "validation_failed", "some fields are invalid",
			map[string]string{"file": "File must be 10 MB or smaller"})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be multipart/form-data")
		return
	}
	if fields := upload.Validate(); len(fields) > 0 {
		writeFieldErrors(w, http.StatusUnprocessableEntity, "validation_failed", "some fields are invalid", fields)
		return
	}
	overview, err := d.service.Upload(r.Context(), auth.UserID(r.Context()), upload)
	d.respond(w, http.StatusCreated, overview, err, "upload document")
}

func (d *Documents) respond(w http.ResponseWriter, status int, overview documents.Overview, err error, operation string) {
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a valid access token is required")
		return
	}
	if err != nil {
		d.logger.Error("http handler failed", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	writeJSON(w, status, overview)
}

// readUpload streams the form, so the document is never written to a temporary file.
func readUpload(w http.ResponseWriter, r *http.Request) (documents.Upload, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentFormBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return documents.Upload{}, err
	}
	var upload documents.Upload
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return upload, nil
		}
		if err != nil {
			return documents.Upload{}, err
		}
		limit := int64(maxDocumentValueBytes)
		if part.FormName() == "file" {
			limit = documents.MaxFileBytes + 1
		}
		value, err := io.ReadAll(io.LimitReader(part, limit))
		if err != nil {
			return documents.Upload{}, err
		}
		switch part.FormName() {
		case "file":
			upload.File = value
		case "type":
			upload.Type = string(value)
		case "source":
			upload.Source = string(value)
		case "captureLanguage":
			upload.CaptureLanguage = string(value)
		case "fields":
			upload.Fields = string(value)
		}
	}
}
