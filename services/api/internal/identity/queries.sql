-- Queries for the identity module. sqlc turns each into a typed Go method in
-- internal/identity/store. Every statement is parameterised.

-- ---------------------------------------------------------------- users

-- name: CreateUser :one
INSERT INTO users (email, email_canonical, first_name, last_name, email_verified_at)
VALUES (@email, @email_canonical, @first_name, @last_name, sqlc.narg(email_verified_at))
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByCanonicalEmail :one
SELECT * FROM users WHERE email_canonical = @email_canonical;

-- name: MarkEmailVerified :one
UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateUserName :exec
UPDATE users SET first_name = @first_name, last_name = @last_name, updated_at = now() WHERE id = @id;

-- name: SetUserStatus :execrows
UPDATE users SET status = @status, updated_at = now() WHERE id = @id;

-- name: TouchLogin :exec
UPDATE users SET last_login_at = now() WHERE id = @id;

-- ---------------------------------------------------------------- passwords

-- name: GetPasswordHash :one
SELECT password_hash FROM password_credentials WHERE user_id = @user_id;

-- name: SetPassword :exec
INSERT INTO password_credentials (user_id, password_hash) VALUES (@user_id, @password_hash)
ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now();

-- ---------------------------------------------------------------- sessions

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, audience, client, mfa_verified_at, user_agent, ip_hash, expires_at)
VALUES (@user_id, @token_hash, @audience, @client, sqlc.narg(mfa_verified_at), sqlc.narg(user_agent), sqlc.narg(ip_hash), @expires_at)
RETURNING *;

-- name: FindLiveSession :one
-- A session is live when it is not revoked, not past its absolute expiry,
-- used within the idle limit, and its user is active.
SELECT s.id, s.user_id, s.audience, s.mfa_verified_at, s.created_at, s.last_seen_at, s.expires_at,
       u.email, u.first_name, u.last_name, u.email_verified_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash
  AND s.audience = @audience
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND s.last_seen_at > now() - make_interval(secs => sqlc.arg(idle_seconds)::float8)
  AND u.status = 'active';

-- name: TouchSession :exec
-- Written at most once a minute per session, not on every request.
UPDATE sessions SET last_seen_at = now()
WHERE id = @id AND last_seen_at < now() - interval '1 minute';

-- name: ElevateSession :exec
-- After the second factor: mark the session and swap its token, so a token
-- seen before MFA is worthless after it.
UPDATE sessions
SET mfa_verified_at = now(), token_hash = @new_token_hash, expires_at = @expires_at, last_seen_at = now()
WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = now(), revoked_reason = @reason
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeUserSessions :execrows
-- Revokes every live session of a user, optionally keeping one (the caller's).
UPDATE sessions SET revoked_at = now(), revoked_reason = @reason
WHERE user_id = @user_id AND revoked_at IS NULL
  AND (sqlc.narg(keep_id)::uuid IS NULL OR id <> sqlc.narg(keep_id)::uuid);

-- name: ListLiveSessions :many
SELECT id, audience, client, user_agent, created_at, last_seen_at, expires_at
FROM sessions
WHERE user_id = @user_id AND audience = @audience AND revoked_at IS NULL AND expires_at > now()
ORDER BY last_seen_at DESC
LIMIT 50;

-- name: DeleteStaleSessions :exec
-- Keeps ended sessions for 30 days (they show in investigations), then drops them.
DELETE FROM sessions
WHERE (expires_at < now() - interval '30 days')
   OR (revoked_at IS NOT NULL AND revoked_at < now() - interval '30 days');

-- ---------------------------------------------------------------- email tokens

-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at)
VALUES (@user_id, @purpose, @token_hash, @expires_at);

-- name: ConsumeAuthToken :one
UPDATE auth_tokens SET consumed_at = now()
WHERE token_hash = @token_hash AND purpose = @purpose AND consumed_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: InvalidateAuthTokens :exec
UPDATE auth_tokens SET consumed_at = now()
WHERE user_id = @user_id AND purpose = @purpose AND consumed_at IS NULL;

-- ---------------------------------------------------------------- RBAC

-- name: UserRoles :many
-- Global (unscoped) roles only.
SELECT role_key FROM user_roles WHERE user_id = @user_id AND scope_type IS NULL ORDER BY role_key;

-- name: UserPermissions :many
-- Global permissions: the union over the user's unscoped roles.
SELECT DISTINCT rp.permission_key
FROM user_roles ur
JOIN role_permissions rp ON rp.role_key = ur.role_key
WHERE ur.user_id = @user_id AND ur.scope_type IS NULL
ORDER BY rp.permission_key;

-- name: RoleExists :one
SELECT EXISTS (SELECT 1 FROM roles WHERE key = @key);

-- name: GrantRole :exec
INSERT INTO user_roles (user_id, role_key, scope_type, scope_id, granted_by)
VALUES (@user_id, @role_key, sqlc.narg(scope_type), sqlc.narg(scope_id), sqlc.narg(granted_by))
ON CONFLICT ON CONSTRAINT user_roles_unique DO NOTHING;

-- name: RevokeRole :execrows
DELETE FROM user_roles
WHERE user_id = @user_id AND role_key = @role_key AND scope_type IS NULL;

-- name: ListPermissionKeys :many
SELECT key FROM permissions ORDER BY key;

-- ---------------------------------------------------------------- MFA

-- name: GetTOTP :one
SELECT * FROM mfa_totp WHERE user_id = @user_id;

-- name: StartTOTPEnrolment :execrows
-- Replaces an unconfirmed secret; never overwrites a confirmed one.
INSERT INTO mfa_totp (user_id, secret_ciphertext) VALUES (@user_id, @secret_ciphertext)
ON CONFLICT (user_id) DO UPDATE
SET secret_ciphertext = EXCLUDED.secret_ciphertext, last_used_step = NULL, updated_at = now()
WHERE mfa_totp.confirmed_at IS NULL;

-- name: ConfirmTOTP :exec
UPDATE mfa_totp SET confirmed_at = now(), updated_at = now() WHERE user_id = @user_id AND confirmed_at IS NULL;

-- name: UseTOTPStep :execrows
-- Accepts a time step only if it is newer than the last one used.
UPDATE mfa_totp SET last_used_step = @step, updated_at = now()
WHERE user_id = @user_id AND (last_used_step IS NULL OR last_used_step < @step);

-- name: DeleteTOTP :execrows
DELETE FROM mfa_totp WHERE user_id = @user_id;
