package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"claims-system/internal/database"

	"github.com/google/uuid"
)

func hashBackupCode(plain string) string {
	n := strings.ToUpper(strings.TrimSpace(plain))
	sum := sha256.Sum256([]byte(n))
	return hex.EncodeToString(sum[:])
}

// generateBackupCodes replaces any existing backup codes and returns plaintext codes once.
func (apiCfg *apiConfig) generateBackupCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if err := apiCfg.DB.DeleteMfaBackupCodesForUser(ctx, userID); err != nil {
		return nil, err
	}
	out := make([]string, 0, 10)
	for range 10 {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		plain := hex.EncodeToString(raw)
		if err := apiCfg.DB.InsertMfaBackupCode(ctx, database.InsertMfaBackupCodeParams{
			UserID:   userID,
			CodeHash: hashBackupCode(plain),
		}); err != nil {
			return nil, err
		}
		out = append(out, plain)
	}
	return out, nil
}

func (apiCfg *apiConfig) tryConsumeBackupCode(ctx context.Context, userID uuid.UUID, plain string) bool {
	if strings.TrimSpace(plain) == "" {
		return false
	}
	_, err := apiCfg.DB.ConsumeMfaBackupCode(ctx, database.ConsumeMfaBackupCodeParams{
		UserID:   userID,
		CodeHash: hashBackupCode(plain),
	})
	return err == nil
}
