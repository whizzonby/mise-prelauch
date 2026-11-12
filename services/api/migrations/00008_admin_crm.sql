-- +goose Up

-- Sprint 4: administration and CRM. These tables hold only what is new:
-- CRM stage, assignment and notes, support cases, campaigns, segments, staff
-- invitations, staff alerts, exports and contact settings. Customers, leads,
-- subscriptions, orders and payments stay in their own modules and are read
-- from there; nothing here copies them.

-- ---------------------------------------------------------------- audit

-- Why a sensitive change was made (a reason code), where one is required.
ALTER TABLE audit_log ADD COLUMN reason text CHECK (reason IS NULL OR char_length(reason) <= 300);
CREATE INDEX audit_log_action_idx ON audit_log (action, created_at DESC);

-- The audit log is append-only. The only deletion allowed is a lead's
-- erasure (it removes that lead's own entries), which marks its
-- transaction with SET LOCAL mise.audit_erasure = 'on'. The only update
-- allowed is the database clearing an actor when that user row is deleted.
-- +goose StatementBegin
CREATE FUNCTION audit_log_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('mise.audit_erasure', true) = 'on' AND OLD.target_type = 'lead' THEN
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE'
       AND (NEW.actor_user_id IS NULL OR NEW.actor_user_id IS NOT DISTINCT FROM OLD.actor_user_id)
       AND (NEW.actor_admin_id IS NULL OR NEW.actor_admin_id IS NOT DISTINCT FROM OLD.actor_admin_id)
       AND (NEW.action, NEW.target_type, NEW.target_id, NEW.metadata, NEW.created_at, NEW.request_id, NEW.reason)
           IS NOT DISTINCT FROM (OLD.action, OLD.target_type, OLD.target_id, OLD.metadata, OLD.created_at, OLD.request_id, OLD.reason) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'audit_log is append-only' USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_log_append_only BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();

-- ---------------------------------------------------------------- CRM

-- The CRM stage of a lead, beside (not instead of) its lifecycle status in
-- leads.status. "Converted" and "unsubscribed" are not stages: they come
-- from the lifecycle (a linked customer; UNSUBSCRIBED).
CREATE TABLE crm_lead_profiles (
    lead_id          uuid PRIMARY KEY REFERENCES leads (id) ON DELETE CASCADE,
    stage            text NOT NULL DEFAULT 'new' CHECK (stage IN ('new', 'contacted', 'qualified', 'unqualified')),
    assigned_to      uuid REFERENCES users (id) ON DELETE SET NULL,
    stage_changed_at timestamptz NOT NULL DEFAULT now(),
    updated_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX crm_lead_profiles_stage_idx ON crm_lead_profiles (stage);
CREATE INDEX crm_lead_profiles_assignee_idx ON crm_lead_profiles (assigned_to) WHERE assigned_to IS NOT NULL;

-- Leads the team already marked as qualified start at that stage.
INSERT INTO crm_lead_profiles (lead_id, stage)
SELECT id, 'qualified' FROM leads WHERE status = 'QUALIFIED';

-- Internal notes about a lead or a customer. Never shown to them.
CREATE TABLE crm_notes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id     uuid REFERENCES leads (id) ON DELETE CASCADE,
    customer_id uuid REFERENCES customers (id) ON DELETE CASCADE,
    body        text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    author_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT crm_notes_one_subject CHECK ((lead_id IS NULL) <> (customer_id IS NULL))
);

CREATE INDEX crm_notes_lead_idx ON crm_notes (lead_id, created_at DESC) WHERE lead_id IS NOT NULL;
CREATE INDEX crm_notes_customer_idx ON crm_notes (customer_id, created_at DESC) WHERE customer_id IS NOT NULL;

-- Saved customer segments: explainable filters over stored data. The rule
-- keys are checked in Go (internal/crm/segments.go).
CREATE TABLE crm_segments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
    rules       jsonb NOT NULL DEFAULT '{}',
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT crm_segments_name_key UNIQUE (name)
);

-- ---------------------------------------------------------------- support

CREATE SEQUENCE support_case_number_seq;

CREATE TABLE support_cases (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number       bigint NOT NULL DEFAULT nextval('support_case_number_seq'),
    customer_id  uuid REFERENCES customers (id) ON DELETE SET NULL,
    lead_id      uuid REFERENCES leads (id) ON DELETE SET NULL,
    subject      text NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 160),
    category     text NOT NULL CHECK (category IN ('subscription', 'billing', 'meal_selection', 'delivery', 'account_access', 'product_feedback', 'general')),
    priority     text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'waiting_for_customer', 'resolved', 'closed')),
    assigned_to  uuid REFERENCES users (id) ON DELETE SET NULL,
    resolution   text CHECK (resolution IS NULL OR char_length(resolution) <= 2000),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    resolved_at  timestamptz,
    closed_at    timestamptz,

    CONSTRAINT support_cases_number_key UNIQUE (number),
    CONSTRAINT support_cases_resolution CHECK (status NOT IN ('resolved', 'closed') OR resolution IS NOT NULL)
);

CREATE INDEX support_cases_status_idx ON support_cases (status, priority, created_at DESC);
CREATE INDEX support_cases_customer_idx ON support_cases (customer_id, created_at DESC) WHERE customer_id IS NOT NULL;
CREATE INDEX support_cases_assignee_idx ON support_cases (assigned_to, status) WHERE assigned_to IS NOT NULL;

-- What happened on a case: notes, status, priority and assignment changes.
CREATE TABLE support_case_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    case_id    uuid NOT NULL REFERENCES support_cases (id) ON DELETE CASCADE,
    type       text NOT NULL CHECK (type IN ('created', 'note', 'status', 'priority', 'assignment', 'resolution')),
    body       text CHECK (body IS NULL OR char_length(body) <= 2000),
    from_value text,
    to_value   text,
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX support_case_events_case_idx ON support_case_events (case_id, id);

-- ---------------------------------------------------------------- campaigns

-- Campaign records. Nothing is sent from here: sending belongs to an
-- approved marketing provider, which is not connected.
CREATE TABLE marketing_campaigns (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    objective    text NOT NULL CHECK (objective IN ('acquisition', 'activation', 'retention', 'win_back', 'referral', 'awareness')),
    channel      text NOT NULL CHECK (channel IN ('email', 'social', 'paid_social', 'search', 'partnership', 'events', 'other')),
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'active', 'completed', 'cancelled')),
    -- The utm_campaign value links visits and signups to this campaign.
    utm_campaign text CHECK (utm_campaign IS NULL OR utm_campaign ~ '^[a-z0-9][a-z0-9_-]{1,59}$'),
    segment_id   uuid REFERENCES crm_segments (id) ON DELETE SET NULL,
    promotion_id uuid REFERENCES promotions (id) ON DELETE SET NULL,
    starts_at    timestamptz,
    ends_at      timestamptz,
    notes        text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT marketing_campaigns_utm_key UNIQUE (utm_campaign),
    CONSTRAINT marketing_campaigns_window CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);

-- ---------------------------------------------------------------- staff

-- A single-use invitation to a staff account. Only the token's hash is kept.
CREATE TABLE staff_invitations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email       text NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254),
    email_canonical text NOT NULL,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    role_key    text NOT NULL REFERENCES roles (key) ON DELETE RESTRICT,
    token_hash  bytea NOT NULL,
    invited_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT staff_invitations_token_key UNIQUE (token_hash)
);

-- At most one open invitation per address.
CREATE UNIQUE INDEX staff_invitations_one_open ON staff_invitations (email_canonical)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- ---------------------------------------------------------------- alerts

-- Things staff should look at. One row per dedupe key, so a retried event
-- or a second scheduler run never raises the same alert twice. Only staff
-- holding `permission` see an alert.
CREATE TABLE admin_alerts (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dedupe_key    text NOT NULL CHECK (char_length(dedupe_key) BETWEEN 1 AND 200),
    kind          text NOT NULL CHECK (char_length(kind) BETWEEN 1 AND 60),
    severity      text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body          text NOT NULL DEFAULT '' CHECK (char_length(body) <= 1000),
    resource_type text,
    resource_id   text,
    permission    text NOT NULL REFERENCES permissions (key) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    resolved_at   timestamptz,

    CONSTRAINT admin_alerts_dedupe_key UNIQUE (dedupe_key)
);

CREATE INDEX admin_alerts_open_idx ON admin_alerts (created_at DESC) WHERE resolved_at IS NULL;

CREATE TABLE admin_alert_reads (
    alert_id bigint NOT NULL REFERENCES admin_alerts (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    read_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (alert_id, user_id)
);

-- ---------------------------------------------------------------- exports

-- CSV exports, built by a background job, downloadable by the person who
-- asked until they expire.
CREATE TABLE report_exports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         text NOT NULL CHECK (kind IN ('customers', 'subscriptions', 'orders', 'payments', 'leads')),
    params       jsonb NOT NULL DEFAULT '{}',
    status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'ready', 'failed', 'expired')),
    requested_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    row_count    integer,
    truncated    boolean NOT NULL DEFAULT false,
    content      bytea,
    error        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at   timestamptz NOT NULL DEFAULT now() + interval '24 hours'
);

CREATE INDEX report_exports_requester_idx ON report_exports (requested_by, created_at DESC);

-- ---------------------------------------------------------------- settings

-- Contact details the business can change without a deployment. Policies
-- owned elsewhere (cutoff, tax, fees, payments, MFA) are not stored here.
CREATE TABLE business_settings (
    key        text PRIMARY KEY CHECK (key IN ('business.display_name', 'business.support_email', 'business.support_phone',
                                               'business.address', 'business.whatsapp')),
    value      text NOT NULL CHECK (char_length(value) <= 500),
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- permissions

INSERT INTO permissions (key, description) VALUES
    ('customers.read',      'View customer accounts, subscriptions, orders and billing.'),
    ('customers.sensitive', 'View allergies, delivery addresses and phone numbers.'),
    ('customers.export',    'Export customer, subscription, order and payment lists as CSV.'),
    ('leads.manage',    'Set lead stages and assignees, and add notes to leads and customers.'),
    ('cases.read',      'View support cases.'),
    ('cases.manage',    'Open, update, assign and resolve support cases.'),
    ('campaigns.manage',    'Create and change marketing campaigns and customer segments.'),
    ('subscriptions.manage','Pause, resume or cancel a subscription on a customer''s behalf, with a reason.'),
    ('reports.read',        'View business reports and the operational overview.'),
    ('audit.read',          'View the audit log.'),
    ('settings.manage',     'Change business contact settings.');

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('super_admin', 'customers.read'), ('super_admin', 'customers.sensitive'), ('super_admin', 'customers.export'),
    ('super_admin', 'leads.manage'), ('super_admin', 'cases.read'), ('super_admin', 'cases.manage'),
    ('super_admin', 'campaigns.manage'), ('super_admin', 'subscriptions.manage'), ('super_admin', 'reports.read'),
    ('super_admin', 'audit.read'), ('super_admin', 'settings.manage'),
    ('admin', 'customers.read'), ('admin', 'customers.sensitive'), ('admin', 'customers.export'),
    ('admin', 'leads.manage'), ('admin', 'cases.read'), ('admin', 'cases.manage'),
    ('admin', 'campaigns.manage'), ('admin', 'subscriptions.manage'), ('admin', 'reports.read'), ('admin', 'audit.read'),
    -- Support helps customers: their accounts and cases, addresses for delivery questions.
    ('support', 'customers.read'), ('support', 'customers.sensitive'), ('support', 'cases.read'), ('support', 'cases.manage'),
    ('support', 'leads.manage'), ('support', 'subscriptions.manage'),
    -- Operations: the weekly flow, and addresses and allergies for preparing orders.
    ('operations', 'customers.read'), ('operations', 'customers.sensitive'), ('operations', 'cases.read'),
    ('operations', 'reports.read'),
    -- Marketing: leads, segments, campaigns; no health data, no exports of customers.
    ('marketing', 'leads.manage'), ('marketing', 'campaigns.manage'), ('marketing', 'reports.read'),
    -- Finance: billing views, reports and finance exports.
    ('finance', 'customers.read'), ('finance', 'customers.export'), ('finance', 'reports.read'), ('finance', 'cases.read');

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('customers.read', 'customers.sensitive', 'customers.export',
    'leads.manage', 'cases.read', 'cases.manage', 'campaigns.manage', 'subscriptions.manage', 'reports.read',
    'audit.read', 'settings.manage');
DROP TABLE business_settings;
DROP TABLE report_exports;
DROP TABLE admin_alert_reads;
DROP TABLE admin_alerts;
DELETE FROM permissions WHERE key IN ('customers.read', 'customers.sensitive', 'customers.export',
    'leads.manage', 'cases.read', 'cases.manage', 'campaigns.manage', 'subscriptions.manage', 'reports.read',
    'audit.read', 'settings.manage');
DROP TABLE staff_invitations;
DROP TABLE marketing_campaigns;
DROP TABLE support_case_events;
DROP TABLE support_cases;
DROP SEQUENCE support_case_number_seq;
DROP TABLE crm_segments;
DROP TABLE crm_notes;
DROP TABLE crm_lead_profiles;
DROP TRIGGER audit_log_append_only ON audit_log;
DROP FUNCTION audit_log_append_only();
DROP INDEX audit_log_action_idx;
ALTER TABLE audit_log DROP COLUMN reason;
