package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"osyn/internal/database"
	"osyn/internal/observability"
)

type Store interface {
	ListMessages(ctx context.Context, filter database.MessageFilter) (database.Page[database.SMSMessage], error)
	GetMessage(ctx context.Context, id int64) (database.SMSMessage, error)
	Stats(ctx context.Context) (database.Stats, error)
	OperationalMetrics(ctx context.Context) (database.Metrics, error)
	ListOutboxEvents(ctx context.Context, filter database.OutboxFilter) (database.Page[database.OutboxEvent], error)
	GetOutboxEvent(ctx context.Context, id string) (database.OutboxEvent, error)
	ListDeadLetters(ctx context.Context, filter database.DeadLetterFilter) (database.Page[database.DeadLetter], error)
	GetDeadLetter(ctx context.Context, id string) (database.DeadLetter, error)
}

type DeadLetterManager interface {
	Replay(ctx context.Context, id string) (string, error)
	Resolve(ctx context.Context, id string) (database.DeadLetter, error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

type Handler struct {
	store    Store
	dlq      DeadLetterManager
	database Pinger
	nats     ReadinessChecker
	metrics  *observability.Metrics
	logger   *slog.Logger
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type healthResponse struct {
	Status string `json:"status"`
}

type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

type replayResponse struct {
	Status  string `json:"status"`
	EventID string `json:"event_id"`
}

type operationalMetrics struct {
	observability.MetricsSnapshot
	Goroutines int                     `json:"goroutines"`
	Messages   database.MessageMetrics `json:"messages"`
	Outbox     database.OutboxMetrics  `json:"outbox"`
	DLQ        database.DLQMetrics     `json:"dlq"`
}

func New(store Store, dlq DeadLetterManager, database Pinger, nats ReadinessChecker, metrics *observability.Metrics, logger *slog.Logger) *Handler {
	return &Handler{
		store:    store,
		dlq:      dlq,
		database: database,
		nats:     nats,
		metrics:  metrics,
		logger:   logger,
	}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{"postgres": "ok", "nats": "ok"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := false
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := h.database.Ping(ctx); err != nil {
			h.logger.Error("readiness database check failed", "error", err)
			mu.Lock()
			checks["postgres"] = "unavailable"
			failed = true
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		if err := h.nats.Ready(ctx); err != nil {
			h.logger.Error("readiness nats check failed", "error", err)
			mu.Lock()
			checks["nats"] = "unavailable"
			failed = true
			mu.Unlock()
		}
	}()
	wg.Wait()

	status := "ready"
	code := http.StatusOK
	if failed {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, readinessResponse{Status: status, Checks: checks})
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r)
	filter := database.MessageFilter{
		Page:      page,
		PageSize:  pageSize,
		Sender:    r.URL.Query().Get("sender"),
		MessageID: r.URL.Query().Get("message_id"),
	}
	from, err := parseQueryTime(r.URL.Query().Get("from"), false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_from", "from must be RFC3339 or YYYY-MM-DD")
		return
	}
	to, err := parseQueryTime(r.URL.Query().Get("to"), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_to", "to must be RFC3339 or YYYY-MM-DD")
		return
	}
	if from != nil && to != nil && from.After(*to) {
		writeError(w, http.StatusBadRequest, "invalid_date_range", "from must not be after to")
		return
	}
	filter.From = from
	filter.To = to
	result, err := h.store.ListMessages(r.Context(), filter)
	if err != nil {
		h.internalError(w, "list messages", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a positive integer")
		return
	}
	message, err := h.store.GetMessage(r.Context(), id)
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}
	if err != nil {
		h.internalError(w, "get message", err)
		return
	}
	writeJSON(w, http.StatusOK, message)
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.Stats(r.Context())
	if err != nil {
		h.internalError(w, "load stats", err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) ListOutbox(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if !validStatus(status, "pending", "processing", "published", "failed") {
		writeError(w, http.StatusBadRequest, "invalid_status", "unsupported outbox status")
		return
	}
	page, pageSize := parsePagination(r)
	result, err := h.store.ListOutboxEvents(r.Context(), database.OutboxFilter{Page: page, PageSize: pageSize, Status: status})
	if err != nil {
		h.internalError(w, "list outbox events", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetOutbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a UUID")
		return
	}
	event, err := h.store.GetOutboxEvent(r.Context(), id)
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "outbox event not found")
		return
	}
	if err != nil {
		h.internalError(w, "get outbox event", err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (h *Handler) ListDLQ(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if !validStatus(status, "unresolved", "resolved") {
		writeError(w, http.StatusBadRequest, "invalid_status", "unsupported dead letter status")
		return
	}
	page, pageSize := parsePagination(r)
	result, err := h.store.ListDeadLetters(r.Context(), database.DeadLetterFilter{Page: page, PageSize: pageSize, Status: status})
	if err != nil {
		h.internalError(w, "list dead letters", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetDLQ(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a UUID")
		return
	}
	letter, err := h.store.GetDeadLetter(r.Context(), id)
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "dead letter not found")
		return
	}
	if err != nil {
		h.internalError(w, "get dead letter", err)
		return
	}
	writeJSON(w, http.StatusOK, letter)
}

func (h *Handler) ReplayDLQ(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a UUID")
		return
	}
	eventID, err := h.dlq.Replay(r.Context(), id)
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "dead letter not found")
		return
	}
	if errors.Is(err, database.ErrDLQResolved) {
		writeError(w, http.StatusConflict, "resolved", "a resolved dead letter cannot be replayed")
		return
	}
	if err != nil {
		h.internalError(w, "replay dead letter", err)
		return
	}
	writeJSON(w, http.StatusAccepted, replayResponse{Status: "queued", EventID: eventID})
}

func (h *Handler) ResolveDLQ(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a UUID")
		return
	}
	letter, err := h.dlq.Resolve(r.Context(), id)
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "dead letter not found")
		return
	}
	if err != nil {
		h.internalError(w, "resolve dead letter", err)
		return
	}
	writeJSON(w, http.StatusOK, letter)
}

func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	dbMetrics, err := h.store.OperationalMetrics(r.Context())
	if err != nil {
		h.internalError(w, "load operational metrics", err)
		return
	}
	snapshot := h.metrics.Snapshot()
	writeJSON(w, http.StatusOK, operationalMetrics{
		MetricsSnapshot: snapshot,
		Goroutines:      runtime.NumGoroutine(),
		Messages:        dbMetrics.Messages,
		Outbox:          dbMetrics.Outbox,
		DLQ:             dbMetrics.DLQ,
	})
}

func (h *Handler) internalError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error("http handler failed", "operation", operation, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func parsePagination(r *http.Request) (int, int) {
	page := 1
	pageSize := 20
	if parsed, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && parsed > 0 {
		page = parsed
	}
	if parsed, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && parsed > 0 {
		pageSize = parsed
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func validStatus(status string, allowed ...string) bool {
	if status == "" {
		return true
	}
	for _, value := range allowed {
		if status == value {
			return true
		}
	}
	return false
}

func parseQueryTime(value string, endOfDay bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	if endOfDay {
		utc = utc.Add(24*time.Hour - time.Nanosecond)
	}
	return &utc, nil
}
