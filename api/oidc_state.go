package main

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const oidcStateIssuer = "claims-system-oidc-state"

func (cfg *apiConfig) createOIDCStateToken(orgID uuid.UUID) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":     oidcStateIssuer,
		"org_id":  orgID.String(),
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(10 * time.Minute).Unix(),
	})
	return tok.SignedString([]byte(cfg.JwtSecret))
}

func (cfg *apiConfig) parseOIDCStateToken(s string) (uuid.UUID, error) {
	token, err := jwt.Parse(s, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.JwtSecret), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, fmt.Errorf("invalid state")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["iss"] != oidcStateIssuer {
		return uuid.Nil, fmt.Errorf("invalid state issuer")
	}
	oid, _ := claims["org_id"].(string)
	orgID, err := uuid.Parse(oid)
	if err != nil {
		return uuid.Nil, err
	}
	return orgID, nil
}
