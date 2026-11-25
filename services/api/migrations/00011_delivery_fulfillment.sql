-- +goose Up

-- Sprint 7: fulfillment (internal/fulfillment) and delivery (internal/delivery).
-- Additive only. Commerce keeps orders, production keeps batches, holds and
-- packaging, inventory keeps stock; nothing here alters their tables.
-- Business inputs (zones, days, windows, capacity, partners, proof policy,
-- fees, rates) ship empty: they are configuration, never assumptions.

CREATE TABLE delivery_counters (
    kind   text NOT NULL,
    period text NOT NULL,
    last   integer NOT NULL DEFAULT 0,
    PRIMARY KEY (kind, period)
);

-- ---------------------------------------------------------------- fulfillment

-- The commitment to fulfil one paid order. One per order (and an order is
-- one per subscription cycle), so a replayed readiness event cannot make a
-- second one.
CREATE TABLE fulfillment_obligations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number           text NOT NULL,
    order_id         uuid NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    order_number     text NOT NULL,
    customer_id      uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    week_start       date NOT NULL,
    plan_id          uuid REFERENCES production_plans (id) ON DELETE RESTRICT,
    status           text NOT NULL CHECK (status IN ('ready_for_verification', 'verification_in_progress', 'verified',
        'ready_for_dispatch', 'handed_over', 'fulfilled', 'exception', 'cancelled')),
    -- The state an exception interrupted; resolving returns to it.
    resume_status    text CHECK (resume_status IS NULL OR resume_status IN ('ready_for_verification', 'verification_in_progress',
        'verified', 'ready_for_dispatch', 'handed_over')),
    version          integer NOT NULL DEFAULT 1,
    kits_required    integer NOT NULL CHECK (kits_required > 0),
    source_event_id  bigint,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    verified_at      timestamptz,
    released_at      timestamptz,
    handed_over_at   timestamptz,
    fulfilled_at     timestamptz,
    cancelled_at     timestamptz,

    CONSTRAINT fulfillment_obligations_order_key UNIQUE (order_id),
    CONSTRAINT fulfillment_obligations_number_key UNIQUE (number),
    CONSTRAINT fulfillment_obligations_resume CHECK (resume_status IS NULL OR status = 'exception')
);

CREATE INDEX fulfillment_obligations_status_idx ON fulfillment_obligations (status, week_start);
CREATE INDEX fulfillment_obligations_customer_idx ON fulfillment_obligations (customer_id, week_start DESC);

-- A physical package (bag or box) for an order. One package per order is
-- the default; the identity is stable and printed on the label.
CREATE TABLE fulfillment_packages (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code                text NOT NULL,
    obligation_id       uuid NOT NULL REFERENCES fulfillment_obligations (id) ON DELETE RESTRICT,
    sequence            smallint NOT NULL CHECK (sequence > 0),
    verification_status text NOT NULL DEFAULT 'pending' CHECK (verification_status IN ('pending', 'verified', 'discrepancy')),
    verified_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    verified_at         timestamptz,
    label_printed_at    timestamptz,
    handoff_status      text NOT NULL DEFAULT 'pending' CHECK (handoff_status IN ('pending', 'handed_over', 'returned', 'missing')),
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT fulfillment_packages_code_key UNIQUE (code),
    CONSTRAINT fulfillment_packages_seq_key UNIQUE (obligation_id, sequence)
);

-- What a package holds: kits of one meal, made by one production batch.
-- Traceability is batch-level: the batch, not the kit, links to ingredient lots.
CREATE TABLE fulfillment_package_items (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    package_id        uuid NOT NULL REFERENCES fulfillment_packages (id) ON DELETE RESTRICT,
    obligation_id     uuid NOT NULL REFERENCES fulfillment_obligations (id) ON DELETE RESTRICT,
    meal_id           uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    meal_name         text NOT NULL,
    quantity          smallint NOT NULL CHECK (quantity > 0),
    batch_id          uuid NOT NULL REFERENCES production_batches (id) ON DELETE RESTRICT,
    batch_code        text NOT NULL,
    recipe_version_id uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE RESTRICT,
    status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified', 'discrepancy')),
    verified_quantity smallint CHECK (verified_quantity IS NULL OR verified_quantity >= 0),

    CONSTRAINT fulfillment_package_items_meal_key UNIQUE (obligation_id, meal_id)
);

CREATE INDEX fulfillment_package_items_batch_idx ON fulfillment_package_items (batch_id);

-- Each check a person made at the packing table. Append-only.
CREATE TABLE fulfillment_verifications (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    obligation_id      uuid NOT NULL REFERENCES fulfillment_obligations (id) ON DELETE RESTRICT,
    item_id            uuid NOT NULL REFERENCES fulfillment_package_items (id) ON DELETE RESTRICT,
    counted_quantity   smallint NOT NULL CHECK (counted_quantity >= 0),
    scanned_batch_code text NOT NULL,
    recipe_confirmed   boolean NOT NULL,
    packaging_ok       boolean NOT NULL,
    label_ok           boolean NOT NULL,
    result             text NOT NULL CHECK (result IN ('verified', 'discrepancy')),
    discrepancy        text CHECK (discrepancy IS NULL OR char_length(discrepancy) <= 1000),
    actor_user_id      uuid REFERENCES users (id) ON DELETE SET NULL,
    idempotency_key    text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 100),
    created_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT fulfillment_verifications_idem UNIQUE (item_id, idempotency_key)
);

CREATE TRIGGER fulfillment_verifications_append_only BEFORE UPDATE OR DELETE ON fulfillment_verifications
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE fulfillment_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    obligation_id uuid NOT NULL REFERENCES fulfillment_obligations (id) ON DELETE RESTRICT,
    from_status   text,
    to_status     text NOT NULL,
    reason        text CHECK (reason IS NULL OR char_length(reason) <= 1000),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX fulfillment_events_obligation_idx ON fulfillment_events (obligation_id, id);
CREATE TRIGGER fulfillment_events_append_only BEFORE UPDATE OR DELETE ON fulfillment_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- delivery settings

-- Business policies. Everything starts unset: until the business approves a
-- proof policy, no delivery can be completed.
CREATE TABLE delivery_settings (
    id                           boolean PRIMARY KEY DEFAULT true CHECK (id),
    -- Methods that count as proof. Empty: policy not configured.
    proof_methods                text[] NOT NULL DEFAULT '{}' CHECK (proof_methods <@ ARRAY['confirmation_code', 'recipient_acknowledgment',
        'staff_confirmation', 'partner_confirmation']::text[]),
    proof_policy_ref             text NOT NULL DEFAULT '' CHECK (char_length(proof_policy_ref) <= 200),
    -- Hours before the service date after which a customer may no longer ask
    -- to reschedule. NULL: no policy, requests are collected, nothing promised.
    reschedule_cutoff_hours      integer CHECK (reschedule_cutoff_hours IS NULL OR reschedule_cutoff_hours BETWEEN 0 AND 336),
    failure_reasons              text[] NOT NULL DEFAULT ARRAY['recipient_unavailable', 'address_not_found', 'refused',
        'damaged', 'partner_unable', 'operational_delay', 'other'],
    -- Assignment to a partner that is not approved, by explicit exception.
    allow_unapproved_partners    boolean NOT NULL DEFAULT false,
    unapproved_partner_policy_ref text NOT NULL DEFAULT '' CHECK (char_length(unapproved_partner_policy_ref) <= 200),
    -- Package labels are not certified for live use until this is true.
    label_certified              boolean NOT NULL DEFAULT false,
    -- Customer delivery emails need approved wording.
    customer_emails_approved     boolean NOT NULL DEFAULT false,
    updated_by                   uuid,
    updated_at                   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_settings_partner_exception CHECK (NOT allow_unapproved_partners OR unapproved_partner_policy_ref <> '')
);
INSERT INTO delivery_settings DEFAULT VALUES;

-- ---------------------------------------------------------------- zones and calendar

-- Operational zones used for dispatch. Customer-facing serviceability stays
-- in delivery_zones (per region); a zone covers regions through explicit rules.
CREATE TABLE delivery_service_zones (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code           text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{1,19}$'),
    name           text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'inactive')),
    fee_policy_ref text NOT NULL DEFAULT '' CHECK (char_length(fee_policy_ref) <= 200),
    notes          text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_service_zones_code_key UNIQUE (code)
);

-- Coverage rule: this region belongs to this zone. A region is in at most one zone.
CREATE TABLE delivery_zone_regions (
    zone_id uuid NOT NULL REFERENCES delivery_service_zones (id) ON DELETE CASCADE,
    country text NOT NULL,
    region  text NOT NULL,

    PRIMARY KEY (country, region),
    FOREIGN KEY (country, region) REFERENCES delivery_zones (country, region) ON DELETE CASCADE
);

-- A date Mise delivers (zone NULL: every zone), or a closure.
CREATE TABLE delivery_service_days (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_date date NOT NULL,
    zone_id      uuid REFERENCES delivery_service_zones (id) ON DELETE CASCADE,
    status       text NOT NULL CHECK (status IN ('open', 'closed')),
    cutoff_at    timestamptz,
    notes        text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 500),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_service_days_key UNIQUE NULLS NOT DISTINCT (service_date, zone_id)
);

-- A delivery window on a service day. Customers see only approved windows.
CREATE TABLE delivery_windows (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_day_id uuid NOT NULL REFERENCES delivery_service_days (id) ON DELETE CASCADE,
    label          text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 60),
    starts_at      time NOT NULL,
    ends_at        time NOT NULL,
    status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'approved')),
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_windows_order CHECK (ends_at > starts_at),
    CONSTRAINT delivery_windows_key UNIQUE (service_day_id, starts_at, ends_at)
);

-- ---------------------------------------------------------------- partners and couriers

CREATE TABLE delivery_partners (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    contact_name      text NOT NULL DEFAULT '' CHECK (char_length(contact_name) <= 120),
    phone             text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 40),
    email             text NOT NULL DEFAULT '' CHECK (char_length(email) <= 200),
    escalation_contact text NOT NULL DEFAULT '' CHECK (char_length(escalation_contact) <= 300),
    integration_mode  text NOT NULL DEFAULT 'manual' CHECK (integration_mode IN ('manual', 'file', 'api', 'internal_fleet')),
    agreement_status  text NOT NULL DEFAULT 'none' CHECK (agreement_status IN ('none', 'negotiating', 'signed', 'expired', 'terminated')),
    agreement_ref     text NOT NULL DEFAULT '' CHECK (char_length(agreement_ref) <= 200),
    status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_approval', 'approved', 'suspended', 'retired')),
    capabilities      text[] NOT NULL DEFAULT '{}',
    supported_windows text NOT NULL DEFAULT '' CHECK (char_length(supported_windows) <= 300),
    capacity_note     text NOT NULL DEFAULT '' CHECK (char_length(capacity_note) <= 300),
    billing_reference text NOT NULL DEFAULT '' CHECK (char_length(billing_reference) <= 120),
    notes             text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    -- Local demo data only (misectl delivery seed-demo); never a real partner.
    demo              boolean NOT NULL DEFAULT false,
    version           integer NOT NULL DEFAULT 1,
    approved_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at       timestamptz,
    created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    -- An approved partner has a signed agreement.
    CONSTRAINT delivery_partners_approved CHECK (status <> 'approved' OR agreement_status = 'signed')
);

CREATE TABLE delivery_partner_zones (
    partner_id uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE CASCADE,
    zone_id    uuid NOT NULL REFERENCES delivery_service_zones (id) ON DELETE CASCADE,
    PRIMARY KEY (partner_id, zone_id)
);

-- Qualification evidence per criterion. Approval needs every required criterion met.
CREATE TABLE delivery_partner_criteria (
    partner_id  uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE CASCADE,
    criterion   text NOT NULL CHECK (criterion IN ('contract', 'service_coverage', 'handling_capability', 'reliability',
        'escalation_process', 'delivery_evidence', 'handling_procedures')),
    met         boolean NOT NULL,
    evidence    text NOT NULL DEFAULT '' CHECK (char_length(evidence) <= 1000),
    recorded_by uuid REFERENCES users (id) ON DELETE SET NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (partner_id, criterion)
);

CREATE TABLE delivery_partner_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    partner_id    uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE CASCADE,
    from_status   text,
    to_status     text NOT NULL,
    reason        text CHECK (reason IS NULL OR char_length(reason) <= 1000),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER delivery_partner_events_append_only BEFORE UPDATE ON delivery_partner_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- A courier works for a partner (internal couriers belong to an
-- internal_fleet partner). A courier with a login (user_id) sees only the
-- runs assigned to them.
CREATE TABLE delivery_couriers (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE RESTRICT,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    phone      text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 40),
    user_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    zone_ids   uuid[] NOT NULL DEFAULT '{}',
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    notes      text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    demo       boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_couriers_user_key UNIQUE (user_id)
);

-- Capacity in deliveries for a date, optionally narrowed to a zone, window,
-- partner or courier. No row: capacity is unknown, never unlimited.
CREATE TABLE delivery_capacity (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_date date NOT NULL,
    zone_id      uuid REFERENCES delivery_service_zones (id) ON DELETE CASCADE,
    window_id    uuid REFERENCES delivery_windows (id) ON DELETE CASCADE,
    partner_id   uuid REFERENCES delivery_partners (id) ON DELETE CASCADE,
    courier_id   uuid REFERENCES delivery_couriers (id) ON DELETE CASCADE,
    capacity     integer NOT NULL CHECK (capacity >= 0),
    notes        text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 300),
    updated_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_capacity_scope_key UNIQUE NULLS NOT DISTINCT (service_date, zone_id, window_id, partner_id, courier_id)
);

-- ---------------------------------------------------------------- deliveries

-- The single authority on where a delivery stands.
CREATE TABLE deliveries (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number                 text NOT NULL,
    obligation_id          uuid NOT NULL REFERENCES fulfillment_obligations (id) ON DELETE RESTRICT,
    order_id               uuid NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    customer_id            uuid NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    status                 text NOT NULL DEFAULT 'pending_assignment' CHECK (status IN ('pending_assignment', 'assigned',
        'ready_for_pickup', 'picked_up', 'in_transit', 'arriving', 'delivered', 'delivery_failed', 'returned', 'cancelled')),
    version                integer NOT NULL DEFAULT 1,
    attempt                smallint NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 20),
    zone_id                uuid REFERENCES delivery_service_zones (id) ON DELETE RESTRICT,
    zone_source            text NOT NULL DEFAULT 'none' CHECK (zone_source IN ('none', 'coverage_rule', 'manual')),
    service_date           date,
    window_id              uuid REFERENCES delivery_windows (id) ON DELETE RESTRICT,
    schedule_confirmed_at  timestamptz,
    -- Immutable snapshot of the order's address at release (corrections are
    -- explicit, permissioned and logged as tracking events).
    address                jsonb NOT NULL,
    address_validation     text NOT NULL CHECK (address_validation IN ('serviceable', 'unzoned', 'not_serviceable', 'unknown')),
    package_count          smallint NOT NULL CHECK (package_count > 0),
    -- The confirmation code is not stored: it is an HMAC of (delivery id,
    -- attempt) under the server key, so it only ever matches this delivery.
    delivered_at           timestamptz,
    failed_at              timestamptz,
    cancelled_at           timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT deliveries_obligation_key UNIQUE (obligation_id),
    CONSTRAINT deliveries_number_key UNIQUE (number),
    CONSTRAINT deliveries_zone_source CHECK ((zone_id IS NULL) = (zone_source = 'none')),
    CONSTRAINT deliveries_confirmed CHECK (schedule_confirmed_at IS NULL OR service_date IS NOT NULL)
);

CREATE INDEX deliveries_status_idx ON deliveries (status, service_date);
CREATE INDEX deliveries_customer_idx ON deliveries (customer_id, created_at DESC);
CREATE INDEX deliveries_order_idx ON deliveries (order_id);

-- +goose StatementBegin
CREATE FUNCTION delivery_address_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.address IS DISTINCT FROM OLD.address AND OLD.status NOT IN ('pending_assignment', 'assigned') THEN
        RAISE EXCEPTION 'delivery address cannot change once dispatched' USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER deliveries_address_guard BEFORE UPDATE ON deliveries
    FOR EACH ROW EXECUTE FUNCTION delivery_address_guard();

-- ---------------------------------------------------------------- dispatch

CREATE TABLE dispatch_runs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number       text NOT NULL,
    service_date date NOT NULL,
    partner_id   uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE RESTRICT,
    courier_id   uuid REFERENCES delivery_couriers (id) ON DELETE RESTRICT,
    zone_id      uuid REFERENCES delivery_service_zones (id) ON DELETE RESTRICT,
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'finalized', 'released', 'completed', 'reconciled', 'cancelled')),
    version      integer NOT NULL DEFAULT 1,
    notes        text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    finalized_at timestamptz,
    released_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    released_at  timestamptz,
    completed_at timestamptz,
    cancelled_at timestamptz,

    CONSTRAINT dispatch_runs_number_key UNIQUE (number)
);

CREATE INDEX dispatch_runs_date_idx ON dispatch_runs (service_date, status);

-- A stop is the assignment of a delivery to a run. One active stop per delivery.
CREATE TABLE dispatch_stops (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            uuid NOT NULL REFERENCES dispatch_runs (id) ON DELETE RESTRICT,
    delivery_id       uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    attempt           smallint NOT NULL,
    sequence          smallint NOT NULL CHECK (sequence > 0),
    status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed', 'done')),
    capacity_override text CHECK (capacity_override IS NULL OR char_length(capacity_override) BETWEEN 3 AND 500),
    partner_override  text CHECK (partner_override IS NULL OR char_length(partner_override) BETWEEN 3 AND 500),
    added_by          uuid REFERENCES users (id) ON DELETE SET NULL,
    added_at          timestamptz NOT NULL DEFAULT now(),
    removed_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    removed_at        timestamptz,
    removal_reason    text CHECK (removal_reason IS NULL OR char_length(removal_reason) <= 500)
);

CREATE UNIQUE INDEX dispatch_stops_active_delivery ON dispatch_stops (delivery_id) WHERE status = 'active';
CREATE INDEX dispatch_stops_run_idx ON dispatch_stops (run_id, status, sequence);

-- A manifest is generated when a run is finalized: a frozen copy of its
-- stops and packages. A later change supersedes it; history is kept.
CREATE TABLE dispatch_manifests (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        uuid NOT NULL REFERENCES dispatch_runs (id) ON DELETE RESTRICT,
    revision      integer NOT NULL CHECK (revision > 0),
    status        text NOT NULL DEFAULT 'current' CHECK (status IN ('current', 'superseded')),
    content       jsonb NOT NULL,
    content_hash  text NOT NULL,
    stop_count    integer NOT NULL,
    package_count integer NOT NULL,
    generated_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    generated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT dispatch_manifests_revision_key UNIQUE (run_id, revision)
);

CREATE UNIQUE INDEX dispatch_manifests_current ON dispatch_manifests (run_id) WHERE status = 'current';

-- ---------------------------------------------------------------- custody

-- A recorded handoff of packages from one party to another. Never inferred
-- from a manifest.
CREATE TABLE delivery_handoffs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            uuid NOT NULL REFERENCES dispatch_runs (id) ON DELETE RESTRICT,
    from_party        text NOT NULL CHECK (char_length(from_party) BETWEEN 1 AND 120),
    to_party          text NOT NULL CHECK (char_length(to_party) BETWEEN 1 AND 120),
    partner_id        uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE RESTRICT,
    courier_id        uuid REFERENCES delivery_couriers (id) ON DELETE RESTRICT,
    handed_at         timestamptz NOT NULL,
    location          text CHECK (location IS NULL OR char_length(location) <= 200),
    accepted_count    integer NOT NULL CHECK (accepted_count >= 0),
    rejected_count    integer NOT NULL CHECK (rejected_count >= 0),
    missing_count     integer NOT NULL CHECK (missing_count >= 0),
    disputed          boolean NOT NULL,
    evidence_ref      text NOT NULL DEFAULT '' CHECK (char_length(evidence_ref) <= 200),
    notes             text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    actor_user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
    idempotency_key   text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 100),
    created_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_handoffs_idem UNIQUE (run_id, idempotency_key)
);

CREATE TRIGGER delivery_handoffs_append_only BEFORE UPDATE OR DELETE ON delivery_handoffs
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE delivery_handoff_packages (
    handoff_id  uuid NOT NULL REFERENCES delivery_handoffs (id) ON DELETE RESTRICT,
    package_id  uuid NOT NULL REFERENCES fulfillment_packages (id) ON DELETE RESTRICT,
    delivery_id uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    attempt     smallint NOT NULL,
    outcome     text NOT NULL CHECK (outcome IN ('accepted', 'rejected', 'missing')),
    PRIMARY KEY (handoff_id, package_id)
);

-- A package is accepted into custody once per delivery attempt.
CREATE UNIQUE INDEX delivery_handoff_packages_once ON delivery_handoff_packages (package_id, attempt) WHERE outcome = 'accepted';
CREATE TRIGGER delivery_handoff_packages_append_only BEFORE UPDATE OR DELETE ON delivery_handoff_packages
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- tracking and proof

-- Append-only history. Location only when verified; source says who reported it.
CREATE TABLE delivery_tracking_events (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id       uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    type              text NOT NULL CHECK (type ~ '^[a-z_]+$'),
    from_status       text,
    to_status         text,
    attempt           smallint NOT NULL,
    source            text NOT NULL CHECK (source IN ('staff', 'partner', 'customer', 'system')),
    actor_user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
    partner_id        uuid REFERENCES delivery_partners (id) ON DELETE SET NULL,
    occurred_at       timestamptz NOT NULL,
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    location          text CHECK (location IS NULL OR char_length(location) <= 200),
    location_verified boolean NOT NULL DEFAULT false,
    notes             text CHECK (notes IS NULL OR char_length(notes) <= 1000),
    details           jsonb NOT NULL DEFAULT '{}',
    idempotency_key   text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 120),

    CONSTRAINT delivery_tracking_idem UNIQUE (delivery_id, idempotency_key),
    CONSTRAINT delivery_tracking_location CHECK (location IS NULL OR location_verified)
);

CREATE INDEX delivery_tracking_delivery_idx ON delivery_tracking_events (delivery_id, id);
CREATE TRIGGER delivery_tracking_events_append_only BEFORE UPDATE OR DELETE ON delivery_tracking_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE delivery_proofs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id       uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    attempt           smallint NOT NULL,
    method            text NOT NULL CHECK (method IN ('confirmation_code', 'recipient_acknowledgment', 'staff_confirmation',
        'partner_confirmation', 'photo')),
    status            text NOT NULL CHECK (status IN ('accepted', 'rejected')),
    recipient_name    text CHECK (recipient_name IS NULL OR char_length(recipient_name) <= 120),
    partner_reference text CHECK (partner_reference IS NULL OR char_length(partner_reference) <= 120),
    notes             text CHECK (notes IS NULL OR char_length(notes) <= 1000),
    rejection_reason  text CHECK (rejection_reason IS NULL OR char_length(rejection_reason) <= 300),
    recorded_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    idempotency_key   text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 100),
    created_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_proofs_idem UNIQUE (delivery_id, idempotency_key)
);

CREATE UNIQUE INDEX delivery_proofs_accepted_once ON delivery_proofs (delivery_id, attempt) WHERE status = 'accepted';
CREATE TRIGGER delivery_proofs_append_only BEFORE UPDATE OR DELETE ON delivery_proofs
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- exceptions

-- One queue for fulfillment and delivery exceptions.
CREATE TABLE delivery_exceptions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number        text NOT NULL,
    stage         text NOT NULL CHECK (stage IN ('fulfillment', 'delivery')),
    type          text NOT NULL CHECK (type IN ('eligibility', 'missing_package', 'incorrect_package', 'kit_discrepancy',
        'batch_problem', 'address_problem', 'capacity_shortage', 'partner_cancellation', 'late_handoff', 'handoff_dispute',
        'delivery_failure', 'missing_proof', 'damaged_package', 'customer_complaint', 'other')),
    severity      text NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'resolved', 'closed')),
    -- A blocking exception stops dispatch of what it names.
    blocking      boolean NOT NULL DEFAULT false,
    owner_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    obligation_id uuid REFERENCES fulfillment_obligations (id) ON DELETE RESTRICT,
    delivery_id   uuid REFERENCES deliveries (id) ON DELETE RESTRICT,
    order_id      uuid REFERENCES orders (id) ON DELETE RESTRICT,
    customer_id   uuid REFERENCES customers (id) ON DELETE RESTRICT,
    batch_id      uuid REFERENCES production_batches (id) ON DELETE RESTRICT,
    run_id        uuid REFERENCES dispatch_runs (id) ON DELETE RESTRICT,
    summary       text NOT NULL CHECK (char_length(summary) BETWEEN 3 AND 300),
    notes         text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    next_action   text NOT NULL DEFAULT 'none' CHECK (next_action IN ('none', 'reattempt', 'reschedule', 'return', 'escalate', 'refund_review')),
    resolution    text CHECK (resolution IS NULL OR char_length(resolution) <= 2000),
    -- One open exception per cause (a retried consumer or double click).
    dedupe_key    text,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    resolved_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    resolved_at   timestamptz,

    CONSTRAINT delivery_exceptions_number_key UNIQUE (number),
    CONSTRAINT delivery_exceptions_dedupe_key UNIQUE (dedupe_key),
    CONSTRAINT delivery_exceptions_resolution CHECK (status NOT IN ('resolved', 'closed') OR resolution IS NOT NULL)
);

CREATE INDEX delivery_exceptions_status_idx ON delivery_exceptions (status, severity, created_at DESC);
CREATE INDEX delivery_exceptions_delivery_idx ON delivery_exceptions (delivery_id) WHERE delivery_id IS NOT NULL;
CREATE INDEX delivery_exceptions_obligation_idx ON delivery_exceptions (obligation_id) WHERE obligation_id IS NOT NULL;

CREATE TABLE delivery_exception_events (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    exception_id  uuid NOT NULL REFERENCES delivery_exceptions (id) ON DELETE CASCADE,
    type          text NOT NULL CHECK (type IN ('created', 'note', 'status', 'owner', 'next_action', 'resolution')),
    body          text CHECK (body IS NULL OR char_length(body) <= 2000),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER delivery_exception_events_append_only BEFORE UPDATE ON delivery_exception_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- Support cases about a delivery (support keeps the case; this is the link).
CREATE TABLE delivery_case_links (
    support_case_id uuid NOT NULL REFERENCES support_cases (id) ON DELETE CASCADE,
    delivery_id     uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (support_case_id, delivery_id)
);

-- ---------------------------------------------------------------- rescheduling and returns

CREATE TABLE delivery_reschedule_requests (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id     uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    requested_by    text NOT NULL CHECK (requested_by IN ('customer', 'staff')),
    requester_user  uuid REFERENCES users (id) ON DELETE SET NULL,
    preferred_date  date,
    reason          text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 500),
    status          text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested', 'approved', 'declined', 'withdrawn')),
    new_date        date,
    new_window_id   uuid REFERENCES delivery_windows (id) ON DELETE RESTRICT,
    decision_note   text CHECK (decision_note IS NULL OR char_length(decision_note) <= 500),
    decided_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    decided_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_reschedule_approved CHECK (status <> 'approved' OR new_date IS NOT NULL)
);

CREATE UNIQUE INDEX delivery_reschedule_open ON delivery_reschedule_requests (delivery_id) WHERE status = 'requested';

-- Returned packages are quarantined until reviewed; they never go back to stock.
CREATE TABLE delivery_returns (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id       uuid NOT NULL REFERENCES deliveries (id) ON DELETE RESTRICT,
    attempt           smallint NOT NULL,
    reason            text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 500),
    returned_at       timestamptz NOT NULL,
    received_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    condition         text NOT NULL CHECK (condition IN ('intact', 'damaged', 'temperature_concern', 'unknown')),
    disposition       text NOT NULL DEFAULT 'quarantined' CHECK (disposition IN ('quarantined', 'discarded', 'other_approved')),
    disposition_note  text CHECK (disposition_note IS NULL OR char_length(disposition_note) <= 1000),
    status            text NOT NULL DEFAULT 'awaiting_review' CHECK (status IN ('awaiting_review', 'reviewed')),
    reviewed_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_returns_attempt_key UNIQUE (delivery_id, attempt),
    CONSTRAINT delivery_returns_review CHECK ((status = 'reviewed') = (reviewed_at IS NOT NULL)),
    CONSTRAINT delivery_returns_other CHECK (disposition <> 'other_approved' OR disposition_note IS NOT NULL)
);

CREATE TABLE delivery_return_packages (
    return_id  uuid NOT NULL REFERENCES delivery_returns (id) ON DELETE RESTRICT,
    package_id uuid NOT NULL REFERENCES fulfillment_packages (id) ON DELETE RESTRICT,
    PRIMARY KEY (return_id, package_id)
);

-- ---------------------------------------------------------------- costs and reconciliation

CREATE TABLE delivery_cost_records (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id     uuid NOT NULL REFERENCES delivery_partners (id) ON DELETE RESTRICT,
    delivery_id    uuid REFERENCES deliveries (id) ON DELETE RESTRICT,
    run_id         uuid REFERENCES dispatch_runs (id) ON DELETE RESTRICT,
    kind           text NOT NULL CHECK (kind IN ('estimated', 'contracted', 'actual', 'adjustment')),
    amount_minor   bigint NOT NULL,
    currency       text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    rate_reference text NOT NULL DEFAULT '' CHECK (char_length(rate_reference) <= 200),
    status         text NOT NULL DEFAULT 'recorded' CHECK (status IN ('recorded', 'disputed', 'approved')),
    notes          text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    recorded_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_cost_target CHECK (delivery_id IS NOT NULL OR run_id IS NOT NULL),
    CONSTRAINT delivery_cost_sign CHECK (kind = 'adjustment' OR amount_minor >= 0)
);

CREATE TABLE delivery_reconciliations (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        uuid NOT NULL REFERENCES dispatch_runs (id) ON DELETE RESTRICT,
    status        text NOT NULL CHECK (status IN ('open', 'reconciled')),
    summary       jsonb NOT NULL,
    discrepancies jsonb NOT NULL,
    explanation   text CHECK (explanation IS NULL OR char_length(explanation) <= 2000),
    closed_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    closed_at     timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_reconciliations_run_key UNIQUE (run_id),
    CONSTRAINT delivery_reconciliations_closed CHECK ((status = 'reconciled') = (closed_at IS NOT NULL))
);

-- ---------------------------------------------------------------- customer messages

-- Every customer delivery message: queued, sent, failed or skipped (and why).
-- A message is "sent" only when the mail provider accepted it.
CREATE TABLE delivery_notifications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id uuid NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
    customer_id uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('scheduled', 'dispatched', 'delayed', 'failed', 'delivered', 'rescheduled')),
    channel     text NOT NULL DEFAULT 'email' CHECK (channel IN ('email')),
    dedupe_key  text NOT NULL,
    status      text NOT NULL CHECK (status IN ('queued', 'sent', 'failed', 'skipped')),
    reason      text CHECK (reason IS NULL OR char_length(reason) <= 300),
    attempts    integer NOT NULL DEFAULT 0,
    last_error  text CHECK (last_error IS NULL OR char_length(last_error) <= 500),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_notifications_dedupe_key UNIQUE (dedupe_key)
);

CREATE INDEX delivery_notifications_delivery_idx ON delivery_notifications (delivery_id, created_at);

-- ---------------------------------------------------------------- event consumer

INSERT INTO event_consumer_offsets (consumer) VALUES ('fulfillment') ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------- roles and permissions

INSERT INTO roles (key, name, description) VALUES
    ('fulfillment_manager',  'Fulfillment manager',  'Runs order verification and releases verified orders to dispatch.'),
    ('fulfillment_staff',    'Fulfillment staff',    'Verifies that the right kits are in each order''s packages.'),
    ('dispatch_manager',     'Dispatch manager',     'Plans dispatch, manages partners and zones, reconciles runs.'),
    ('dispatcher',           'Dispatcher',           'Assigns deliveries to runs, prints manifests and records handoffs.'),
    ('delivery_coordinator', 'Delivery coordinator', 'Follows deliveries, records status and proof, handles exceptions.'),
    ('courier',              'Courier',              'Sees only their own assigned runs; records status and proof.');

INSERT INTO permissions (key, description) VALUES
    ('fulfillment.read',            'View fulfillment obligations, packages and verifications.'),
    ('fulfillment.verify',          'Verify kits against orders and record discrepancies.'),
    ('fulfillment.release',         'Release verified orders to dispatch.'),
    ('delivery.read',               'View all deliveries, dispatch runs, manifests and tracking.'),
    ('delivery.read_assigned',      'View only the deliveries on runs assigned to you as a courier.'),
    ('delivery.plan',               'Schedule deliveries, assign zones and correct addresses before dispatch.'),
    ('delivery.assign',             'Assign deliveries to dispatch runs.'),
    ('delivery.dispatch',           'Finalize and release dispatch runs, record handoffs, override capacity.'),
    ('delivery.update_status',      'Record delivery status updates.'),
    ('delivery.reschedule',         'Decide reschedule requests.'),
    ('delivery.confirm',            'Complete deliveries and record failures.'),
    ('delivery.reconcile',          'Reconcile and close dispatch runs.'),
    ('delivery_partners.read',      'View delivery partners and couriers.'),
    ('delivery_partners.manage',    'Create and edit delivery partners, couriers and cost records.'),
    ('delivery_partners.approve',   'Approve, suspend and retire delivery partners.'),
    ('delivery_zones.read',         'View delivery zones, the service calendar and capacity.'),
    ('delivery_zones.manage',       'Configure delivery zones, service days, windows and capacity.'),
    ('delivery_proof.read',         'View proof of delivery.'),
    ('delivery_proof.create',       'Record proof of delivery.'),
    ('delivery_exceptions.read',    'View fulfillment and delivery exceptions and returns.'),
    ('delivery_exceptions.manage',  'Raise, work and resolve exceptions; record and review returns.'),
    ('delivery_reports.read',       'View delivery reports and the operations overview.'),
    ('delivery_settings.manage',    'Set delivery policies and make audited status corrections.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM unnest(ARRAY['super_admin', 'admin']) AS r,
     unnest(ARRAY['fulfillment.read', 'fulfillment.verify', 'fulfillment.release', 'delivery.read', 'delivery.plan',
                  'delivery.assign', 'delivery.dispatch', 'delivery.update_status', 'delivery.reschedule', 'delivery.confirm',
                  'delivery.reconcile', 'delivery_partners.read', 'delivery_partners.manage', 'delivery_partners.approve',
                  'delivery_zones.read', 'delivery_zones.manage', 'delivery_proof.read', 'delivery_proof.create',
                  'delivery_exceptions.read', 'delivery_exceptions.manage', 'delivery_reports.read', 'delivery_settings.manage']) AS p;

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('fulfillment_manager', ARRAY['admin.access', 'fulfillment.read', 'fulfillment.verify', 'fulfillment.release', 'delivery.read',
        'delivery_zones.read', 'delivery_exceptions.read', 'delivery_exceptions.manage', 'delivery_reports.read', 'production.read', 'quality.read']),
    ('fulfillment_staff', ARRAY['admin.access', 'fulfillment.read', 'fulfillment.verify', 'delivery_exceptions.read']),
    ('dispatch_manager', ARRAY['admin.access', 'fulfillment.read', 'delivery.read', 'delivery.plan', 'delivery.assign', 'delivery.dispatch',
        'delivery.update_status', 'delivery.reschedule', 'delivery.confirm', 'delivery.reconcile', 'delivery_partners.read',
        'delivery_partners.manage', 'delivery_partners.approve', 'delivery_zones.read', 'delivery_zones.manage', 'delivery_proof.read',
        'delivery_proof.create', 'delivery_exceptions.read', 'delivery_exceptions.manage', 'delivery_reports.read']),
    ('dispatcher', ARRAY['admin.access', 'fulfillment.read', 'delivery.read', 'delivery.plan', 'delivery.assign', 'delivery.dispatch',
        'delivery.update_status', 'delivery_partners.read', 'delivery_zones.read', 'delivery_proof.read', 'delivery_exceptions.read',
        'delivery_exceptions.manage']),
    ('delivery_coordinator', ARRAY['admin.access', 'delivery.read', 'delivery.update_status', 'delivery.reschedule', 'delivery.confirm',
        'delivery_partners.read', 'delivery_zones.read', 'delivery_proof.read', 'delivery_proof.create', 'delivery_exceptions.read',
        'delivery_exceptions.manage', 'delivery_reports.read']),
    ('courier', ARRAY['admin.access', 'delivery.read_assigned', 'delivery.update_status', 'delivery.confirm', 'delivery_proof.create']),
    ('support', ARRAY['delivery.read', 'delivery_proof.read', 'delivery_exceptions.read']),
    ('operations', ARRAY['fulfillment.read', 'delivery.read', 'delivery_zones.read', 'delivery_partners.read', 'delivery_proof.read',
        'delivery_exceptions.read', 'delivery_reports.read'])
) AS g(r, perms), unnest(perms) AS p
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('fulfillment.read', 'fulfillment.verify', 'fulfillment.release', 'delivery.read',
    'delivery.read_assigned', 'delivery.plan', 'delivery.assign', 'delivery.dispatch', 'delivery.update_status', 'delivery.reschedule',
    'delivery.confirm', 'delivery.reconcile', 'delivery_partners.read', 'delivery_partners.manage', 'delivery_partners.approve',
    'delivery_zones.read', 'delivery_zones.manage', 'delivery_proof.read', 'delivery_proof.create', 'delivery_exceptions.read',
    'delivery_exceptions.manage', 'delivery_reports.read', 'delivery_settings.manage');
DELETE FROM role_permissions WHERE role_key IN ('fulfillment_manager', 'fulfillment_staff', 'dispatch_manager', 'dispatcher',
    'delivery_coordinator', 'courier');
-- Rolling back removes the roles, so pending invitations to them go too (Sprint 9 fix:
-- an outstanding invitation used to block the rollback).
DELETE FROM staff_invitations WHERE role_key IN ('fulfillment_manager', 'fulfillment_staff', 'dispatch_manager', 'dispatcher',
    'delivery_coordinator', 'courier');
DELETE FROM user_roles WHERE role_key IN ('fulfillment_manager', 'fulfillment_staff', 'dispatch_manager', 'dispatcher',
    'delivery_coordinator', 'courier');
DELETE FROM permissions WHERE key IN ('fulfillment.read', 'fulfillment.verify', 'fulfillment.release', 'delivery.read',
    'delivery.read_assigned', 'delivery.plan', 'delivery.assign', 'delivery.dispatch', 'delivery.update_status', 'delivery.reschedule',
    'delivery.confirm', 'delivery.reconcile', 'delivery_partners.read', 'delivery_partners.manage', 'delivery_partners.approve',
    'delivery_zones.read', 'delivery_zones.manage', 'delivery_proof.read', 'delivery_proof.create', 'delivery_exceptions.read',
    'delivery_exceptions.manage', 'delivery_reports.read', 'delivery_settings.manage');
DELETE FROM roles WHERE key IN ('fulfillment_manager', 'fulfillment_staff', 'dispatch_manager', 'dispatcher',
    'delivery_coordinator', 'courier');
DELETE FROM event_consumer_offsets WHERE consumer = 'fulfillment';

DROP TABLE delivery_notifications;
DROP TABLE delivery_reconciliations;
DROP TABLE delivery_cost_records;
DROP TABLE delivery_return_packages;
DROP TABLE delivery_returns;
DROP TABLE delivery_reschedule_requests;
DROP TABLE delivery_case_links;
DROP TABLE delivery_exception_events;
DROP TABLE delivery_exceptions;
DROP TABLE delivery_proofs;
DROP TABLE delivery_tracking_events;
DROP TABLE delivery_handoff_packages;
DROP TABLE delivery_handoffs;
DROP TABLE dispatch_manifests;
DROP TABLE dispatch_stops;
DROP TABLE dispatch_runs;
DROP TRIGGER deliveries_address_guard ON deliveries;
DROP FUNCTION delivery_address_guard();
DROP TABLE deliveries;
DROP TABLE delivery_capacity;
DROP TABLE delivery_couriers;
DROP TABLE delivery_partner_events;
DROP TABLE delivery_partner_criteria;
DROP TABLE delivery_partner_zones;
DROP TABLE delivery_partners;
DROP TABLE delivery_windows;
DROP TABLE delivery_service_days;
DROP TABLE delivery_zone_regions;
DROP TABLE delivery_service_zones;
DROP TABLE delivery_settings;
DROP TABLE fulfillment_events;
DROP TABLE fulfillment_verifications;
DROP TABLE fulfillment_package_items;
DROP TABLE fulfillment_packages;
DROP TABLE fulfillment_obligations;
DROP TABLE delivery_counters;
