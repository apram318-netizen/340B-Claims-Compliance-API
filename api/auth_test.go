package main

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

func TestParseJWTClaims_PreviousSecret(t *testing.T) {
	t.Parallel()

	uid := uuid.New()
	oldSecret := strings.Repeat("o", 32)
	newSecret := strings.Repeat("n", 32)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": uid.String(),
		"jti":     uuid.New().String(),
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"iss":     tokenIssuer,
		"aud":     tokenAudience,
	})
	signed, err := tok.SignedString([]byte(oldSecret))
	if err != nil {
		t.Fatal(err)
	}

	cfg := &apiConfig{JwtSecret: newSecret, JwtSecretPrevious: oldSecret}
	claims, err := cfg.parseJWTClaims(signed)
	if err != nil {
		t.Fatalf("expected token valid with previous secret: %v", err)
	}
	if claims["user_id"] != uid.String() {
		t.Fatalf("user_id claim: got %v", claims["user_id"])
	}
}
