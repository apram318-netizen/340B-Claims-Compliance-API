package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const (
	tokenIssuer   = "claims-system-api"
	tokenAudience = "claims-system-clients"
)

func createJWT(userID uuid.UUID, jwtSecret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID.String(),
		"jti":     uuid.New().String(),
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"iss":     tokenIssuer,
		"aud":     tokenAudience,
	})

	return token.SignedString([]byte(jwtSecret))
}

func getJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		slog.Error("JWT_SECRET is not set")
		os.Exit(1)
	}

	if len(secret) < 32 {
		slog.Error("JWT_SECRET must be at least 32 characters")
		os.Exit(1)
	}

	return secret
}

// jwtVerificationSecrets returns the primary secret first, then JWT_SECRET_PREVIOUS
// when set, so tokens can be verified during rolling JWT secret rotation.
func (cfg *apiConfig) jwtVerificationSecrets() []string {
	s := []string{cfg.JwtSecret}
	if cfg.JwtSecretPrevious != "" {
		s = append(s, cfg.JwtSecretPrevious)
	}
	return s
}

// parseJWTClaims validates the HS256 token against the primary and (if configured)
// previous JWT secrets.
func (cfg *apiConfig) parseJWTClaims(tokenString string) (jwt.MapClaims, error) {
	var lastErr error
	for _, secret := range cfg.jwtVerificationSecrets() {
		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(secret), nil
		})
		if err != nil {
			lastErr = err
			continue
		}
		if !token.Valid {
			continue
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			lastErr = fmt.Errorf("invalid token claims")
			continue
		}
		return claims, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("invalid token")
	}
	return nil, lastErr
}
