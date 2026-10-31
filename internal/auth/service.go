package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"osyn/internal/database"
)

const passwordCost = 12

var (
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrInvalidAccessToken  = errors.New("invalid access token")
)

type Store interface {
	CreateUser(ctx context.Context, user database.NewUser, token database.RefreshToken) (database.User, error)
	GetUserCredentials(ctx context.Context, email string) (database.User, string, error)
	GetUser(ctx context.Context, id string) (database.User, error)
	CreateRefreshToken(ctx context.Context, userID string, token database.RefreshToken) error
	RotateRefreshToken(ctx context.Context, oldHash []byte, next database.RefreshToken) (string, error)
}

type Config struct {
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type Session struct {
	User         database.User `json:"user"`
	AccessToken  string        `json:"accessToken"`
	RefreshToken string        `json:"refreshToken"`
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type Service struct {
	store     Store
	config    Config
	dummyHash []byte
}

func NewService(store Store, config Config) *Service {
	// Checked when the email is unknown, so that miss takes as long as a wrong password.
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("osyn-unknown-account"), passwordCost)
	return &Service{store: store, config: config, dummyHash: dummyHash}
}

func (s *Service) Register(ctx context.Context, request RegisterRequest) (Session, error) {
	request = request.normalize()
	if fields := request.validate(); len(fields) > 0 {
		return Session{}, &ValidationError{Fields: fields}
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.Password), passwordCost)
	if err != nil {
		return Session{}, fmt.Errorf("hash password: %w", err)
	}
	refreshToken, stored := s.newRefreshToken()
	user, err := s.store.CreateUser(ctx, database.NewUser{
		ID:           uuid.NewString(),
		FirstName:    request.FirstName,
		LastName:     request.LastName,
		Email:        request.Email,
		MobileNumber: request.MobileNumber,
		PasswordHash: string(passwordHash),
	}, stored)
	if err != nil {
		return Session{}, err
	}
	return s.session(user, refreshToken)
}

func (s *Service) Login(ctx context.Context, request LoginRequest) (Session, error) {
	user, passwordHash, err := s.store.GetUserCredentials(ctx, normalizeEmail(request.Email))
	if errors.Is(err, database.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(request.Password))
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(request.Password)) != nil {
		return Session{}, ErrInvalidCredentials
	}
	refreshToken, stored := s.newRefreshToken()
	if err := s.store.CreateRefreshToken(ctx, user.ID, stored); err != nil {
		return Session{}, err
	}
	return s.session(user, refreshToken)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	next, stored := s.newRefreshToken()
	userID, err := s.store.RotateRefreshToken(ctx, hashRefreshToken(refreshToken), stored)
	if errors.Is(err, database.ErrNotFound) {
		return TokenPair{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return TokenPair{}, err
	}
	accessToken, err := s.accessToken(userID)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: accessToken, RefreshToken: next}, nil
}

func (s *Service) CurrentUser(ctx context.Context, userID string) (database.User, error) {
	return s.store.GetUser(ctx, userID)
}

func (s *Service) VerifyAccessToken(token string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return s.config.Secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || claims.Subject == "" {
		return "", ErrInvalidAccessToken
	}
	return claims.Subject, nil
}

func (s *Service) session(user database.User, refreshToken string) (Session, error) {
	accessToken, err := s.accessToken(user.ID)
	if err != nil {
		return Session{}, err
	}
	return Session{User: user, AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

func (s *Service) accessToken(userID string) (string, error) {
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.config.AccessTTL)),
	}).SignedString(s.config.Secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return token, nil
}

// Refresh tokens are random and only their hash is stored.
func (s *Service) newRefreshToken() (string, database.RefreshToken) {
	token := rand.Text()
	return token, database.RefreshToken{Hash: hashRefreshToken(token), ExpiresAt: time.Now().Add(s.config.RefreshTTL)}
}

func hashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type userIDKey struct{}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

func UserID(ctx context.Context) string {
	userID, _ := ctx.Value(userIDKey{}).(string)
	return userID
}
