package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifyAccessToken(t *testing.T) {
	secret := []byte("test-secret-that-is-at-least-32-bytes")
	service := NewService(nil, Config{Secret: secret, AccessTTL: time.Minute})
	expired := NewService(nil, Config{Secret: secret, AccessTTL: -time.Minute})
	foreign := NewService(nil, Config{Secret: []byte("another-secret-that-is-at-least-32-bytes"), AccessTTL: time.Minute})

	valid, err := service.accessToken("user-1")
	if err != nil {
		t.Fatal(err)
	}
	expiredToken, err := expired.accessToken("user-1")
	if err != nil {
		t.Fatal(err)
	}
	foreignToken, err := foreign.accessToken("user-1")
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	noExpiry, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "user-1"}).SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{name: "valid", token: valid},
		{name: "expired", token: expiredToken, wantErr: true},
		{name: "signed with another secret", token: foreignToken, wantErr: true},
		{name: "unsigned", token: unsigned, wantErr: true},
		{name: "without expiry", token: noExpiry, wantErr: true},
		{name: "malformed", token: "not-a-token", wantErr: true},
		{name: "empty", token: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			userID, err := service.VerifyAccessToken(test.token)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got user %q", userID)
				}
				return
			}
			if err != nil || userID != "user-1" {
				t.Fatalf("got user %q, error %v", userID, err)
			}
		})
	}
}
