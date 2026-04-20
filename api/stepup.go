package main

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const (
	stepUpIssuer   = "claims-system-step-up"
	stepUpAudience = "mfa-step-up"
	stepUpPurpose  = "mfa_step_up"
)

func (cfg *apiConfig) createStepUpToken(userID uuid.UUID) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID.String(),
		"purpose": stepUpPurpose,
		"iss":     stepUpIssuer,
		"aud":     stepUpAudience,
		"jti":     uuid.New().String(),
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(10 * time.Minute).Unix(),
	})
	return tok.SignedString([]byte(cfg.JwtSecret))
}

func (cfg *apiConfig) verifyStepUpToken(tokenString string, expectedUserID uuid.UUID) error {
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
			lastErr = fmt.Errorf("invalid claims")
			continue
		}
		if claims["iss"] != stepUpIssuer {
			lastErr = fmt.Errorf("invalid issuer")
			continue
		}
		if !hasAudience(claims["aud"], stepUpAudience) {
			lastErr = fmt.Errorf("invalid audience")
			continue
		}
		if p, _ := claims["purpose"].(string); p != stepUpPurpose {
			lastErr = fmt.Errorf("invalid purpose")
			continue
		}
		uidStr, _ := claims["user_id"].(string)
		uid, err := uuid.Parse(uidStr)
		if err != nil || uid != expectedUserID {
			lastErr = fmt.Errorf("user mismatch")
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("invalid step-up token")
	}
	return lastErr
}
