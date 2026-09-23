-- +goose Up

CREATE TABLE leads (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name        text NOT NULL CHECK (char_length(first_name) BETWEEN 1 AND 80),
    -- email is what the person typed, trimmed and lower-cased; it is where we send mail.
    email             text NOT NULL CHECK (char_length(email) <= 254),
    -- email_canonical additionally strips "+tag" (and dots for Gmail) so one inbox
    -- cannot join, or refer itself, many times.
    email_canonical   text NOT NULL,
    phone             text CHECK (phone IS NULL OR char_length(phone) <= 20),
    location          text NOT NULL CHECK (char_length(location) BETWEEN 1 AND 80),
    status            text NOT NULL DEFAULT 'PENDING'
                      CHECK (status IN ('PENDING', 'VERIFIED', 'QUALIFIED', 'CONVERTED', 'UNSUBSCRIBED', 'BLOCKED')),
    referral_code     text NOT NULL,
    referred_by       uuid REFERENCES leads (id) ON DELETE SET NULL,
    email_verified_at timestamptz,
    consent_at        timestamptz NOT NULL,
    consent_version   text NOT NULL,
    -- Keyed hash of the signup IP, used only to flag same-network referrals.
    signup_ip_hash    text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT leads_email_canonical_key UNIQUE (email_canonical),
    CONSTRAINT leads_referral_code_key UNIQUE (referral_code),
    CONSTRAINT leads_not_self_referred CHECK (referred_by IS NULL OR referred_by <> id)
);

CREATE INDEX leads_created_at_idx ON leads (created_at DESC);
CREATE INDEX leads_status_idx ON leads (status);
CREATE INDEX leads_location_idx ON leads (location);
CREATE INDEX leads_referred_by_idx ON leads (referred_by) WHERE referred_by IS NOT NULL;

CREATE TABLE lead_preferences (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id             uuid NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    household_size      smallint CHECK (household_size BETWEEN 1 AND 12),
    meals_per_week      smallint CHECK (meals_per_week BETWEEN 1 AND 21),
    dietary_preferences text[] NOT NULL DEFAULT '{}',
    meal_interests      text[] NOT NULL DEFAULT '{}',
    cooking_frequency   text,
    delivery_area       text,
    -- Answers to optional profiling questions that do not yet justify a column.
    metadata            jsonb NOT NULL DEFAULT '{}',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT lead_preferences_lead_id_key UNIQUE (lead_id)
);

CREATE INDEX lead_preferences_dietary_idx ON lead_preferences USING gin (dietary_preferences);

CREATE TABLE referrals (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    referrer_lead_id uuid NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    referred_lead_id uuid NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    referral_code    text NOT NULL,
    -- pending: referred lead has not verified. converted: verified and counted.
    -- flagged: looks like self-referral; never counted, visible to admins.
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'converted', 'flagged')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    converted_at     timestamptz,

    -- A lead can be referred by at most one person.
    CONSTRAINT referrals_referred_lead_id_key UNIQUE (referred_lead_id),
    CONSTRAINT referrals_not_self CHECK (referrer_lead_id <> referred_lead_id),
    CONSTRAINT referrals_converted_has_time CHECK ((status = 'converted') = (converted_at IS NOT NULL))
);

CREATE INDEX referrals_referrer_status_idx ON referrals (referrer_lead_id, status);

CREATE TABLE lead_attribution (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id      uuid NOT NULL REFERENCES leads (id) ON DELETE CASCADE,
    -- first: the visit that first brought them. latest: the visit they signed up on.
    touch        text NOT NULL CHECK (touch IN ('first', 'latest')),
    utm_source   text,
    utm_medium   text,
    utm_campaign text,
    utm_content  text,
    utm_term     text,
    landing_page text,
    referrer_url text,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT lead_attribution_lead_touch_key UNIQUE (lead_id, touch)
);

CREATE INDEX lead_attribution_source_idx ON lead_attribution (utm_source) WHERE touch = 'first';

CREATE TABLE lead_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lead_id      uuid REFERENCES leads (id) ON DELETE CASCADE,
    anonymous_id text,
    event_type   text NOT NULL,
    session_id   text,
    metadata     jsonb NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT lead_events_has_subject CHECK (lead_id IS NOT NULL OR anonymous_id IS NOT NULL)
);

CREATE INDEX lead_events_type_created_idx ON lead_events (event_type, created_at DESC);
CREATE INDEX lead_events_lead_idx ON lead_events (lead_id, created_at DESC) WHERE lead_id IS NOT NULL;
CREATE INDEX lead_events_anonymous_idx ON lead_events (anonymous_id) WHERE anonymous_id IS NOT NULL;

CREATE TABLE jobs (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind         text NOT NULL,
    payload      jsonb NOT NULL DEFAULT '{}',
    status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    attempts     integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 5,
    run_at       timestamptz NOT NULL DEFAULT now(),
    locked_at    timestamptz,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz
);

CREATE INDEX jobs_ready_idx ON jobs (run_at) WHERE status = 'queued';
CREATE INDEX jobs_running_idx ON jobs (locked_at) WHERE status = 'running';

CREATE TABLE admin_users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL,
    name          text NOT NULL,
    password_hash text NOT NULL,
    -- Role names map to permissions in code (internal/admin/permissions.go).
    role          text NOT NULL,
    disabled_at   timestamptz,
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT admin_users_email_key UNIQUE (email)
);

CREATE TABLE admin_sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_user_id uuid NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
    -- SHA-256 of the bearer token; the token itself is never stored.
    token_hash    bytea NOT NULL,
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT admin_sessions_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX admin_sessions_user_idx ON admin_sessions (admin_user_id);
CREATE INDEX admin_sessions_expires_idx ON admin_sessions (expires_at);

CREATE TABLE audit_log (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_admin_id uuid REFERENCES admin_users (id) ON DELETE SET NULL,
    action         text NOT NULL,
    target_type    text,
    target_id      text,
    metadata       jsonb NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_created_idx ON audit_log (created_at DESC);
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id);

-- +goose Down

DROP TABLE audit_log;
DROP TABLE admin_sessions;
DROP TABLE admin_users;
DROP TABLE jobs;
DROP TABLE lead_events;
DROP TABLE lead_attribution;
DROP TABLE referrals;
DROP TABLE lead_preferences;
DROP TABLE leads;
