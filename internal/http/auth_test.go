package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"osyn/internal/auth"
	"osyn/internal/database"
	apihttp "osyn/internal/http"
	"osyn/internal/http/handlers"
	"osyn/internal/observability"
)

type fakeUser struct {
	user         database.User
	passwordHash string
}

type fakeStore struct {
	mu     sync.Mutex
	users  map[string]fakeUser
	tokens map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: make(map[string]fakeUser), tokens: make(map[string]string)}
}

func (s *fakeStore) CreateUser(_ context.Context, user database.NewUser, token database.RefreshToken) (database.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conflict := &database.UserConflict{}
	for _, existing := range s.users {
		conflict.Email = conflict.Email || existing.user.Email == user.Email
		conflict.MobileNumber = conflict.MobileNumber || existing.user.MobileNumber == user.MobileNumber
	}
	if conflict.Email || conflict.MobileNumber {
		return database.User{}, conflict
	}
	created := database.User{
		ID:               user.ID,
		FirstName:        user.FirstName,
		LastName:         user.LastName,
		Email:            user.Email,
		MobileNumber:     user.MobileNumber,
		OnboardingStatus: "documents_required",
	}
	s.users[user.Email] = fakeUser{user: created, passwordHash: user.PasswordHash}
	s.tokens[string(token.Hash)] = user.ID
	return created, nil
}

func (s *fakeStore) GetUserCredentials(_ context.Context, email string) (database.User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.users[email]
	if !ok {
		return database.User{}, "", database.ErrNotFound
	}
	return stored.user, stored.passwordHash, nil
}

func (s *fakeStore) GetUser(_ context.Context, id string) (database.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stored := range s.users {
		if stored.user.ID == id {
			return stored.user, nil
		}
	}
	return database.User{}, database.ErrNotFound
}

func (s *fakeStore) CreateRefreshToken(_ context.Context, userID string, token database.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[string(token.Hash)] = userID
	return nil
}

func (s *fakeStore) RotateRefreshToken(_ context.Context, oldHash []byte, next database.RefreshToken) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userID, ok := s.tokens[string(oldHash)]
	if !ok {
		return "", database.ErrNotFound
	}
	delete(s.tokens, string(oldHash))
	s.tokens[string(next.Hash)] = userID
	return userID, nil
}

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	service := auth.NewService(newFakeStore(), auth.Config{
		Secret:     []byte("test-secret-that-is-at-least-32-bytes"),
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	})
	logger := slog.New(slog.DiscardHandler)
	server := httptest.NewServer(apihttp.NewRouter(apihttp.Dependencies{
		Handlers: &handlers.Handler{},
		Auth:     handlers.NewAuth(service, logger),
		Users:    service,
		APIKey:   "test-api-key",
		Logger:   logger,
		Metrics:  observability.NewMetrics(),
	}))
	t.Cleanup(server.Close)
	return server
}

type response struct {
	status int
	body   map[string]any
}

func (r response) errorField(name string) any {
	errBody, _ := r.body["error"].(map[string]any)
	return errBody[name]
}

func (r response) fields() map[string]any {
	fields, _ := r.errorField("fields").(map[string]any)
	return fields
}

func call(t *testing.T, server *httptest.Server, method, path, token string, body any) response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("%s %s: decode response: %v", method, path, err)
	}
	return response{status: resp.StatusCode, body: decoded}
}

func registration() map[string]string {
	return map[string]string{
		"firstName":    "Amina",
		"lastName":     "Benali",
		"email":        " Amina@Example.com ",
		"mobileNumber": "+213551234567",
		"password":     "Secret123",
	}
}

func TestAuthSessionLifecycle(t *testing.T) {
	server := newServer(t)

	registered := call(t, server, http.MethodPost, "/api/v1/auth/register", "", registration())
	if registered.status != http.StatusCreated {
		t.Fatalf("register: status %d, body %v", registered.status, registered.body)
	}
	user, _ := registered.body["user"].(map[string]any)
	if user["email"] != "amina@example.com" || user["onboardingStatus"] != "documents_required" {
		t.Fatalf("register: unexpected user %v", user)
	}
	if _, leaked := user["password"]; leaked {
		t.Fatal("register: response contains the password")
	}
	accessToken, _ := registered.body["accessToken"].(string)
	refreshToken, _ := registered.body["refreshToken"].(string)
	if accessToken == "" || refreshToken == "" {
		t.Fatalf("register: missing tokens in %v", registered.body)
	}

	me := call(t, server, http.MethodGet, "/api/v1/me", accessToken, nil)
	if me.status != http.StatusOK || me.body["id"] != user["id"] {
		t.Fatalf("me: status %d, body %v", me.status, me.body)
	}

	login := call(t, server, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email":    "AMINA@example.com",
		"password": "Secret123",
	})
	if login.status != http.StatusOK {
		t.Fatalf("login: status %d, body %v", login.status, login.body)
	}

	rotated := call(t, server, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": refreshToken})
	if rotated.status != http.StatusOK {
		t.Fatalf("refresh: status %d, body %v", rotated.status, rotated.body)
	}
	nextRefreshToken, _ := rotated.body["refreshToken"].(string)
	if nextRefreshToken == "" || nextRefreshToken == refreshToken || rotated.body["accessToken"] == "" {
		t.Fatalf("refresh: tokens were not rotated: %v", rotated.body)
	}

	reused := call(t, server, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": refreshToken})
	if reused.status != http.StatusUnauthorized || reused.errorField("code") != "invalid_refresh_token" {
		t.Fatalf("reused refresh token: status %d, body %v", reused.status, reused.body)
	}

	next := call(t, server, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refreshToken": nextRefreshToken})
	if next.status != http.StatusOK {
		t.Fatalf("refresh with rotated token: status %d, body %v", next.status, next.body)
	}
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name   string
		change map[string]string
		want   []string
	}{
		{name: "short first name", change: map[string]string{"firstName": " A "}, want: []string{"firstName"}},
		{name: "long last name", change: map[string]string{"lastName": strings.Repeat("b", 51)}, want: []string{"lastName"}},
		{name: "invalid email", change: map[string]string{"email": "amina@example"}, want: []string{"email"}},
		{name: "email with display name", change: map[string]string{"email": "Amina <amina@example.com>"}, want: []string{"email"}},
		{name: "local mobile number", change: map[string]string{"mobileNumber": "0551234567"}, want: []string{"mobileNumber"}},
		{name: "short password", change: map[string]string{"password": "Sec123"}, want: []string{"password"}},
		{name: "password over bcrypt limit", change: map[string]string{"password": "Aa1" + strings.Repeat("x", 70)}, want: []string{"password"}},
		{name: "password without uppercase", change: map[string]string{"password": "secret123"}, want: []string{"password"}},
		{name: "password without number", change: map[string]string{"password": "SecretWord"}, want: []string{"password"}},
		{
			name:   "every field missing",
			change: map[string]string{"firstName": "", "lastName": "", "email": "", "mobileNumber": "", "password": ""},
			want:   []string{"firstName", "lastName", "email", "mobileNumber", "password"},
		},
	}
	server := newServer(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := registration()
			for key, value := range test.change {
				body[key] = value
			}
			got := call(t, server, http.MethodPost, "/api/v1/auth/register", "", body)
			if got.status != http.StatusUnprocessableEntity || got.errorField("code") != "validation_failed" {
				t.Fatalf("status %d, body %v", got.status, got.body)
			}
			fields := got.fields()
			if len(fields) != len(test.want) {
				t.Fatalf("got fields %v, want %v", fields, test.want)
			}
			for _, field := range test.want {
				if _, ok := fields[field]; !ok {
					t.Fatalf("missing error for %s in %v", field, fields)
				}
			}
		})
	}
}

func TestRegisterConflict(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		mobileNumber string
		want         []string
	}{
		{name: "email taken", email: "AMINA@example.com", mobileNumber: "+213661234567", want: []string{"email"}},
		{name: "mobile number taken", email: "other@example.com", mobileNumber: "+213551234567", want: []string{"mobileNumber"}},
		{name: "both taken", email: "amina@example.com", mobileNumber: "+213551234567", want: []string{"email", "mobileNumber"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newServer(t)
			if first := call(t, server, http.MethodPost, "/api/v1/auth/register", "", registration()); first.status != http.StatusCreated {
				t.Fatalf("first registration: status %d", first.status)
			}
			body := registration()
			body["email"] = test.email
			body["mobileNumber"] = test.mobileNumber
			got := call(t, server, http.MethodPost, "/api/v1/auth/register", "", body)
			if got.status != http.StatusConflict || got.errorField("code") != "already_registered" {
				t.Fatalf("status %d, body %v", got.status, got.body)
			}
			if fields := got.fields(); len(fields) != len(test.want) {
				t.Fatalf("got fields %v, want %v", fields, test.want)
			}
			for _, field := range test.want {
				if _, ok := got.fields()[field]; !ok {
					t.Fatalf("missing error for %s in %v", field, got.fields())
				}
			}
		})
	}
}

func TestLoginFailuresLookTheSame(t *testing.T) {
	server := newServer(t)
	call(t, server, http.MethodPost, "/api/v1/auth/register", "", registration())

	wrongPassword := call(t, server, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email":    "amina@example.com",
		"password": "Wrong1234",
	})
	unknownEmail := call(t, server, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email":    "nobody@example.com",
		"password": "Secret123",
	})
	for name, got := range map[string]response{"wrong password": wrongPassword, "unknown email": unknownEmail} {
		if got.status != http.StatusUnauthorized || got.errorField("code") != "invalid_credentials" {
			t.Fatalf("%s: status %d, body %v", name, got.status, got.body)
		}
	}
	if wrongPassword.errorField("message") != unknownEmail.errorField("message") {
		t.Fatalf("messages differ: %v vs %v", wrongPassword.body, unknownEmail.body)
	}
}

func TestProtectedRoutes(t *testing.T) {
	server := newServer(t)
	registered := call(t, server, http.MethodPost, "/api/v1/auth/register", "", registration())
	accessToken, _ := registered.body["accessToken"].(string)

	tests := []struct {
		name  string
		path  string
		token string
	}{
		{name: "me without a token", path: "/api/v1/me"},
		{name: "me with a malformed token", path: "/api/v1/me", token: "not-a-token"},
		{name: "me with the API key", path: "/api/v1/me", token: "test-api-key"},
		{name: "admin route with a user token", path: "/api/v1/stats", token: accessToken},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := call(t, server, http.MethodGet, test.path, test.token, nil)
			if got.status != http.StatusUnauthorized || got.errorField("code") != "unauthorized" {
				t.Fatalf("status %d, body %v", got.status, got.body)
			}
		})
	}
}

func TestMalformedBody(t *testing.T) {
	server := newServer(t)
	resp, err := server.Client().Post(server.URL+"/api/v1/auth/login", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestCredentialAttemptsAreRateLimited(t *testing.T) {
	server := newServer(t)
	for attempt := 1; attempt <= 20; attempt++ {
		if got := call(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]string{}); got.status != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status %d", attempt, got.status)
		}
	}
	got := call(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]string{})
	if got.status != http.StatusTooManyRequests || got.errorField("code") != "rate_limited" {
		t.Fatalf("status %d, body %v", got.status, got.body)
	}
}
