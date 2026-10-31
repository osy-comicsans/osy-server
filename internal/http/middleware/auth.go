package middleware

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"osyn/internal/auth"
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
				unauthorized(w, "valid API key is required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type AccessTokenVerifier interface {
	VerifyAccessToken(token string) (string, error)
}

func RequireUser(verifier AccessTokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				unauthorized(w, "a valid access token is required")
				return
			}
			userID, err := verifier.VerifyAccessToken(strings.TrimSpace(token))
			if err != nil {
				unauthorized(w, "a valid access token is required")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUserID(r.Context(), userID)))
		})
	}
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(errorEnvelope{
		Error: errorBody{Code: "unauthorized", Message: message},
	})
}
