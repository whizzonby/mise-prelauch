-- +goose Up

-- Sprint 8: analytics (internal/analytics), automation (internal/automation)
-- and forecasting (internal/forecasting). Additive only: these modules read
-- the domain tables and write only their own. Thresholds, recipients and
-- activation criteria ship unset: they are business decisions.

-- ---------------------------------------------------------------- analytics

-- One refresh of daily metric snapshots (scheduled, event-driven, backfill).
CREATE TABLE analytics_refresh_runs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reason      text NOT NULL CHECK (reason IN ('schedule', 'event', 'backfill', 'manual')),
    from_day    date NOT NULL,
    to_day      date NOT NULL,
    status      text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed')),
    values_written integer NOT NULL DEFAULT 0,
    error       text CHECK (error IS NULL OR char_length(error) <= 1000),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,

    CONSTRAINT analytics_refresh_runs_range CHECK (to_day >= from_day)
);

CREATE INDEX analytics_refresh_runs_started_idx ON analytics_refresh_runs (started_at DESC);

-- A metric's value for one local day. Upserted: refreshing the same day
-- twice (replay, backfill, late events) replaces the value, never adds.
CREATE TABLE analytics_snapshots (
    metric_id      text NOT NULL CHECK (metric_id ~ '^[a-z_]+\.[a-z_]+$'),
    day            date NOT NULL,
    value          numeric,
    refresh_run_id bigint REFERENCES analytics_refresh_runs (id) ON DELETE SET NULL,
    computed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (metric_id, day)
);

-- The result of each data-quality check run.
CREATE TABLE analytics_quality_results (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    check_id   text NOT NULL,
    status     text NOT NULL CHECK (status IN ('ok', 'warning', 'failed')),
    count      integer NOT NULL DEFAULT 0,
    detail     text NOT NULL DEFAULT '' CHECK (char_length(detail) <= 2000),
    checked_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX analytics_quality_results_idx ON analytics_quality_results (check_id, id DESC);

-- ---------------------------------------------------------------- automation

-- One rule per kind. Kinds are typed in code; parameters are validated per
-- kind. A rule can be enabled only with an approval reference (and the
-- kind's required parameters, checked in code).
CREATE TABLE automation_rules (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind             text NOT NULL,
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description      text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    enabled          boolean NOT NULL DEFAULT false,
    params           jsonb NOT NULL DEFAULT '{}',
    action           text NOT NULL CHECK (action IN ('alert', 'task', 'request_approval')),
    owner_permission text NOT NULL REFERENCES permissions (key) ON DELETE RESTRICT,
    priority         text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'critical')),
    cooldown_minutes integer CHECK (cooldown_minutes IS NULL OR cooldown_minutes BETWEEN 1 AND 43200),
    approval_ref     text NOT NULL DEFAULT '' CHECK (char_length(approval_ref) <= 200),
    version          integer NOT NULL DEFAULT 1,
    updated_by       uuid, -- no FK: seeded configuration outlives accounts (as delivery_settings)
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    last_evaluated_at timestamptz,
    enabled_at       timestamptz, -- when it was last switched on: only records after this trigger it

    CONSTRAINT automation_rules_kind_key UNIQUE (kind),
    CONSTRAINT automation_rules_enabled CHECK (NOT enabled OR (approval_ref <> '' AND cooldown_minutes IS NOT NULL))
);

-- Every version of a rule's configuration, append-only.
CREATE TABLE automation_rule_versions (
    rule_id      uuid NOT NULL REFERENCES automation_rules (id) ON DELETE CASCADE,
    version      integer NOT NULL,
    config       jsonb NOT NULL,
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, version)
);

CREATE TRIGGER automation_rule_versions_append_only BEFORE UPDATE OR DELETE ON automation_rule_versions
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- One occurrence of a rule for one subject. The dedupe key (rule, subject,
-- cooldown window) makes a replayed trigger or a second worker a no-op.
CREATE TABLE automation_executions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id       uuid NOT NULL REFERENCES automation_rules (id) ON DELETE CASCADE,
    rule_version  integer NOT NULL,
    trigger       text NOT NULL CHECK (trigger IN ('schedule', 'event', 'manual')),
    trigger_ref   text NOT NULL DEFAULT '' CHECK (char_length(trigger_ref) <= 200),
    subject_type  text NOT NULL,
    subject_id    text NOT NULL,
    subject_label text NOT NULL DEFAULT '' CHECK (char_length(subject_label) <= 300),
    dedupe_key    text NOT NULL,
    action        text NOT NULL,
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed', 'awaiting_approval',
        'approved', 'rejected')),
    attempts      integer NOT NULL DEFAULT 0,
    last_error    text CHECK (last_error IS NULL OR char_length(last_error) <= 1000),
    result        jsonb NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    decided_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    decided_at    timestamptz,
    decision_note text CHECK (decision_note IS NULL OR char_length(decision_note) <= 1000),

    CONSTRAINT automation_executions_dedupe_key UNIQUE (dedupe_key)
);

CREATE INDEX automation_executions_rule_idx ON automation_executions (rule_id, created_at DESC);
CREATE INDEX automation_executions_status_idx ON automation_executions (status, created_at DESC);

CREATE TABLE automation_execution_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    execution_id  uuid NOT NULL REFERENCES automation_executions (id) ON DELETE CASCADE,
    type          text NOT NULL CHECK (type IN ('created', 'attempt', 'succeeded', 'failed', 'retry_requested', 'approved', 'rejected')),
    body          text CHECK (body IS NULL OR char_length(body) <= 1000),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER automation_execution_events_append_only BEFORE UPDATE OR DELETE ON automation_execution_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- An internal task for a team (staff holding a permission).
CREATE TABLE automation_tasks (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id  uuid REFERENCES automation_executions (id) ON DELETE SET NULL,
    title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    detail        text NOT NULL DEFAULT '' CHECK (char_length(detail) <= 2000),
    permission    text NOT NULL REFERENCES permissions (key) ON DELETE RESTRICT,
    link          text NOT NULL DEFAULT '' CHECK (char_length(link) <= 300),
    status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'dismissed')),
    assigned_to   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    closed_at     timestamptz,
    closed_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    close_note    text CHECK (close_note IS NULL OR char_length(close_note) <= 1000),

    CONSTRAINT automation_tasks_closed CHECK ((status = 'open') = (closed_at IS NULL))
);

CREATE INDEX automation_tasks_open_idx ON automation_tasks (status, created_at DESC);

-- The six recipes, disabled and without thresholds: the business sets them.
INSERT INTO automation_rules (kind, name, description, action, owner_permission, priority) VALUES
    ('procurement_shortage', 'Procurement shortage', 'A released production plan has ingredient lines still short (not covered by stock or an order).', 'alert', 'procurement.plan', 'high'),
    ('overdue_purchase_order', 'Overdue purchase order', 'An issued purchase order is past its expected date and not fully received.', 'task', 'receiving.manage', 'normal'),
    ('production_hold_unresolved', 'Unresolved quality hold', 'A quality hold has stayed open longer than the approved interval.', 'alert', 'quality.release_hold', 'high'),
    ('delivery_failure_unresolved', 'Unresolved failed delivery', 'A failed delivery has had no next step longer than the approved interval.', 'alert', 'delivery_exceptions.manage', 'high'),
    ('run_unreconciled', 'Dispatch run not reconciled', 'A completed dispatch run has not been reconciled within the approved interval.', 'task', 'delivery.reconcile', 'normal'),
    ('payment_failed', 'Payment failed', 'A week''s payment failed: ask finance to follow up (nothing is charged or changed automatically).', 'request_approval', 'payments.read', 'normal');

-- ---------------------------------------------------------------- scheduled reports

CREATE TABLE scheduled_reports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         text NOT NULL CHECK (kind IN ('daily_operations', 'weekly_business', 'weekly_production', 'weekly_procurement', 'monthly_executive')),
    name         text NOT NULL,
    enabled      boolean NOT NULL DEFAULT false,
    -- Staff accounts that receive it by email; each must hold the report's permission.
    recipients   uuid[] NOT NULL DEFAULT '{}',
    approval_ref text NOT NULL DEFAULT '' CHECK (char_length(approval_ref) <= 200),
    updated_by   uuid, -- no FK: seeded configuration outlives accounts (as delivery_settings)
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT scheduled_reports_kind_key UNIQUE (kind),
    CONSTRAINT scheduled_reports_enabled CHECK (NOT enabled OR approval_ref <> '')
);

INSERT INTO scheduled_reports (kind, name) VALUES
    ('daily_operations', 'Daily operations summary'),
    ('weekly_business', 'Weekly business performance'),
    ('weekly_production', 'Weekly production and waste'),
    ('weekly_procurement', 'Weekly procurement and inventory'),
    ('monthly_executive', 'Monthly executive review');

-- One generated report per schedule and period: a second run for the same
-- occurrence is refused by the key.
CREATE TABLE report_runs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id  uuid NOT NULL REFERENCES scheduled_reports (id) ON DELETE CASCADE,
    period_start date NOT NULL,
    period_end   date NOT NULL,
    status       text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed')),
    content      jsonb,
    csv          text,
    error        text CHECK (error IS NULL OR char_length(error) <= 1000),
    trigger      text NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    started_at   timestamptz NOT NULL DEFAULT now(),
    generated_at timestamptz,

    CONSTRAINT report_runs_occurrence_key UNIQUE (schedule_id, period_start)
);

-- Each email of a report run and its real outcome.
CREATE TABLE report_run_deliveries (
    run_id     uuid NOT NULL REFERENCES report_runs (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status     text NOT NULL CHECK (status IN ('queued', 'sent', 'failed', 'skipped')),
    reason     text CHECK (reason IS NULL OR char_length(reason) <= 300),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, user_id)
);

-- ---------------------------------------------------------------- forecasting

-- Activation criteria and method limits. Unset: no forecast can be activated.
CREATE TABLE forecast_settings (
    id                    boolean PRIMARY KEY DEFAULT true CHECK (id),
    min_weeks             integer CHECK (min_weeks IS NULL OR min_weeks BETWEEN 4 AND 520),
    max_missing_share     numeric(4, 3) CHECK (max_missing_share IS NULL OR max_missing_share BETWEEN 0 AND 1),
    min_improvement       numeric(4, 3) CHECK (min_improvement IS NULL OR min_improvement BETWEEN 0 AND 1),
    min_interval_residuals integer CHECK (min_interval_residuals IS NULL OR min_interval_residuals BETWEEN 5 AND 1000),
    horizon_weeks         integer NOT NULL DEFAULT 4 CHECK (horizon_weeks BETWEEN 1 AND 12),
    season_weeks          integer CHECK (season_weeks IS NULL OR season_weeks BETWEEN 2 AND 52),
    approval_ref          text NOT NULL DEFAULT '' CHECK (char_length(approval_ref) <= 200),
    updated_by            uuid,
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT forecast_settings_approved CHECK (min_weeks IS NULL OR approval_ref <> '')
);
INSERT INTO forecast_settings DEFAULT VALUES;

-- Every forecast run, kept: refused runs record why.
CREATE TABLE forecast_runs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger        text NOT NULL CHECK (trigger IN ('manual', 'schedule')),
    actor_user_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    status         text NOT NULL CHECK (status IN ('refused', 'completed')),
    input_from     date,
    input_to       date,
    method         text,
    method_version text,
    horizon_weeks  integer NOT NULL,
    readiness      jsonb NOT NULL,
    notes          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER forecast_runs_append_only BEFORE UPDATE OR DELETE ON forecast_runs
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE forecast_values (
    run_id   uuid NOT NULL REFERENCES forecast_runs (id) ON DELETE RESTRICT,
    meal_id  uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    week     date NOT NULL,
    expected numeric(12, 2) NOT NULL CHECK (expected >= 0),
    lower    numeric(12, 2),
    upper    numeric(12, 2),
    PRIMARY KEY (run_id, meal_id, week)
);

CREATE TRIGGER forecast_values_append_only BEFORE UPDATE OR DELETE ON forecast_values
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE forecast_backtests (
    run_id   uuid NOT NULL REFERENCES forecast_runs (id) ON DELETE RESTRICT,
    method   text NOT NULL,
    folds    integer NOT NULL,
    mae      numeric(14, 4),
    wape     numeric(14, 4),
    bias     numeric(14, 4),
    mase     numeric(14, 4),
    selected boolean NOT NULL DEFAULT false,
    PRIMARY KEY (run_id, method)
);

CREATE TRIGGER forecast_backtests_append_only BEFORE UPDATE OR DELETE ON forecast_backtests
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- consumers

INSERT INTO event_consumer_offsets (consumer) VALUES ('analytics'), ('automation') ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------- roles and permissions

INSERT INTO roles (key, name, description) VALUES
    ('analyst', 'Analyst', 'Reads every analytics area, forecasts and automation history, and exports reports.');

INSERT INTO permissions (key, description) VALUES
    ('analytics_executive.read',   'View the executive overview and cross-functional insights.'),
    ('analytics_revenue.read',     'View revenue and subscription analytics.'),
    ('analytics_customers.read',   'View customer, cohort and retention analytics.'),
    ('analytics_production.read',  'View meal and production analytics.'),
    ('analytics_procurement.read', 'View procurement analytics.'),
    ('analytics_inventory.read',   'View inventory analytics.'),
    ('analytics_delivery.read',    'View delivery analytics.'),
    ('analytics.export',           'Export analytics to CSV and run analytics refreshes.'),
    ('automation.read',            'View automation rules, executions, tasks and scheduled reports.'),
    ('automation.manage',          'Configure automation rules and scheduled reports.'),
    ('automation.execute',         'Run rules and reports on demand and retry failed executions.'),
    ('automation.approve',         'Decide approvals requested by automation.'),
    ('forecasting.read',           'View demand history, readiness, forecasts and their accuracy.'),
    ('forecasting.run',            'Run forecasts.'),
    ('forecasting.configure',      'Set forecast activation criteria.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM unnest(ARRAY['super_admin', 'admin']) AS r,
     unnest(ARRAY['analytics_executive.read', 'analytics_revenue.read', 'analytics_customers.read', 'analytics_production.read',
                  'analytics_procurement.read', 'analytics_inventory.read', 'analytics_delivery.read', 'analytics.export', 'automation.read',
                  'automation.manage', 'automation.execute', 'automation.approve', 'forecasting.read', 'forecasting.run',
                  'forecasting.configure']) AS p;

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('analyst', ARRAY['admin.access', 'analytics_executive.read', 'analytics_revenue.read', 'analytics_customers.read', 'analytics_production.read',
        'analytics_procurement.read', 'analytics_inventory.read', 'analytics_delivery.read', 'analytics.export', 'automation.read', 'forecasting.read']),
    ('finance', ARRAY['analytics_revenue.read', 'analytics_executive.read', 'automation.read', 'automation.approve']),
    ('operations', ARRAY['analytics_executive.read', 'analytics_production.read', 'analytics_delivery.read', 'automation.read']),
    ('production_manager', ARRAY['analytics_production.read', 'forecasting.read', 'automation.read']),
    ('procurement_manager', ARRAY['analytics_procurement.read', 'analytics_inventory.read', 'forecasting.read', 'automation.read']),
    ('inventory_manager', ARRAY['analytics_inventory.read', 'automation.read']),
    ('dispatch_manager', ARRAY['analytics_delivery.read', 'automation.read'])
) AS g(r, perms), unnest(perms) AS p
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('analytics_executive.read', 'analytics_revenue.read', 'analytics_customers.read',
    'analytics_production.read', 'analytics_procurement.read', 'analytics_inventory.read', 'analytics_delivery.read', 'analytics.export',
    'automation.read', 'automation.manage', 'automation.execute', 'automation.approve', 'forecasting.read', 'forecasting.run', 'forecasting.configure');
DELETE FROM role_permissions WHERE role_key = 'analyst';
DELETE FROM user_roles WHERE role_key = 'analyst';
DELETE FROM roles WHERE key = 'analyst';
DROP TABLE forecast_backtests;
DROP TABLE forecast_values;
DROP TABLE forecast_runs;
DROP TABLE forecast_settings;
DROP TABLE report_run_deliveries;
DROP TABLE report_runs;
DROP TABLE scheduled_reports;
DROP TABLE automation_tasks;
DROP TABLE automation_execution_events;
DROP TABLE automation_executions;
DROP TABLE automation_rule_versions;
DROP TABLE automation_rules;
DROP TABLE analytics_quality_results;
DROP TABLE analytics_snapshots;
DROP TABLE analytics_refresh_runs;
DELETE FROM permissions WHERE key IN ('analytics_executive.read', 'analytics_revenue.read', 'analytics_customers.read',
    'analytics_production.read', 'analytics_procurement.read', 'analytics_inventory.read', 'analytics_delivery.read', 'analytics.export',
    'automation.read', 'automation.manage', 'automation.execute', 'automation.approve', 'forecasting.read', 'forecasting.run', 'forecasting.configure');
DELETE FROM event_consumer_offsets WHERE consumer IN ('analytics', 'automation');
