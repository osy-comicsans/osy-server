package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "osyn/docs"
	"osyn/internal/http/handlers"
	"osyn/internal/http/middleware"
	"osyn/internal/observability"
)

type Dependencies struct {
	Handlers *handlers.Handler
	APIKey   string
	Logger   *slog.Logger
	Metrics  *observability.Metrics
}

func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)
	router.Use(chimiddleware.CleanPath)
	router.Use(middleware.Observe(deps.Metrics, deps.Logger))
	router.Use(chimiddleware.Recoverer)
	router.Use(chimiddleware.Timeout(20 * time.Second))

	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"route not found"}}`))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":{"code":"method_not_allowed","message":"method not allowed"}}`))
	})

	router.Get("/health", deps.Handlers.Health)
	router.Get("/ready", deps.Handlers.Ready)
	router.Get("/swagger/*", httpSwagger.Handler())

	router.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.APIKey(deps.APIKey))
		api.Get("/messages", deps.Handlers.ListMessages)
		api.Get("/messages/{id}", deps.Handlers.GetMessage)
		api.Get("/stats", deps.Handlers.Stats)
		api.Get("/outbox", deps.Handlers.ListOutbox)
		api.Get("/outbox/{id}", deps.Handlers.GetOutbox)
		api.Get("/dlq", deps.Handlers.ListDLQ)
		api.Get("/dlq/{id}", deps.Handlers.GetDLQ)
		api.Post("/dlq/{id}/replay", deps.Handlers.ReplayDLQ)
		api.Post("/dlq/{id}/resolve", deps.Handlers.ResolveDLQ)
		api.Get("/metrics", deps.Handlers.Metrics)
	})
	return router
}
