package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"osyn/internal/auth"
	"osyn/internal/database"
)

const maxAuthBodyBytes = 64 << 10

type AuthService interface {
	Register(ctx context.Context, request auth.RegisterRequest) (auth.Session, error)
	Login(ctx context.Context, request auth.LoginRequest) (auth.Session, error)
	Refresh(ctx context.Context, refreshToken string) (auth.TokenPair, error)
	CurrentUser(ctx context.Context, userID string) (database.User, error)
}

type Auth struct {
	service AuthService
	logger  *slog.Logger
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func NewAuth(service AuthService, logger *slog.Logger) *Auth {
	return &Auth{service: service, logger: logger}
}

func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var request auth.RegisterRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session, err := a.service.Register(r.Context(), request)
	var invalid *auth.ValidationError
	var conflict *database.UserConflict
	switch {
	case errors.As(err, &invalid):
		writeFieldErrors(w, http.StatusUnprocessableEntity, "validation_failed", "some fields are invalid", invalid.Fields)
	case errors.As(err, &conflict):
		fields := make(map[string]string)
		if conflict.Email {
			fields["email"] = "This email is already registered"
		}
		if conflict.MobileNumber {
			fields["mobileNumber"] = "This mobile number is already registered"
		}
		writeFieldErrors(w, http.StatusConflict, "already_registered", "an account with these details already exists", fields)
	case err != nil:
		a.internalError(w, "register user", err)
	default:
		writeJSON(w, http.StatusCreated, session)
	}
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var request auth.LoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session, err := a.service.Login(r.Context(), request)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	if err != nil {
		a.internalError(w, "log in user", err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request) {
	var request refreshRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	tokens, err := a.service.Refresh(r.Context(), request.RefreshToken)
	if errors.Is(err, auth.ErrInvalidRefreshToken) {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "refresh token is invalid or expired")
		return
	}
	if err != nil {
		a.internalError(w, "refresh tokens", err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (a *Auth) Me(w http.ResponseWriter, r *http.Request) {
	user, err := a.service.CurrentUser(r.Context(), auth.UserID(r.Context()))
	if errors.Is(err, database.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a valid access token is required")
		return
	}
	if err != nil {
		a.internalError(w, "get current user", err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *Auth) internalError(w http.ResponseWriter, operation string, err error) {
	a.logger.Error("http handler failed", "operation", operation, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be a JSON object")
		return false
	}
	return true
}

func writeFieldErrors(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message, Fields: fields}})
}
