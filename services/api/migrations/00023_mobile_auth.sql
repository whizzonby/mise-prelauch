-- Sprint 10 Stage 3: first-party mobile sign-in for the Flutter customer app.
--
-- A signed-in device is one `sessions` row (client 'mobile', audience
-- 'customer'), so it shows in /me/sessions and can be revoked like any other.
-- Its bearer token is a short-lived access token: `access_expires_at` ends it
-- after minutes, and every refresh swaps the token hash. Refresh tokens live
-- in `mobile_refresh_tokens`, hashed, one family per session. Presenting a
-- refresh token that was already used revokes the session and the family
-- (reuse detection). Web sessions are untouched: their access_expires_at is
-- NULL and their idle limit still applies.

-- +goose Up

ALTER TABLE sessions
    ADD COLUMN access_expires_at timestamptz,
    ADD COLUMN device_name text CHECK (device_name IS NULL OR char_length(device_name) BETWEEN 1 AND 80),
    -- A random id the app generates once per install (not a hardware id).
    ADD COLUMN installation_id text CHECK (installation_id IS NULL OR installation_id ~ '^[A-Za-z0-9_-]{8,64}$');

ALTER TABLE sessions ADD CONSTRAINT sessions_access_expiry_mobile_only
    CHECK (access_expires_at IS NULL OR client = 'mobile');

ALTER TABLE sessions DROP CONSTRAINT sessions_revoked_reason_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_revoked_reason_check CHECK (revoked_reason IS NULL OR revoked_reason IN
    ('logout', 'user_revoked', 'password_reset', 'password_changed', 'admin_revoked', 'account_disabled', 'mfa_failed', 'mfa_reset',
     'refresh_reuse'));

CREATE TABLE mobile_refresh_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  uuid NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    -- SHA-256 of the opaque refresh token. The token itself is never stored.
    token_hash  bytea NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    -- Set when the token is exchanged. A second exchange is reuse.
    used_at     timestamptz,
    revoked_at  timestamptz,

    CONSTRAINT mobile_refresh_tokens_hash_key UNIQUE (token_hash),
    CONSTRAINT mobile_refresh_tokens_expiry CHECK (expires_at > created_at)
);

CREATE INDEX mobile_refresh_tokens_session_idx ON mobile_refresh_tokens (session_id);
CREATE INDEX mobile_refresh_tokens_expires_idx ON mobile_refresh_tokens (expires_at);

-- +goose Down

DROP TABLE mobile_refresh_tokens;
UPDATE sessions SET revoked_reason = 'user_revoked' WHERE revoked_reason = 'refresh_reuse';
ALTER TABLE sessions DROP CONSTRAINT sessions_revoked_reason_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_revoked_reason_check CHECK (revoked_reason IS NULL OR revoked_reason IN
    ('logout', 'user_revoked', 'password_reset', 'password_changed', 'admin_revoked', 'account_disabled', 'mfa_failed', 'mfa_reset'));
ALTER TABLE sessions DROP CONSTRAINT sessions_access_expiry_mobile_only;
ALTER TABLE sessions DROP COLUMN installation_id, DROP COLUMN device_name, DROP COLUMN access_expires_at;
