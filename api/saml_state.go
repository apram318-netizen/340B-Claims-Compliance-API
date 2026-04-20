package main

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

const samlStateIssuer = "claims-system-saml-state"

func (cfg *apiConfig) createSAMLStateToken(orgID uuid.UUID, authnRequestID string) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":    samlStateIssuer,
		"org_id": orgID.String(),
		"req_id": authnRequestID,
		"iat":    time.Now().Unix(),
		"exp":    time.Now().Add(10 * time.Minute).Unix(),
	})
	return tok.SignedString([]byte(cfg.JwtSecret))
}

func (cfg *apiConfig) parseSAMLStateToken(s string) (orgID uuid.UUID, authnRequestID string, err error) {
	token, err := jwt.Parse(s, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.JwtSecret), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, "", fmt.Errorf("invalid relay state")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["iss"] != samlStateIssuer {
		return uuid.Nil, "", fmt.Errorf("invalid relay state issuer")
	}
	oid, _ := claims["org_id"].(string)
	orgID, err = uuid.Parse(oid)
	if err != nil {
		return uuid.Nil, "", err
	}
	authnRequestID, _ = claims["req_id"].(string)
	if authnRequestID == "" {
		return uuid.Nil, "", fmt.Errorf("missing authn request id")
	}
	return orgID, authnRequestID, nil
}
