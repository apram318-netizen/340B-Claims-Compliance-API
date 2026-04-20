package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ── Params ────────────────────────────────────────────────────────────────────

type CreatePasswordResetTokenParams struct {
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
}

// ── Queries ───────────────────────────────────────────────────────────────────

const createPasswordResetToken = `
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, user_id, token_hash, expires_at, used_at, created_at
`

func (q *Queries) CreatePasswordResetToken(ctx context.Context, arg CreatePasswordResetTokenParams) (PasswordResetToken, error) {
	row := q.db.QueryRow(ctx, createPasswordResetToken, arg.UserID, arg.TokenHash, arg.ExpiresAt)
	var i PasswordResetToken
	err := row.Scan(&i.ID, &i.UserID, &i.TokenHash, &i.ExpiresAt, &i.UsedAt, &i.CreatedAt)
	return i, err
}

const getPasswordResetToken = `
SELECT id, user_id, token_hash, expires_at, used_at, created_at
FROM password_reset_tokens
WHERE token_hash = $1 AND expires_at > NOW() AND used_at IS NULL
`

func (q *Queries) GetPasswordResetToken(ctx context.Context, tokenHash string) (PasswordResetToken, error) {
	row := q.db.QueryRow(ctx, getPasswordResetToken, tokenHash)
	var i PasswordResetToken
	err := row.Scan(&i.ID, &i.UserID, &i.TokenHash, &i.ExpiresAt, &i.UsedAt, &i.CreatedAt)
	return i, err
}

const markPasswordResetTokenUsed = `
UPDATE password_reset_tokens SET used_at = NOW() WHERE id = $1
`

func (q *Queries) MarkPasswordResetTokenUsed(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, markPasswordResetTokenUsed, id)
	return err
}

const updateUserPassword = `
UPDATE users SET password_hash = $2, failed_login_attempts = 0, locked_until = NULL
WHERE id = $1
`

type UpdateUserPasswordParams struct {
	ID           uuid.UUID
	PasswordHash string
}

func (q *Queries) UpdateUserPassword(ctx context.Context, arg UpdateUserPasswordParams) error {
	_, err := q.db.Exec(ctx, updateUserPassword, arg.ID, arg.PasswordHash)
	return err
}

const purgeExpiredAuditEvents = `
DELETE FROM audit_events WHERE expires_at < NOW()
`

// PurgeExpiredAuditEvents deletes audit events that have passed their 6-year
// HIPAA retention window. Call from a scheduled maintenance job.
func (q *Queries) PurgeExpiredAuditEvents(ctx context.Context) (int64, error) {
	result, err := q.db.Exec(ctx, purgeExpiredAuditEvents)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Suppress unused import warning
var _ = pgtype.Timestamptz{}
