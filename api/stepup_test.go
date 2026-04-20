package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestStepUpToken_RoundTrip(t *testing.T) {
	t.Parallel()
	uid := uuid.New()
	secret := strings.Repeat("s", 32)
	cfg := &apiConfig{JwtSecret: secret}
	tok, err := cfg.createStepUpToken(uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.verifyStepUpToken(tok, uid); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := cfg.verifyStepUpToken(tok, uuid.New()); err == nil {
		t.Fatal("expected user mismatch")
	}
}

func TestStepUpToken_PreviousJWTSecret(t *testing.T) {
	t.Parallel()
	uid := uuid.New()
	oldS := strings.Repeat("a", 32)
	newS := strings.Repeat("b", 32)
	cfg := &apiConfig{JwtSecret: newS, JwtSecretPrevious: oldS}
	tok, err := cfg.createStepUpToken(uid)
	if err != nil {
		t.Fatal(err)
	}
	// Issued with primary secret only — verify with previous should fail
	cfgWrong := &apiConfig{JwtSecret: oldS}
	if err := cfgWrong.verifyStepUpToken(tok, uid); err == nil {
		t.Fatal("expected failure when verified only with previous secret")
	}
	if err := cfg.verifyStepUpToken(tok, uid); err != nil {
		t.Fatalf("verify with primary: %v", err)
	}
}
