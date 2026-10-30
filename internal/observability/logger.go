package observability

import (
	"log/slog"
	"os"
	"strings"
)

func NewLogger(level, appName, service string) *slog.Logger {
	options := &slog.HandlerOptions{Level: parseLevel(level)}
	handler := slog.NewJSONHandler(os.Stdout, options)
	return slog.New(handler).With(
		"app", appName,
		"service", service,
	)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
