-- Sprint 9.2: lightweight management finance.
--
-- Records the operating costs the platform did not hold (salaries, rent,
-- utilities, packaging, marketing...) so management can see contribution
-- and operating results. Not an accounting system and not audited
-- statements: collections, refunds, provider fees, ingredient costs and
-- delivery costs stay in their own authoritative tables
-- (docs/finance/FINANCIAL_METRIC_DEFINITIONS.md) and are never re-entered here.
--
-- A category with nothing recorded for a month is MISSING, not zero. It
-- becomes zero only by an explicit coverage confirmation.

-- +goose Up

CREATE TABLE finance_expense_categories (
    key         text PRIMARY KEY CHECK (key ~ '^[a-z][a-z_]{1,40}$'),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    -- How the category enters the results: variable costs reduce contribution margin;
    -- fixed, labour and marketing reduce the operating result.
    cost_class  text NOT NULL CHECK (cost_class IN ('variable', 'fixed', 'labor', 'marketing')),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
    position    integer NOT NULL DEFAULT 0
);
INSERT INTO finance_expense_categories (key, name, cost_class, description, position) VALUES
    ('salaries_wages', 'Salaries and wages', 'labor', 'Pay for staff and contractors paid like staff.', 1),
    ('kitchen_rent', 'Kitchen rent', 'fixed', 'Rent for the kitchen and storage space.', 2),
    ('utilities', 'Utilities', 'fixed', 'Electricity, water, gas, internet, phone.', 3),
    ('packaging', 'Packaging', 'variable', 'Boxes, liners, ice packs, labels. Not recorded anywhere else in the platform.', 4),
    ('marketing', 'Marketing', 'marketing', 'Advertising, promotions spend, content.', 5),
    ('insurance', 'Insurance', 'fixed', 'Business, liability, vehicle and property insurance.', 6),
    ('equipment_maintenance', 'Equipment maintenance', 'fixed', 'Repairs and servicing of kitchen and cold-chain equipment.', 7),
    ('transportation', 'Transportation', 'variable', 'Transport other than delivery partner costs (those are recorded on deliveries).', 8),
    ('payment_processing_fees', 'Payment processing fees', 'variable', 'Fees a provider does not report per payment (e.g. a monthly gateway fee). Per-payment fees come from the payment ledger.', 9),
    ('professional_services', 'Professional services', 'fixed', 'Accounting, legal and consulting.', 10),
    ('other_operating', 'Other operating expenses', 'fixed', 'Anything else, described on the record.', 11);

CREATE TABLE finance_recurring_expenses (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    category_key  text NOT NULL REFERENCES finance_expense_categories (key),
    description   text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 300),
    amount_minor  bigint NOT NULL CHECK (amount_minor > 0),
    currency      text NOT NULL DEFAULT 'TTD' CHECK (currency ~ '^[A-Z]{3}$'),
    cadence       text NOT NULL CHECK (cadence IN ('weekly', 'monthly', 'quarterly', 'yearly')),
    starts_on     date NOT NULL,
    ends_on       date CHECK (ends_on IS NULL OR ends_on >= starts_on),
    status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'approved', 'ended')),
    reference     text NOT NULL DEFAULT '' CHECK (char_length(reference) <= 300),
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT finance_recurring_approved CHECK (status = 'draft' OR approved_at IS NOT NULL)
);

CREATE TABLE finance_expenses (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    category_key  text NOT NULL REFERENCES finance_expense_categories (key),
    description   text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 300),
    amount_minor  bigint NOT NULL CHECK (amount_minor >= 0),
    currency      text NOT NULL DEFAULT 'TTD' CHECK (currency ~ '^[A-Z]{3}$'),
    -- actual: incurred; estimated: expected, to be replaced by the actual.
    kind          text NOT NULL CHECK (kind IN ('actual', 'estimated')),
    expense_date  date NOT NULL,
    payment_date  date,
    reference     text NOT NULL DEFAULT '' CHECK (char_length(reference) <= 300),
    status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'submitted', 'approved', 'rejected', 'void')),
    recurring_id  uuid REFERENCES finance_recurring_expenses (id) ON DELETE SET NULL,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT finance_expense_approved CHECK (status <> 'approved' OR approved_at IS NOT NULL),
    CONSTRAINT finance_expense_paid_actual CHECK (payment_date IS NULL OR kind = 'actual')
);
CREATE INDEX finance_expenses_period_idx ON finance_expenses (expense_date, category_key) WHERE status = 'approved';
-- One recorded expense per occurrence of a recurring schedule (it replaces the estimate).
CREATE UNIQUE INDEX finance_expenses_recurring_occurrence ON finance_expenses (recurring_id, expense_date)
    WHERE recurring_id IS NOT NULL AND status <> 'void';

CREATE TABLE finance_expense_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    expense_id  uuid REFERENCES finance_expenses (id) ON DELETE CASCADE,
    recurring_id uuid REFERENCES finance_recurring_expenses (id) ON DELETE CASCADE,
    action      text NOT NULL CHECK (action ~ '^[a-z_]{2,40}$'),
    from_status text NOT NULL DEFAULT '',
    to_status   text NOT NULL DEFAULT '',
    changes     jsonb NOT NULL DEFAULT '{}',
    note        text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT finance_expense_event_target CHECK ((expense_id IS NULL) <> (recurring_id IS NULL))
);
CREATE INDEX finance_expense_events_expense_idx ON finance_expense_events (expense_id, id);
CREATE TRIGGER finance_expense_events_immutable BEFORE UPDATE OR DELETE ON finance_expense_events
    FOR EACH ROW WHEN (pg_trigger_depth() = 0) EXECUTE FUNCTION forbid_change();

-- "Nothing to record in this category for this month": the only way a missing category counts as zero.
CREATE TABLE finance_coverage_confirmations (
    category_key text NOT NULL REFERENCES finance_expense_categories (key),
    month        date NOT NULL CHECK (extract(day FROM month) = 1),
    note         text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    confirmed_by uuid REFERENCES users (id) ON DELETE SET NULL,
    confirmed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (category_key, month)
);

-- Approved cash balances (an opening balance, or a reconciled bank balance on a date).
CREATE TABLE finance_cash_balances (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    as_of         date NOT NULL,
    account_label text NOT NULL CHECK (char_length(account_label) BETWEEN 1 AND 100),
    amount_minor  bigint NOT NULL,
    currency      text NOT NULL DEFAULT 'TTD' CHECK (currency ~ '^[A-Z]{3}$'),
    reference     text NOT NULL CHECK (char_length(reference) BETWEEN 1 AND 300),
    recorded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (as_of, account_label)
);

INSERT INTO permissions (key, description) VALUES
    ('finance_expenses.read',    'View management expenses, recurring costs, coverage and cash balances.'),
    ('finance_expenses.manage',  'Record and edit management expenses and recurring costs; confirm coverage.'),
    ('finance_expenses.approve', 'Approve or reject expenses and recurring costs; record cash balances.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('super_admin', ARRAY['finance_expenses.read', 'finance_expenses.manage', 'finance_expenses.approve']),
    ('admin',       ARRAY['finance_expenses.read', 'finance_expenses.manage']),
    ('finance',     ARRAY['finance_expenses.read', 'finance_expenses.manage', 'finance_expenses.approve'])
) AS g(r, perms), unnest(perms) AS p;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key LIKE 'finance_expenses.%';
DELETE FROM permissions WHERE key LIKE 'finance_expenses.%';
DROP TABLE finance_cash_balances;
DROP TABLE finance_coverage_confirmations;
DROP TABLE finance_expense_events;
DROP TABLE finance_expenses;
DROP TABLE finance_recurring_expenses;
DROP TABLE finance_expense_categories;
