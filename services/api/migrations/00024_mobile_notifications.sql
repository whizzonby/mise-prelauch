-- Sprint 10 Stage 9: the app's notification centre, push device registry and
-- account deletion requests.
--
-- customer_notifications: the in-app centre. Written by the worker from the
--   same events that send customer emails (payments, weeks, deliveries,
--   chat replies). Authoritative: push only points at these rows.
-- push_devices: one row per app installation with a push token, tied to the
--   signed-in device session; signing out of that session removes it.
-- account_deletion_requests: a customer's request to delete the account.
--   Recording a request deletes nothing: erasure waits for the approved
--   retention policy (go-live blockers 22 and 23c).

-- +goose Up

CREATE TABLE customer_notifications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type        text NOT NULL CHECK (type ~ '^[a-z_]+(\.[a-z_]+)?$'),
    title       text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    body        text NOT NULL CHECK (char_length(body) <= 500),
    -- An in-app path (validated again by the app), never an external URL.
    link        text CHECK (link IS NULL OR link ~ '^/[A-Za-z0-9/_-]{0,199}$'),
    -- The same event never makes two entries (worker retries, duplicates).
    dedup_key   text NOT NULL CHECK (char_length(dedup_key) BETWEEN 8 AND 128),
    read_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT customer_notifications_dedup UNIQUE (user_id, dedup_key)
);

CREATE INDEX customer_notifications_user_idx ON customer_notifications (user_id, created_at DESC);
CREATE INDEX customer_notifications_unread_idx ON customer_notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE push_devices (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The device session that registered it; ending it removes the device.
    session_id      uuid REFERENCES sessions (id) ON DELETE SET NULL,
    installation_id text NOT NULL CHECK (installation_id ~ '^[A-Za-z0-9_-]{8,64}$'),
    platform        text NOT NULL CHECK (platform IN ('android', 'ios')),
    -- FCM registration token (APNs goes through FCM). Not a secret, but
    -- personal: never logged.
    push_token      text NOT NULL CHECK (char_length(push_token) BETWEEN 16 AND 4096),
    app_version     text CHECK (app_version IS NULL OR char_length(app_version) <= 40),
    token_updated_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    revoked_at      timestamptz,
    revoked_reason  text CHECK (revoked_reason IS NULL OR revoked_reason IN ('logout', 'unregistered', 'invalid_token', 'replaced', 'user')),

    CONSTRAINT push_devices_revoked_has_reason CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);

-- One live registration per installation (a reinstall makes a new id).
CREATE UNIQUE INDEX push_devices_installation_live ON push_devices (installation_id) WHERE revoked_at IS NULL;
CREATE INDEX push_devices_user_live ON push_devices (user_id) WHERE revoked_at IS NULL;
CREATE INDEX push_devices_session ON push_devices (session_id) WHERE revoked_at IS NULL;

CREATE TABLE account_deletion_requests (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status       text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested', 'withdrawn', 'in_progress', 'completed', 'declined')),
    reason       text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 1000),
    -- What the customer was told about outstanding orders and retention.
    acknowledged boolean NOT NULL DEFAULT false,
    source       text NOT NULL DEFAULT 'app' CHECK (source IN ('app', 'web', 'support')),
    requested_at timestamptz NOT NULL DEFAULT now(),
    withdrawn_at timestamptz,
    handled_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    handled_at   timestamptz,
    staff_note   text NOT NULL DEFAULT '' CHECK (char_length(staff_note) <= 2000)
);

-- At most one open request per account.
CREATE UNIQUE INDEX account_deletion_requests_open ON account_deletion_requests (user_id) WHERE status IN ('requested', 'in_progress');
CREATE INDEX account_deletion_requests_status ON account_deletion_requests (status, requested_at);

-- +goose Down

DROP TABLE account_deletion_requests;
DROP TABLE push_devices;
DROP TABLE customer_notifications;
