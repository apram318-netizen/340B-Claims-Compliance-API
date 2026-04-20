package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"

	"claims-system/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// findOrCreateSSOUser resolves a user by email within the org, optionally JIT-provisioning from organization_settings.sso_jit_provision.
func (apiCfg *apiConfig) findOrCreateSSOUser(ctx context.Context, orgID uuid.UUID, email string) (database.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return database.User{}, errors.New("email required")
	}
	user, err := apiCfg.DB.GetUserByEmail(ctx, email)
	if err == nil {
		if user.OrgID != orgID {
			return database.User{}, errors.New("user belongs to another organization")
		}
		if !user.Active {
			return database.User{}, errors.New("account is disabled")
		}
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return database.User{}, err
	}
	row, err := apiCfg.DB.GetOrganizationSettings(ctx, orgID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return database.User{}, err
	}
	var settings map[string]any
	if err == nil && len(row.Settings) > 0 {
		_ = json.Unmarshal(row.Settings, &settings)
	}
	jit, _ := settings["sso_jit_provision"].(bool)
	if !jit {
		return database.User{}, errors.New("user not found; enable sso_jit_provision in organization settings or create the user first")
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	hash, err := bcrypt.GenerateFromPassword(raw, bcrypt.DefaultCost)
	if err != nil {
		return database.User{}, err
	}
	return apiCfg.DB.CreateUser(ctx, database.CreateUserParams{
		OrgID:        orgID,
		Email:        email,
		Name:         email,
		Role:         "member",
		PasswordHash: string(hash),
	})
}
