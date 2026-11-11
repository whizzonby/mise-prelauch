-- +goose Up

-- Sprint 3: subscription commerce. Plans and price versions, subscriptions
-- and their weekly cycles, weekly menus and meal selections, orders, billing
-- cycles and invoices, payments through a provider-agnostic layer, refunds,
-- promotions. See docs/database/SPRINT_3_SCHEMA.md.
--
-- Additive: no existing table is changed except for new permissions.
-- Money is always bigint minor units (cents) beside a currency code; nothing
-- here is floating point. Timestamps are UTC (timestamptz); a delivery week
-- is identified by its Monday as a local Trinidad and Tobago date.
-- Value lists (segments, reasons) are checked in Go; the database checks
-- shape, ownership, uniqueness and the states that money depends on.

-- Rows that are financial history must not be rewritten. This trigger turns
-- an UPDATE or DELETE on such a table into an error; corrections are new
-- rows (a new price version, a refund, a credit).
-- +goose StatementBegin
CREATE FUNCTION forbid_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------- plans

CREATE TABLE subscription_plans (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug             text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 60),
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    summary          text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 200),
    description      text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    meals_per_week   smallint NOT NULL CHECK (meals_per_week BETWEEN 1 AND 14),
    billing_interval text NOT NULL DEFAULT 'week' CHECK (billing_interval IN ('week')),
    -- Who the plan is meant for, a key from internal/plans (descriptive only).
    segment          text NOT NULL CHECK (char_length(segment) BETWEEN 1 AND 30),
    benefits         text[] NOT NULL DEFAULT '{}',
    -- Only self-serve plans can be bought online. Others are shown with a
    -- "talk to us" note until their eligibility rules are approved.
    self_serve       boolean NOT NULL DEFAULT true,
    status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'retired')),
    available_from   timestamptz,
    available_until  timestamptz,
    sort_order       smallint NOT NULL DEFAULT 0,
    is_demo          boolean NOT NULL DEFAULT false,
    version          integer NOT NULL DEFAULT 1,
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_plans_slug_key UNIQUE (slug),
    CONSTRAINT subscription_plans_window CHECK (available_until IS NULL OR available_from IS NULL OR available_until > available_from)
);

-- A price is never edited: a new price is a new row with a later
-- effective_from. Subscriptions point at the row they were sold at.
CREATE TABLE subscription_plan_prices (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id        uuid NOT NULL REFERENCES subscription_plans (id) ON DELETE RESTRICT,
    amount_minor   bigint NOT NULL CHECK (amount_minor > 0 AND amount_minor < 100000000),
    currency       text NOT NULL DEFAULT 'TTD' CHECK (currency ~ '^[A-Z]{3}$'),
    effective_from timestamptz NOT NULL,
    note           text CHECK (note IS NULL OR char_length(note) <= 200),
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_plan_prices_effective_key UNIQUE (plan_id, effective_from)
);

CREATE TRIGGER subscription_plan_prices_immutable BEFORE UPDATE OR DELETE ON subscription_plan_prices
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- promotions

CREATE TABLE promotions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code             text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{2,29}$'),
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    kind             text NOT NULL CHECK (kind IN ('percent', 'fixed')),
    -- percent: basis points (1500 = 15%); fixed: minor units.
    percent_bps      integer CHECK (percent_bps IS NULL OR percent_bps BETWEEN 1 AND 10000),
    amount_minor     bigint CHECK (amount_minor IS NULL OR amount_minor > 0),
    currency         text NOT NULL DEFAULT 'TTD' CHECK (currency ~ '^[A-Z]{3}$'),
    -- How many billing cycles of one subscription it applies to (1 = first only).
    cycles           smallint NOT NULL DEFAULT 1 CHECK (cycles BETWEEN 1 AND 52),
    max_redemptions  integer CHECK (max_redemptions IS NULL OR max_redemptions > 0),
    redemptions      integer NOT NULL DEFAULT 0 CHECK (redemptions >= 0),
    valid_from       timestamptz,
    valid_until      timestamptz,
    -- Empty means every plan.
    plan_ids         uuid[] NOT NULL DEFAULT '{}',
    status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    activated_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    activated_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT promotions_code_key UNIQUE (code),
    CONSTRAINT promotions_value CHECK ((kind = 'percent' AND percent_bps IS NOT NULL AND amount_minor IS NULL)
                                    OR (kind = 'fixed' AND amount_minor IS NOT NULL AND percent_bps IS NULL)),
    CONSTRAINT promotions_cap CHECK (max_redemptions IS NULL OR redemptions <= max_redemptions),
    CONSTRAINT promotions_window CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until > valid_from)
);

-- ---------------------------------------------------------------- payment methods

-- A reference to a payment method held by the provider (a token). Never a
-- card number, expiry or security code.
CREATE TABLE payment_methods (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    provider     text NOT NULL CHECK (char_length(provider) BETWEEN 1 AND 30),
    provider_ref text NOT NULL CHECK (char_length(provider_ref) BETWEEN 1 AND 200),
    -- What the provider lets us show, e.g. "Visa ending 4242" or "Test card".
    label        text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 60),
    status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT payment_methods_provider_ref_key UNIQUE (provider, provider_ref)
);

CREATE INDEX payment_methods_customer_idx ON payment_methods (customer_id, created_at DESC);

-- ---------------------------------------------------------------- subscriptions

CREATE TABLE subscriptions (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id          uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    status               text NOT NULL DEFAULT 'pending_payment' CHECK (status IN
        ('pending_payment', 'active', 'pause_scheduled', 'paused', 'past_due', 'cancel_scheduled', 'cancelled', 'expired')),
    -- The plan and price future cycles are created with. Each cycle keeps
    -- its own copy, so changing these never rewrites a cycle already made.
    plan_id              uuid NOT NULL REFERENCES subscription_plans (id) ON DELETE RESTRICT,
    plan_price_id        uuid NOT NULL REFERENCES subscription_plan_prices (id) ON DELETE RESTRICT,
    meals_per_week       smallint NOT NULL CHECK (meals_per_week BETWEEN 1 AND 14),
    amount_minor         bigint NOT NULL CHECK (amount_minor > 0),
    currency             text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    -- Deleting an address never deletes history: orders keep a copy.
    delivery_address_id  uuid REFERENCES customer_addresses (id) ON DELETE SET NULL,
    payment_method_id    uuid REFERENCES payment_methods (id) ON DELETE SET NULL,
    first_week           date NOT NULL CHECK (extract(isodow FROM first_week) = 1),
    activated_at         timestamptz,
    pause_from_week      date CHECK (pause_from_week IS NULL OR extract(isodow FROM pause_from_week) = 1),
    -- First week after the pause (exclusive).
    pause_until_week     date CHECK (pause_until_week IS NULL OR extract(isodow FROM pause_until_week) = 1),
    cancel_requested_at  timestamptz,
    -- First week that will not be delivered.
    cancel_effective_week date CHECK (cancel_effective_week IS NULL OR extract(isodow FROM cancel_effective_week) = 1),
    cancel_reason        text CHECK (cancel_reason IS NULL OR char_length(cancel_reason) <= 40),
    cancelled_at         timestamptz,
    expired_at           timestamptz,
    version              integer NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscriptions_pause_range CHECK ((pause_from_week IS NULL) = (pause_until_week IS NULL)
                                             AND (pause_until_week IS NULL OR pause_until_week > pause_from_week))
);

-- One subscription in progress per customer (a pending one included).
CREATE UNIQUE INDEX subscriptions_one_live ON subscriptions (customer_id)
    WHERE status NOT IN ('cancelled', 'expired');
CREATE INDEX subscriptions_status_idx ON subscriptions (status, created_at DESC);
CREATE INDEX subscriptions_address_idx ON subscriptions (delivery_address_id);

-- Every status transition and customer request, newest last.
CREATE TABLE subscription_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    type            text NOT NULL CHECK (char_length(type) BETWEEN 1 AND 40),
    from_status     text,
    to_status       text,
    effective_week  date,
    actor_user_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscription_events_subscription_idx ON subscription_events (subscription_id, id);

CREATE TABLE subscription_changes (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id     uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    from_plan_price_id  uuid NOT NULL REFERENCES subscription_plan_prices (id) ON DELETE RESTRICT,
    to_plan_price_id    uuid NOT NULL REFERENCES subscription_plan_prices (id) ON DELETE RESTRICT,
    effective_week      date NOT NULL CHECK (extract(isodow FROM effective_week) = 1),
    requested_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscription_changes_subscription_idx ON subscription_changes (subscription_id, created_at);

CREATE TABLE subscription_pauses (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    from_week       date NOT NULL CHECK (extract(isodow FROM from_week) = 1),
    until_week      date NOT NULL CHECK (extract(isodow FROM until_week) = 1),
    -- scheduled/active: in force; ended: ran its course; cancelled: resumed early.
    status          text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'active', 'ended', 'cancelled')),
    requested_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_pauses_range CHECK (until_week > from_week)
);

CREATE INDEX subscription_pauses_subscription_idx ON subscription_pauses (subscription_id, created_at);

-- ---------------------------------------------------------------- weekly menus

CREATE TABLE weekly_menus (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The Monday of the delivery week, a local date.
    week_start         date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    time_zone          text NOT NULL DEFAULT 'America/Port_of_Spain',
    title              text CHECK (title IS NULL OR char_length(title) BETWEEN 1 AND 80),
    note               text CHECK (note IS NULL OR char_length(note) <= 500),
    -- draft: being prepared; scheduled: will publish at publish_at;
    -- published: customers see it; closed: cutoff passed; archived: hidden.
    status             text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'published', 'closed', 'archived')),
    cutoff_at          timestamptz NOT NULL,
    publish_at         timestamptz,
    published_at       timestamptz,
    -- Empty means every plan.
    eligible_plan_ids  uuid[] NOT NULL DEFAULT '{}',
    version            integer NOT NULL DEFAULT 1,
    created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT weekly_menus_week_key UNIQUE (week_start),
    CONSTRAINT weekly_menus_scheduled_has_time CHECK (status <> 'scheduled' OR publish_at IS NOT NULL),
    CONSTRAINT weekly_menus_published_has_time CHECK (status NOT IN ('published', 'closed') OR published_at IS NOT NULL)
);

CREATE INDEX weekly_menus_status_idx ON weekly_menus (status, week_start);

CREATE TABLE weekly_menu_meals (
    menu_id           uuid NOT NULL REFERENCES weekly_menus (id) ON DELETE CASCADE,
    meal_id           uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    position          smallint NOT NULL DEFAULT 0,
    -- Optional cap on how many of this meal one customer may choose.
    max_per_customer  smallint CHECK (max_per_customer IS NULL OR max_per_customer BETWEEN 1 AND 14),

    PRIMARY KEY (menu_id, meal_id)
);

CREATE INDEX weekly_menu_meals_meal_idx ON weekly_menu_meals (meal_id);

-- ---------------------------------------------------------------- cycles

-- One delivery week of one subscription. Created once per (subscription,
-- week): the unique key is what makes the scheduler safe to retry.
CREATE TABLE subscription_cycles (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id  uuid NOT NULL REFERENCES subscriptions (id) ON DELETE RESTRICT,
    customer_id      uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    week_start       date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    cutoff_at        timestamptz NOT NULL,
    -- When the week is billed (prepaid before the cutoff).
    renewal_at       timestamptz NOT NULL,
    -- Plan snapshot for this week.
    plan_id          uuid NOT NULL REFERENCES subscription_plans (id) ON DELETE RESTRICT,
    plan_price_id    uuid NOT NULL REFERENCES subscription_plan_prices (id) ON DELETE RESTRICT,
    meals_per_week   smallint NOT NULL CHECK (meals_per_week BETWEEN 1 AND 14),
    amount_minor     bigint NOT NULL CHECK (amount_minor > 0),
    currency         text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    -- scheduled: upcoming; finalized: an order was made; unpaid: not paid by
    -- the cutoff, no order; needs_resolution: paid but no confirmed selection
    -- or no menu at the cutoff (staff decide); paused, cancelled: not delivered.
    state            text NOT NULL DEFAULT 'scheduled' CHECK (state IN
        ('scheduled', 'finalized', 'unpaid', 'needs_resolution', 'paused', 'cancelled')),
    state_reason     text CHECK (state_reason IS NULL OR char_length(state_reason) <= 60),
    finalized_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_cycles_week_key UNIQUE (subscription_id, week_start),
    CONSTRAINT subscription_cycles_cutoff_before_week CHECK (renewal_at <= cutoff_at)
);

CREATE INDEX subscription_cycles_customer_idx ON subscription_cycles (customer_id, week_start DESC);
CREATE INDEX subscription_cycles_due_cutoff_idx ON subscription_cycles (cutoff_at) WHERE state = 'scheduled';
CREATE INDEX subscription_cycles_due_renewal_idx ON subscription_cycles (renewal_at) WHERE state = 'scheduled';
CREATE INDEX subscription_cycles_week_idx ON subscription_cycles (week_start, state);

-- ---------------------------------------------------------------- meal selections

CREATE TABLE meal_selections (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_cycle_id uuid NOT NULL REFERENCES subscription_cycles (id) ON DELETE RESTRICT,
    customer_id           uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    menu_id               uuid NOT NULL REFERENCES weekly_menus (id) ON DELETE RESTRICT,
    -- draft: saved, not confirmed; confirmed: the customer confirmed the
    -- full allowance; locked: the cutoff passed (no more changes).
    status                text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed', 'locked')),
    confirmed_at          timestamptz,
    locked_at             timestamptz,
    -- Whether it was confirmed when it was locked.
    was_confirmed         boolean,
    version               integer NOT NULL DEFAULT 1,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT meal_selections_cycle_key UNIQUE (subscription_cycle_id),
    CONSTRAINT meal_selections_confirmed_has_time CHECK (status <> 'confirmed' OR confirmed_at IS NOT NULL)
);

CREATE INDEX meal_selections_customer_idx ON meal_selections (customer_id, created_at DESC);

CREATE TABLE meal_selection_items (
    selection_id uuid NOT NULL REFERENCES meal_selections (id) ON DELETE CASCADE,
    meal_id      uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    quantity     smallint NOT NULL CHECK (quantity BETWEEN 1 AND 14),

    PRIMARY KEY (selection_id, meal_id)
);

-- ---------------------------------------------------------------- billing

CREATE TABLE billing_cycles (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id       uuid NOT NULL REFERENCES subscriptions (id) ON DELETE RESTRICT,
    subscription_cycle_id uuid NOT NULL REFERENCES subscription_cycles (id) ON DELETE RESTRICT,
    customer_id           uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    kind                  text NOT NULL CHECK (kind IN ('initial', 'renewal')),
    period_start          date NOT NULL,
    period_end            date NOT NULL,
    due_at                timestamptz NOT NULL,
    currency              text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    subtotal_minor        bigint NOT NULL CHECK (subtotal_minor >= 0),
    discount_minor        bigint NOT NULL DEFAULT 0 CHECK (discount_minor >= 0),
    delivery_fee_minor    bigint NOT NULL DEFAULT 0 CHECK (delivery_fee_minor >= 0),
    tax_minor             bigint NOT NULL DEFAULT 0 CHECK (tax_minor >= 0),
    total_minor           bigint NOT NULL CHECK (total_minor >= 0),
    tax_snapshot          jsonb NOT NULL,
    discount_snapshot     jsonb,
    promotion_id          uuid REFERENCES promotions (id) ON DELETE RESTRICT,
    -- pending: created, not yet payable (initial checkout not started);
    -- due: payable now; paid; failed: the last attempt failed (still payable
    -- until the cutoff); void: not collected (cutoff passed, abandoned).
    status                text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'due', 'paid', 'failed', 'void')),
    attempts              smallint NOT NULL DEFAULT 0,
    next_attempt_at       timestamptz,
    paid_at               timestamptz,
    voided_at             timestamptz,
    void_reason           text CHECK (void_reason IS NULL OR char_length(void_reason) <= 60),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT billing_cycles_cycle_key UNIQUE (subscription_cycle_id),
    -- With tax included in prices, tax_minor is the part of the total that
    -- is tax; otherwise it is added on top.
    CONSTRAINT billing_cycles_total CHECK (total_minor = subtotal_minor - discount_minor + delivery_fee_minor
        + CASE WHEN (tax_snapshot ->> 'mode') = 'inclusive' THEN 0 ELSE tax_minor END),
    CONSTRAINT billing_cycles_discount_cap CHECK (discount_minor <= subtotal_minor),
    CONSTRAINT billing_cycles_paid_has_time CHECK (status <> 'paid' OR paid_at IS NOT NULL)
);

CREATE INDEX billing_cycles_customer_idx ON billing_cycles (customer_id, period_start DESC);
CREATE INDEX billing_cycles_retry_idx ON billing_cycles (next_attempt_at) WHERE status = 'failed' AND next_attempt_at IS NOT NULL;

-- Gapless invoice numbers per calendar year: the counter row is locked and
-- incremented in the transaction that issues the invoice.
CREATE TABLE invoice_counters (
    year integer PRIMARY KEY,
    last bigint NOT NULL DEFAULT 0
);

CREATE TABLE invoices (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number            text NOT NULL,
    customer_id       uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    subscription_id   uuid NOT NULL REFERENCES subscriptions (id) ON DELETE RESTRICT,
    billing_cycle_id  uuid NOT NULL REFERENCES billing_cycles (id) ON DELETE RESTRICT,
    status            text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'paid', 'void')),
    currency          text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    subtotal_minor    bigint NOT NULL,
    discount_minor    bigint NOT NULL,
    delivery_fee_minor bigint NOT NULL,
    tax_minor         bigint NOT NULL,
    total_minor       bigint NOT NULL,
    period_start      date NOT NULL,
    period_end        date NOT NULL,
    issued_at         timestamptz NOT NULL,
    due_at            timestamptz NOT NULL,
    paid_at           timestamptz,
    voided_at         timestamptz,
    -- Copies taken at issue time, so the document never changes afterwards.
    customer_snapshot jsonb NOT NULL,
    business_snapshot jsonb NOT NULL,
    tax_snapshot      jsonb NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT invoices_number_key UNIQUE (number),
    CONSTRAINT invoices_billing_cycle_key UNIQUE (billing_cycle_id)
);

CREATE INDEX invoices_customer_idx ON invoices (customer_id, issued_at DESC);

CREATE TABLE invoice_items (
    invoice_id         uuid NOT NULL REFERENCES invoices (id) ON DELETE RESTRICT,
    position           smallint NOT NULL,
    kind               text NOT NULL CHECK (kind IN ('plan', 'discount', 'delivery', 'tax')),
    description        text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    quantity           integer NOT NULL DEFAULT 1,
    unit_amount_minor  bigint NOT NULL,
    amount_minor       bigint NOT NULL,

    PRIMARY KEY (invoice_id, position)
);

CREATE TRIGGER invoice_items_immutable BEFORE UPDATE OR DELETE ON invoice_items
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE promotion_redemptions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    promotion_id    uuid NOT NULL REFERENCES promotions (id) ON DELETE RESTRICT,
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE RESTRICT,
    customer_id     uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    cycles_total    smallint NOT NULL CHECK (cycles_total BETWEEN 1 AND 52),
    cycles_used     smallint NOT NULL DEFAULT 0 CHECK (cycles_used >= 0),
    -- reserved at subscription creation; redeemed when the first cycle is paid;
    -- released if that never happens.
    status          text NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved', 'redeemed', 'released')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT promotion_redemptions_subscription_key UNIQUE (subscription_id),
    CONSTRAINT promotion_redemptions_used CHECK (cycles_used <= cycles_total)
);

-- One use of a promotion per customer (released reservations do not count).
CREATE UNIQUE INDEX promotion_redemptions_once ON promotion_redemptions (promotion_id, customer_id) WHERE status <> 'released';

-- ---------------------------------------------------------------- payments

CREATE TABLE checkout_sessions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id      uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    billing_cycle_id uuid NOT NULL REFERENCES billing_cycles (id) ON DELETE RESTRICT,
    provider         text NOT NULL,
    provider_ref     text,
    redirect_url     text,
    amount_minor     bigint NOT NULL CHECK (amount_minor > 0),
    currency         text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'completed', 'failed', 'expired', 'cancelled')),
    expires_at       timestamptz NOT NULL,
    completed_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX checkout_sessions_provider_ref_key ON checkout_sessions (provider, provider_ref) WHERE provider_ref IS NOT NULL;
-- At most one open checkout per billing cycle.
CREATE UNIQUE INDEX checkout_sessions_one_open ON checkout_sessions (billing_cycle_id) WHERE status = 'open';
CREATE INDEX checkout_sessions_customer_idx ON checkout_sessions (customer_id, created_at DESC);

-- One attempt to collect a billing cycle through a provider.
CREATE TABLE payments (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id         uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    billing_cycle_id    uuid NOT NULL REFERENCES billing_cycles (id) ON DELETE RESTRICT,
    checkout_session_id uuid REFERENCES checkout_sessions (id) ON DELETE RESTRICT,
    payment_method_id   uuid REFERENCES payment_methods (id) ON DELETE SET NULL,
    provider            text NOT NULL,
    provider_ref        text,
    kind                text NOT NULL CHECK (kind IN ('checkout', 'off_session')),
    amount_minor        bigint NOT NULL CHECK (amount_minor > 0),
    currency            text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status              text NOT NULL DEFAULT 'pending' CHECK (status IN
        ('pending', 'requires_action', 'processing', 'succeeded', 'failed', 'cancelled', 'refunded', 'partially_refunded')),
    refunded_minor      bigint NOT NULL DEFAULT 0 CHECK (refunded_minor >= 0),
    failure_code        text CHECK (failure_code IS NULL OR char_length(failure_code) <= 60),
    failure_message     text CHECK (failure_message IS NULL OR char_length(failure_message) <= 300),
    -- Our key for the provider call; a retry of the same attempt reuses it.
    idempotency_key     text NOT NULL,
    -- Set when money arrived that the system could not apply (for example
    -- a payment that succeeded after its subscription expired). Finance
    -- reviews these; nothing is refunded automatically.
    needs_attention     boolean NOT NULL DEFAULT false,
    attention_reason    text CHECK (attention_reason IS NULL OR char_length(attention_reason) <= 200),
    succeeded_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT payments_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT payments_refund_cap CHECK (refunded_minor <= amount_minor)
);

CREATE UNIQUE INDEX payments_provider_ref_key ON payments (provider, provider_ref) WHERE provider_ref IS NOT NULL;
-- A billing cycle should be paid once. If a second payment succeeds anyway
-- (two methods racing), it is recorded and flagged for a refund rather than
-- refused: refusing it would lose track of money that was taken.
CREATE INDEX payments_customer_idx ON payments (customer_id, created_at DESC);
CREATE INDEX payments_billing_cycle_idx ON payments (billing_cycle_id);
CREATE INDEX payments_attention_idx ON payments (created_at) WHERE needs_attention;

CREATE TABLE payment_status_history (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_id        uuid NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    from_status       text,
    to_status         text NOT NULL,
    -- charge, webhook, reconcile, refund
    source            text NOT NULL CHECK (char_length(source) BETWEEN 1 AND 20),
    provider_event_id text,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX payment_status_history_payment_idx ON payment_status_history (payment_id, id);

-- Every authenticated webhook received, once per provider event id.
-- Requests with a bad signature are logged and refused, never stored.
CREATE TABLE payment_webhook_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider     text NOT NULL,
    event_id     text NOT NULL CHECK (char_length(event_id) BETWEEN 1 AND 200),
    event_type   text NOT NULL CHECK (char_length(event_type) BETWEEN 1 AND 100),
    -- What the event is about: a checkout session or a charge (transaction).
    ref_kind     text NOT NULL CHECK (ref_kind IN ('session', 'transaction', 'none')),
    provider_ref text,
    payload      jsonb NOT NULL,
    status       text NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'processed', 'ignored', 'failed')),
    error        text,
    attempts     integer NOT NULL DEFAULT 0,
    received_at  timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,

    CONSTRAINT payment_webhook_events_event_key UNIQUE (provider, event_id)
);

CREATE INDEX payment_webhook_events_status_idx ON payment_webhook_events (status, received_at DESC);

CREATE TABLE refunds (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id      uuid NOT NULL REFERENCES payments (id) ON DELETE RESTRICT,
    amount_minor    bigint NOT NULL CHECK (amount_minor > 0),
    currency        text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    reason          text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 300),
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    provider_ref    text,
    idempotency_key text NOT NULL,
    requested_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,

    CONSTRAINT refunds_idempotency_key UNIQUE (idempotency_key)
);

CREATE INDEX refunds_payment_idx ON refunds (payment_id, created_at);

-- ---------------------------------------------------------------- orders

CREATE TABLE order_counters (
    year integer PRIMARY KEY,
    last bigint NOT NULL DEFAULT 0
);

CREATE TABLE orders (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number                text NOT NULL,
    customer_id           uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    subscription_id       uuid NOT NULL REFERENCES subscriptions (id) ON DELETE RESTRICT,
    subscription_cycle_id uuid NOT NULL REFERENCES subscription_cycles (id) ON DELETE RESTRICT,
    billing_cycle_id      uuid NOT NULL REFERENCES billing_cycles (id) ON DELETE RESTRICT,
    -- The payment that paid the week; NULL only when a promotion made the
    -- week free, so nothing was collected.
    payment_id            uuid REFERENCES payments (id) ON DELETE RESTRICT,
    menu_id               uuid NOT NULL REFERENCES weekly_menus (id) ON DELETE RESTRICT,
    week_start            date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    -- Sprint 3 creates orders as confirmed. Production and delivery modules
    -- will own the later statuses.
    status                text NOT NULL DEFAULT 'confirmed' CHECK (status IN
        ('pending', 'confirmed', 'awaiting_production', 'in_production', 'ready_for_fulfillment', 'out_for_delivery', 'delivered', 'cancelled')),
    currency              text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    subtotal_minor        bigint NOT NULL,
    discount_minor        bigint NOT NULL,
    delivery_fee_minor    bigint NOT NULL,
    tax_minor             bigint NOT NULL,
    total_minor           bigint NOT NULL,
    meals_count           smallint NOT NULL CHECK (meals_count > 0),
    plan_name             text NOT NULL,
    -- Copies at the time of the order.
    address_snapshot      jsonb NOT NULL,
    -- Allergy keys the customer had declared (keys only), for production.
    customer_allergens    text[] NOT NULL DEFAULT '{}',
    delivery_window       jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT orders_number_key UNIQUE (number),
    -- The scheduler can run twice; a cycle still makes one order.
    CONSTRAINT orders_cycle_key UNIQUE (subscription_cycle_id)
);

CREATE INDEX orders_customer_idx ON orders (customer_id, week_start DESC);
CREATE INDEX orders_week_idx ON orders (week_start, status);

CREATE TABLE order_items (
    order_id        uuid NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    meal_id         uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    -- meals.version when the order was made.
    meal_version    integer NOT NULL,
    meal_name       text NOT NULL,
    meal_slug       text NOT NULL,
    quantity        smallint NOT NULL CHECK (quantity > 0),
    servings        smallint NOT NULL,
    allergens       text[] NOT NULL DEFAULT '{}',
    allergen_status text NOT NULL,
    dietary_tags    text[] NOT NULL DEFAULT '{}',

    PRIMARY KEY (order_id, meal_id)
);

CREATE TRIGGER order_items_immutable BEFORE UPDATE OR DELETE ON order_items
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE order_status_history (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id      uuid NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    from_status   text,
    to_status     text NOT NULL,
    reason        text,
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX order_status_history_order_idx ON order_status_history (order_id, id);

-- ---------------------------------------------------------------- notifications

-- One row per email that must go out at most once (payment receipts,
-- reminders). The job is queued only when the row is new, so a retried
-- event or a second scheduler run never sends a second copy.
CREATE TABLE notification_deliveries (
    dedupe_key  text PRIMARY KEY CHECK (char_length(dedupe_key) BETWEEN 1 AND 200),
    kind        text NOT NULL,
    customer_id uuid REFERENCES customers (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- test provider

-- The simulated payment provider's own records. They stand in for the
-- systems of a real provider, and are written only when
-- PAYMENTS_TEST_PROVIDER=true, which the API refuses in production.
CREATE TABLE testpay_sessions (
    ref            text PRIMARY KEY,
    amount_minor   bigint NOT NULL,
    currency       text NOT NULL,
    description    text NOT NULL,
    return_url     text NOT NULL,
    reference      text NOT NULL,
    status         text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'approved', 'declined', 'cancelled')),
    transaction_ref text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    completed_at   timestamptz
);

CREATE TABLE testpay_transactions (
    ref           text PRIMARY KEY,
    kind          text NOT NULL CHECK (kind IN ('checkout', 'charge', 'refund')),
    session_ref   text REFERENCES testpay_sessions (ref),
    parent_ref    text,
    -- The saved test card: 'approve' or 'decline' on later charges.
    card_token    text,
    amount_minor  bigint NOT NULL,
    currency      text NOT NULL,
    status        text NOT NULL CHECK (status IN ('succeeded', 'failed')),
    idempotency_key text NOT NULL UNIQUE,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- permissions

INSERT INTO permissions (key, description) VALUES
    ('plans.manage',       'Create plans and price versions; activate and retire plans.'),
    ('menus.manage',       'Create and edit weekly menus.'),
    ('menus.publish',      'Publish and unpublish weekly menus.'),
    ('subscriptions.read', 'View customer subscriptions, cycles and their history.'),
    ('orders.read',        'View orders and the weekly demand summary.'),
    ('payments.read',      'View billing, invoices, payments and payment webhooks.'),
    ('payments.refund',    'Issue refunds through the payment provider.'),
    ('promotions.manage',  'Create, activate and archive promotion codes.');

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('super_admin', 'plans.manage'), ('super_admin', 'menus.manage'), ('super_admin', 'menus.publish'),
    ('super_admin', 'subscriptions.read'), ('super_admin', 'orders.read'), ('super_admin', 'payments.read'),
    ('super_admin', 'payments.refund'), ('super_admin', 'promotions.manage'),
    ('admin', 'plans.manage'), ('admin', 'menus.manage'), ('admin', 'menus.publish'),
    ('admin', 'subscriptions.read'), ('admin', 'orders.read'), ('admin', 'payments.read'),
    ('admin', 'payments.refund'), ('admin', 'promotions.manage'),
    -- Finance gets the console now that there is billing to look after.
    ('finance', 'admin.access'), ('finance', 'plans.manage'), ('finance', 'subscriptions.read'),
    ('finance', 'orders.read'), ('finance', 'payments.read'), ('finance', 'payments.refund'),
    ('finance', 'promotions.manage'),
    ('operations', 'menus.manage'), ('operations', 'menus.publish'), ('operations', 'subscriptions.read'),
    ('operations', 'orders.read'),
    ('support', 'subscriptions.read'), ('support', 'orders.read'), ('support', 'payments.read'),
    -- Marketing and chefs help build menus but do not publish them.
    ('marketing', 'menus.manage');

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN
    ('plans.manage', 'menus.manage', 'menus.publish', 'subscriptions.read', 'orders.read', 'payments.read', 'payments.refund', 'promotions.manage');
DELETE FROM role_permissions WHERE role_key = 'finance' AND permission_key = 'admin.access';
DELETE FROM permissions WHERE key IN
    ('plans.manage', 'menus.manage', 'menus.publish', 'subscriptions.read', 'orders.read', 'payments.read', 'payments.refund', 'promotions.manage');

DROP TABLE testpay_transactions;
DROP TABLE testpay_sessions;
DROP TABLE notification_deliveries;
DROP TABLE order_status_history;
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE order_counters;
DROP TABLE refunds;
DROP TABLE payment_webhook_events;
DROP TABLE payment_status_history;
DROP TABLE payments;
DROP TABLE checkout_sessions;
DROP TABLE promotion_redemptions;
DROP TABLE invoice_items;
DROP TABLE invoices;
DROP TABLE invoice_counters;
DROP TABLE billing_cycles;
DROP TABLE meal_selection_items;
DROP TABLE meal_selections;
DROP TABLE subscription_cycles;
DROP TABLE weekly_menu_meals;
DROP TABLE weekly_menus;
DROP TABLE subscription_pauses;
DROP TABLE subscription_changes;
DROP TABLE subscription_events;
DROP TABLE subscriptions;
DROP TABLE payment_methods;
DROP TABLE promotions;
DROP TABLE subscription_plan_prices;
DROP TABLE subscription_plans;
DROP FUNCTION forbid_change();
