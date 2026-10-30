package middleware

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func APIKey(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get("X-API-Key")
			if provided == "" {
				authorization := r.Header.Get("Authorization")
				if strings.HasPrefix(authorization, "Bearer ") {
					provided = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
				}
			}
			if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(errorEnvelope{
					Error: errorBody{Code: "unauthorized", Message: "valid API key is required"},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
