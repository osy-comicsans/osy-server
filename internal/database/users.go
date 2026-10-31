package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const userColumns = "id::text, first_name, last_name, email, mobile_number, onboarding_status"

func (db *DB) CreateUser(ctx context.Context, user NewUser, token RefreshToken) (User, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin create user transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (id, first_name, last_name, email, mobile_number, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+userColumns,
		user.ID, user.FirstName, user.LastName, user.Email, user.MobileNumber, user.PasswordHash))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		_ = tx.Rollback(ctx)
		return User{}, db.userConflict(ctx, user.Email, user.MobileNumber)
	}
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)
	`, token.Hash, created.ID, token.ExpiresAt); err != nil {
		return User{}, fmt.Errorf("insert refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit create user transaction: %w", err)
	}
	return created, nil
}

// Postgres reports only the first violated constraint, so look up both fields.
func (db *DB) userConflict(ctx context.Context, email, mobileNumber string) error {
	conflict := &UserConflict{}
	if err := db.Pool.QueryRow(ctx, `
		SELECT coalesce(bool_or(email = $1), false), coalesce(bool_or(mobile_number = $2), false)
		FROM users
		WHERE email = $1 OR mobile_number = $2
	`, email, mobileNumber).Scan(&conflict.Email, &conflict.MobileNumber); err != nil {
		return fmt.Errorf("find conflicting user: %w", err)
	}
	return conflict
}

func (db *DB) GetUserCredentials(ctx context.Context, email string) (User, string, error) {
	var user User
	var passwordHash string
	err := db.Pool.QueryRow(ctx, `SELECT `+userColumns+`, password_hash FROM users WHERE email = $1`, email).Scan(
		&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.MobileNumber, &user.OnboardingStatus, &passwordHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("get user credentials: %w", err)
	}
	return user, passwordHash, nil
}

func (db *DB) GetUser(ctx context.Context, id string) (User, error) {
	user, err := scanUser(db.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

func (db *DB) CreateRefreshToken(ctx context.Context, userID string, token RefreshToken) error {
	if _, err := db.Pool.Exec(ctx, `
		WITH expired AS (
			DELETE FROM refresh_tokens WHERE user_id = $2 AND expires_at <= now()
		)
		INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)
	`, token.Hash, userID, token.ExpiresAt); err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

// RotateRefreshToken consumes a live token and stores its replacement in one
// statement, so concurrent refreshes with the same token cannot both succeed.
func (db *DB) RotateRefreshToken(ctx context.Context, oldHash []byte, next RefreshToken) (string, error) {
	var userID string
	err := db.Pool.QueryRow(ctx, `
		WITH consumed AS (
			DELETE FROM refresh_tokens
			WHERE token_hash = $1 AND expires_at > now()
			RETURNING user_id
		)
		INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
		SELECT $2, user_id, $3 FROM consumed
		RETURNING user_id::text
	`, oldHash, next.Hash, next.ExpiresAt).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("rotate refresh token: %w", err)
	}
	return userID, nil
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.MobileNumber, &user.OnboardingStatus)
	return user, err
}
