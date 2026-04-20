-- name: InsertMfaBackupCode :exec
INSERT INTO mfa_backup_codes (user_id, code_hash) VALUES ($1, $2);

-- name: DeleteMfaBackupCodesForUser :exec
DELETE FROM mfa_backup_codes WHERE user_id = $1;

-- name: ConsumeMfaBackupCode :one
UPDATE mfa_backup_codes SET used_at = NOW()
WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
RETURNING id;
