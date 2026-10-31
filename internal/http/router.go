package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "osyn/docs"
	"osyn/internal/http/handlers"
	"osyn/internal/http/middleware"
	"osyn/internal/observability"
)

// Register and login run bcrypt, so each IP gets a bounded number of attempts.
const credentialAttemptsPerMinute = 20

type Dependencies struct {
	Handlers  *handlers.Handler
	Auth      *handlers.Auth
	Documents *handlers.Documents
	Users     middleware.AccessTokenVerifier
	APIKey    string
	Logger    *slog.Logger
	Metrics   *observability.Metrics
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
		api.Group(func(ops chi.Router) {
			ops.Use(middleware.APIKey(deps.APIKey))
			ops.Get("/messages", deps.Handlers.ListMessages)
			ops.Get("/messages/{id}", deps.Handlers.GetMessage)
			ops.Get("/stats", deps.Handlers.Stats)
			ops.Get("/outbox", deps.Handlers.ListOutbox)
			ops.Get("/outbox/{id}", deps.Handlers.GetOutbox)
			ops.Get("/dlq", deps.Handlers.ListDLQ)
			ops.Get("/dlq/{id}", deps.Handlers.GetDLQ)
			ops.Post("/dlq/{id}/replay", deps.Handlers.ReplayDLQ)
			ops.Post("/dlq/{id}/resolve", deps.Handlers.ResolveDLQ)
			ops.Get("/metrics", deps.Handlers.Metrics)
		})

		api.Route("/auth", func(auth chi.Router) {
			auth.Group(func(credentials chi.Router) {
				credentials.Use(httprate.Limit(credentialAttemptsPerMinute, time.Minute,
					httprate.WithKeyFuncs(httprate.KeyByIP),
					httprate.WithLimitHandler(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"too many attempts, try again shortly"}}`))
					}),
				))
				credentials.Post("/register", deps.Auth.Register)
				credentials.Post("/login", deps.Auth.Login)
			})
			auth.Post("/refresh", deps.Auth.Refresh)
		})

		api.Group(func(user chi.Router) {
			user.Use(middleware.RequireUser(deps.Users))
			user.Get("/me", deps.Auth.Me)
			user.Get("/me/documents", deps.Documents.List)
			user.Post("/me/documents", deps.Documents.Upload)
		})
	})
	return router
}
