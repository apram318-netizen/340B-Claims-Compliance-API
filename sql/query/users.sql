-- name: CreateUser :one
INSERT INTO users (org_id,email,name,role,password_hash)
VALUES($1,$2,$3,$4,$5)
RETURNING id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active;

-- name: GetUserByEmail :one 
SELECT id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active FROM users WHERE id = $1;

-- name: SetUserMFA :one
UPDATE users SET mfa_enabled = true, mfa_secret = $2 WHERE id = $1
RETURNING id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active;

-- name: DisableUserMFA :one
UPDATE users SET mfa_enabled = false, mfa_secret = NULL WHERE id = $1
RETURNING id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active;

-- name: UpdateUserActive :one
UPDATE users SET active = $2 WHERE id = $1
RETURNING id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active;

-- name: UpdateUserName :one
UPDATE users SET name = $2 WHERE id = $1
RETURNING id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active;