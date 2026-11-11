-- +goose Up

-- Sprint 1: one identity model for every person who signs in (customers now,
-- staff, chefs, farmers and others later), database-backed RBAC, revocable
-- sessions, single-use email tokens, TOTP MFA, and the customer record that a
-- waitlist lead can be linked to.
--
-- This migration is additive. Nothing existing is altered except two new
-- nullable columns on audit_log. Admin accounts are COPIED from admin_users
-- with the same ids and password hashes, so every admin keeps their password.
-- admin_users and admin_sessions are left in place, unused, so the previous
-- release can still run against this schema if it has to be rolled back.

-- ---------------------------------------------------------------- users

CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- What the person typed, trimmed and lower-cased: where we send mail.
    email             text NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254),
    -- Same canonical form as leads.email_canonical (no +tag, no Gmail dots),
    -- so one inbox is one account, and an account can be matched to its lead.
    email_canonical   text NOT NULL,
    email_verified_at timestamptz,
    first_name        text NOT NULL DEFAULT '' CHECK (char_length(first_name) <= 80),
    last_name         text NOT NULL DEFAULT '' CHECK (char_length(last_name) <= 80),
    status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    last_login_at     timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_email_canonical_key UNIQUE (email_canonical)
);

-- One row per password. Kept apart from users so an account can exist with
-- only an external identity (OAuth, later) and no password at all.
CREATE TABLE password_credentials (
    user_id       uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Argon2id in PHC string format.
    password_hash text NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- sessions

CREATE TABLE sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- SHA-256 of the opaque bearer token. The token itself is never stored.
    token_hash      bytea NOT NULL,
    -- Which surface the session is for. A customer session can never reach an
    -- admin endpoint, even when the same person also holds a staff role.
    audience        text NOT NULL CHECK (audience IN ('customer', 'admin')),
    -- web: held by a Next.js server for the browser. mobile: reserved for the
    -- Flutter apps' token flow (docs/security/AUTHENTICATION.md).
    client          text NOT NULL DEFAULT 'web' CHECK (client IN ('web', 'mobile')),
    -- Set when the second factor was presented in this session.
    mfa_verified_at timestamptz,
    user_agent      text CHECK (user_agent IS NULL OR char_length(user_agent) <= 256),
    -- Keyed hash, like leads.signup_ip_hash: lets a person recognise their
    -- sessions without storing the raw address.
    ip_hash         text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    -- Absolute end. The idle limit is applied in code against last_seen_at.
    expires_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    revoked_reason  text CHECK (revoked_reason IS NULL OR revoked_reason IN
                        ('logout', 'user_revoked', 'password_reset', 'admin_revoked', 'account_disabled', 'mfa_failed', 'mfa_reset')),

    CONSTRAINT sessions_token_hash_key UNIQUE (token_hash),
    CONSTRAINT sessions_revoked_has_reason CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);

CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- ---------------------------------------------------------------- email tokens

-- Single-use, revocable tokens sent by email. Unlike the stateless lead
-- tokens (ADR-009 in decisions/prelaunch), these grant account access, so
-- they are stored (hashed), consumed once, and short-lived.
CREATE TABLE auth_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     text NOT NULL CHECK (purpose IN ('verify_email', 'reset_password')),
    token_hash  bytea NOT NULL,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT auth_tokens_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX auth_tokens_user_open_idx ON auth_tokens (user_id, purpose) WHERE consumed_at IS NULL;

-- ---------------------------------------------------------------- MFA

CREATE TABLE mfa_totp (
    user_id           uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- AES-256-GCM sealed secret (internal/platform/crypto), bound to user_id.
    secret_ciphertext bytea NOT NULL,
    -- NULL while enrolment is started but not yet confirmed with a code.
    confirmed_at      timestamptz,
    -- The last 30-second step accepted, so a code cannot be replayed.
    last_used_step    bigint,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- RBAC

CREATE TABLE roles (
    key         text PRIMARY KEY CHECK (key ~ '^[a-z][a-z_]{1,39}$'),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    -- <resource>.<action>, e.g. leads.export
    key         text PRIMARY KEY CHECK (key ~ '^[a-z][a-z_]*\.[a-z][a-z_]*$'),
    description text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
    role_key       text NOT NULL REFERENCES roles (key) ON DELETE CASCADE,
    permission_key text NOT NULL REFERENCES permissions (key) ON DELETE CASCADE,
    PRIMARY KEY (role_key, permission_key)
);

CREATE TABLE user_roles (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_key   text NOT NULL REFERENCES roles (key) ON DELETE RESTRICT,
    -- A scoped assignment applies only inside one operational context, such as
    -- a farmer's own supplier organisation. Global permission checks ignore
    -- scoped assignments; scoped checks arrive with the modules that need them.
    scope_type text CHECK (scope_type IS NULL OR scope_type ~ '^[a-z][a-z_]{1,39}$'),
    scope_id   uuid,
    granted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT user_roles_scope_pair CHECK ((scope_type IS NULL) = (scope_id IS NULL)),
    CONSTRAINT user_roles_unique UNIQUE NULLS NOT DISTINCT (user_id, role_key, scope_type, scope_id)
);

CREATE INDEX user_roles_user_idx ON user_roles (user_id);

INSERT INTO roles (key, name, description) VALUES
    ('customer',      'Customer',            'A person with a Mise account who orders meal kits.'),
    ('farmer',        'Farmer',              'A supplier of produce. Scoped to their supplier organisation.'),
    ('chef',          'Chef',                'Designs recipes and menus.'),
    ('kitchen_staff', 'Kitchen staff',       'Prepares and packs kits. Scoped to a kitchen.'),
    ('nutritionist',  'Nutritionist',        'Reviews and validates recipes and meal plans.'),
    ('operations',    'Operations',          'Runs day-to-day operations.'),
    ('marketing',     'Marketing',           'Works with the waitlist, campaigns and referrals.'),
    ('support',       'Support',             'Helps customers and leads.'),
    ('finance',       'Finance',             'Billing, payments and refunds.'),
    ('admin',         'Administrator',       'Full access to the admin console except managing staff.'),
    ('super_admin',   'Super administrator', 'Everything, including staff accounts and roles.');

-- Only the permissions that guard something that exists today. Later modules
-- add theirs (orders.manage, recipes.approve, ...) in their own migrations.
INSERT INTO permissions (key, description) VALUES
    ('admin.access',   'Sign in to the admin console. Accounts holding it must use MFA outside local development.'),
    ('analytics.view', 'View dashboard statistics.'),
    ('leads.read',     'View leads, their preferences, attribution, referrals and timeline.'),
    ('leads.update',   'Change a lead''s status.'),
    ('leads.export',   'Download the lead list as CSV. Every export is audited.'),
    ('leads.erase',    'Permanently delete a lead (data-subject erasure).'),
    ('users.read',     'View staff accounts and their roles.'),
    ('users.manage',   'Create and disable staff accounts, grant and revoke roles, reset MFA.');

-- The pre-launch role map (internal/admin/auth.go), carried over unchanged.
INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('super_admin', 'admin.access'), ('super_admin', 'analytics.view'), ('super_admin', 'leads.read'),
    ('super_admin', 'leads.update'), ('super_admin', 'leads.export'), ('super_admin', 'leads.erase'),
    ('super_admin', 'users.read'), ('super_admin', 'users.manage'),
    ('admin', 'admin.access'), ('admin', 'analytics.view'), ('admin', 'leads.read'),
    ('admin', 'leads.update'), ('admin', 'leads.export'), ('admin', 'leads.erase'), ('admin', 'users.read'),
    ('marketing', 'admin.access'), ('marketing', 'analytics.view'), ('marketing', 'leads.read'), ('marketing', 'leads.export'),
    ('operations', 'admin.access'), ('operations', 'analytics.view'), ('operations', 'leads.read'),
    ('support', 'admin.access'), ('support', 'leads.read'), ('support', 'leads.update');

-- ---------------------------------------------------------------- customers

CREATE TABLE customers (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The waitlist lead this customer was, when there is one. The lead row is
    -- never copied or changed by the link: its referral, attribution, consent
    -- and event history stay where they are and are read through this key.
    -- Erasing the lead clears the link and nothing else.
    lead_id           uuid REFERENCES leads (id) ON DELETE SET NULL,
    lead_linked_at    timestamptz,
    terms_version     text NOT NULL,
    terms_accepted_at timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT customers_user_id_key UNIQUE (user_id),
    -- A lead becomes at most one customer.
    CONSTRAINT customers_lead_id_key UNIQUE (lead_id),
    CONSTRAINT customers_link_has_time CHECK (lead_id IS NULL OR lead_linked_at IS NOT NULL)
);

-- ---------------------------------------------------------------- audit

-- New entries name the acting user. actor_admin_id stays for history and for
-- the previous release; it is backfilled into actor_user_id below.
ALTER TABLE audit_log ADD COLUMN actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE audit_log ADD COLUMN request_id text;
CREATE INDEX audit_log_actor_user_idx ON audit_log (actor_user_id, created_at DESC) WHERE actor_user_id IS NOT NULL;

-- ---------------------------------------------------------------- copy admins

-- The canonical email expression mirrors leads.NormalizeEmail. A collision
-- (two admins whose addresses canonicalise alike) fails the migration rather
-- than merging accounts.
INSERT INTO users (id, email, email_canonical, email_verified_at, first_name, status, last_login_at, created_at, updated_at)
SELECT
    a.id,
    a.email,
    CASE WHEN split_part(a.email, '@', 2) IN ('gmail.com', 'googlemail.com')
         THEN replace(split_part(split_part(a.email, '@', 1), '+', 1), '.', '') || '@gmail.com'
         ELSE split_part(split_part(a.email, '@', 1), '+', 1) || '@' || split_part(a.email, '@', 2)
    END,
    -- Admins were created by an operator; their address is treated as confirmed.
    a.created_at,
    left(a.name, 80),
    CASE WHEN a.disabled_at IS NULL THEN 'active' ELSE 'disabled' END,
    a.last_login_at,
    a.created_at,
    a.updated_at
FROM admin_users a;

INSERT INTO password_credentials (user_id, password_hash, updated_at)
SELECT id, password_hash, updated_at FROM admin_users;

INSERT INTO user_roles (user_id, role_key, granted_at)
SELECT a.id, a.role, a.created_at
FROM admin_users a
JOIN roles r ON r.key = a.role;

UPDATE audit_log SET actor_user_id = actor_admin_id WHERE actor_admin_id IS NOT NULL;

-- +goose Down

ALTER TABLE audit_log DROP COLUMN request_id;
ALTER TABLE audit_log DROP COLUMN actor_user_id;
DROP TABLE customers;
DROP TABLE user_roles;
DROP TABLE role_permissions;
DROP TABLE permissions;
DROP TABLE roles;
DROP TABLE mfa_totp;
DROP TABLE auth_tokens;
DROP TABLE sessions;
DROP TABLE password_credentials;
DROP TABLE users;
