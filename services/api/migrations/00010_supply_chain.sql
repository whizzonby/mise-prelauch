-- +goose Up

-- Sprint 6: suppliers and farmers, procurement and purchasing, and the
-- ingredient inventory ledger with lot traceability to production batches.
--
-- The ingredient catalogue stays in culinary (ingredients); production plans,
-- requirements and batches stay in production. Nothing here seeds suppliers,
-- prices, certifications, locations, procedures, tolerances or stock: those
-- must come from the business. See docs/architecture/SPRINT_6_PLAN.md.

-- Numbers people read (SUP-0001, PO-2611-0001, LOT-261113-001, ...).
CREATE TABLE supply_counters (
    kind   text NOT NULL,
    period text NOT NULL,
    last   integer NOT NULL DEFAULT 0,
    PRIMARY KEY (kind, period)
);

-- ---------------------------------------------------------------- suppliers

CREATE TABLE suppliers (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code                  text NOT NULL UNIQUE,
    name                  text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    trading_name          text NOT NULL DEFAULT '' CHECK (char_length(trading_name) <= 120),
    type                  text NOT NULL CHECK (type IN ('farmer', 'cooperative', 'producer', 'distributor', 'packaging', 'other')),
    status                text NOT NULL DEFAULT 'draft'
                          CHECK (status IN ('draft', 'pending_review', 'approved', 'suspended', 'rejected', 'inactive')),
    contact_name          text NOT NULL DEFAULT '' CHECK (char_length(contact_name) <= 120),
    phone                 text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 40),
    email                 text NOT NULL DEFAULT '' CHECK (char_length(email) <= 254),
    preferred_channel     text NOT NULL DEFAULT 'phone' CHECK (preferred_channel IN ('phone', 'whatsapp', 'email', 'in_person', 'other')),
    region                text NOT NULL DEFAULT '' CHECK (char_length(region) <= 60),
    location              text NOT NULL DEFAULT '' CHECK (char_length(location) <= 300),
    operating_regions     text[] NOT NULL DEFAULT '{}',
    -- As agreed with the supplier; empty means not agreed.
    payment_terms         text NOT NULL DEFAULT '' CHECK (char_length(payment_terms) <= 300),
    delivery_arrangements text NOT NULL DEFAULT '' CHECK (char_length(delivery_arrangements) <= 500),
    notes                 text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    -- When set, the approval lapses on this date and the supplier needs a new review.
    review_expires_on     date,
    submitted_by          uuid REFERENCES users (id) ON DELETE SET NULL,
    created_by            uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX suppliers_name_key ON suppliers (lower(btrim(name)));
CREATE INDEX suppliers_status_idx ON suppliers (status, type);

-- Approval history: every status change, by whom and why.
CREATE TABLE supplier_status_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    supplier_id uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    from_status text,
    to_status   text NOT NULL,
    note        text,
    actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX supplier_status_events_idx ON supplier_status_events (supplier_id, id);
CREATE TRIGGER supplier_status_events_append_only BEFORE UPDATE OR DELETE ON supplier_status_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- What a review checks, from the business's approved procedure. None shipped.
CREATE TABLE supplier_qualification_criteria (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description   text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    -- Empty means every supplier type.
    supplier_types text[] NOT NULL DEFAULT '{}',
    required      boolean NOT NULL DEFAULT true,
    procedure_ref text NOT NULL DEFAULT '' CHECK (char_length(procedure_ref) <= 120),
    position      smallint NOT NULL DEFAULT 0,
    active        boolean NOT NULL DEFAULT true,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- One review: each criterion's result and the decision. Append-only.
CREATE TABLE supplier_reviews (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    supplier_id uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    decision    text NOT NULL CHECK (decision IN ('approved', 'rejected', 'changes_requested')),
    -- [{criterion_id, name, met, note}]
    results     jsonb NOT NULL DEFAULT '[]',
    notes       text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    expires_on  date,
    reviewer_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX supplier_reviews_idx ON supplier_reviews (supplier_id, created_at);
CREATE TRIGGER supplier_reviews_append_only BEFORE UPDATE OR DELETE ON supplier_reviews
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- Farm details for agricultural suppliers, recorded by staff on the farmer's behalf.
CREATE TABLE farmer_profiles (
    supplier_id             uuid PRIMARY KEY REFERENCES suppliers (id) ON DELETE CASCADE,
    farm_name               text NOT NULL DEFAULT '' CHECK (char_length(farm_name) <= 120),
    farm_location           text NOT NULL DEFAULT '' CHECK (char_length(farm_location) <= 300),
    crops                   text[] NOT NULL DEFAULT '{}',
    growing_seasons         text NOT NULL DEFAULT '' CHECK (char_length(growing_seasons) <= 500),
    harvest_windows         text NOT NULL DEFAULT '' CHECK (char_length(harvest_windows) <= 500),
    estimated_capacity      text NOT NULL DEFAULT '' CHECK (char_length(estimated_capacity) <= 500),
    collection_arrangements text NOT NULL DEFAULT '' CHECK (char_length(collection_arrangements) <= 500),
    -- Whose words these are: "told to us by phone on ...".
    information_source      text NOT NULL DEFAULT '' CHECK (char_length(information_source) <= 300),
    recorded_by             uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at              timestamptz NOT NULL DEFAULT now()
);

-- Document metadata only: there is no file storage. A certification counts
-- only when someone verified it.
CREATE TABLE supplier_documents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    supplier_id uuid NOT NULL REFERENCES suppliers (id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('certification', 'licence', 'inspection_report', 'agreement', 'other')),
    title       text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    reference   text NOT NULL DEFAULT '' CHECK (char_length(reference) <= 300),
    issued_by   text NOT NULL DEFAULT '' CHECK (char_length(issued_by) <= 200),
    expires_on  date,
    verified_by uuid REFERENCES users (id) ON DELETE SET NULL,
    verified_at timestamptz,
    added_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT supplier_documents_verified CHECK ((verified_by IS NULL) = (verified_at IS NULL))
);
CREATE INDEX supplier_documents_idx ON supplier_documents (supplier_id);

-- What staff discussed with a supplier, through which channel. Recorded by
-- people: the system sends nothing.
CREATE TABLE supplier_communications (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    supplier_id   uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    channel       text NOT NULL CHECK (channel IN ('phone', 'whatsapp', 'email', 'in_person', 'document', 'other')),
    direction     text NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    summary       text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 2000),
    resource_type text,
    resource_id   uuid,
    recorded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX supplier_communications_idx ON supplier_communications (supplier_id, id DESC);
CREATE TRIGGER supplier_communications_append_only BEFORE UPDATE OR DELETE ON supplier_communications
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- offers

-- A supplier's product, mapped to one catalogue ingredient (or a packaging
-- item). Sold in packs: pack_quantity of pack_unit per pack.
CREATE TABLE supplier_products (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    supplier_id          uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    kind                 text NOT NULL DEFAULT 'ingredient' CHECK (kind IN ('ingredient', 'packaging')),
    ingredient_id        uuid REFERENCES ingredients (id) ON DELETE RESTRICT,
    sku                  text NOT NULL DEFAULT '' CHECK (char_length(sku) <= 60),
    name                 text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    description          text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    grade                text NOT NULL DEFAULT '' CHECK (char_length(grade) <= 120),
    specification        text NOT NULL DEFAULT '' CHECK (char_length(specification) <= 1000),
    pack_quantity        numeric(14, 3) NOT NULL CHECK (pack_quantity > 0),
    pack_unit            text NOT NULL REFERENCES units_of_measure (code),
    pack_description     text NOT NULL DEFAULT '' CHECK (char_length(pack_description) <= 120),
    min_order_packs      numeric(14, 3) NOT NULL DEFAULT 1 CHECK (min_order_packs > 0),
    lead_time_days       smallint CHECK (lead_time_days IS NULL OR lead_time_days BETWEEN 0 AND 365),
    availability         text NOT NULL DEFAULT 'unknown' CHECK (availability IN ('unknown', 'available', 'limited', 'seasonal', 'unavailable')),
    delivery_constraints text NOT NULL DEFAULT '' CHECK (char_length(delivery_constraints) <= 500),
    -- The ingredient mapping is reviewed: proposed until someone confirms it.
    mapping_status       text NOT NULL DEFAULT 'proposed' CHECK (mapping_status IN ('proposed', 'confirmed', 'rejected')),
    mapping_note         text NOT NULL DEFAULT '' CHECK (char_length(mapping_note) <= 500),
    mapping_reviewed_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    mapping_reviewed_at  timestamptz,
    active               boolean NOT NULL DEFAULT true,
    created_by           uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT supplier_products_kind CHECK ((kind = 'ingredient') = (ingredient_id IS NOT NULL))
);
CREATE INDEX supplier_products_ingredient_idx ON supplier_products (ingredient_id) WHERE active;
CREATE INDEX supplier_products_supplier_idx ON supplier_products (supplier_id);
CREATE UNIQUE INDEX supplier_products_sku_key ON supplier_products (supplier_id, lower(sku)) WHERE sku <> '';

-- Price history: never overwritten. The price in force is the latest valid one.
CREATE TABLE supplier_prices (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id      uuid NOT NULL REFERENCES supplier_products (id) ON DELETE RESTRICT,
    amount_minor    bigint NOT NULL CHECK (amount_minor >= 0),
    currency        text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    valid_from      date NOT NULL,
    valid_until     date,
    origin          text NOT NULL CHECK (origin IN ('price_list', 'quotation', 'verbal', 'other')),
    quotation_id    uuid,
    note            text NOT NULL DEFAULT '' CHECK (char_length(note) <= 300),
    recorded_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT supplier_prices_dates CHECK (valid_until IS NULL OR valid_until >= valid_from)
);
CREATE INDEX supplier_prices_idx ON supplier_prices (product_id, valid_from DESC, created_at DESC);
CREATE TRIGGER supplier_prices_append_only BEFORE UPDATE OR DELETE ON supplier_prices
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- What a supplier says they could supply. Never stock, never a promise.
CREATE TABLE supplier_availability (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    supplier_id        uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    ingredient_id      uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    status             text NOT NULL CHECK (status IN ('available_now', 'expected_harvest', 'limited', 'unavailable', 'seasonal')),
    estimated_quantity numeric(14, 3) CHECK (estimated_quantity IS NULL OR estimated_quantity > 0),
    unit               text REFERENCES units_of_measure (code),
    available_from     date,
    available_until    date,
    confidence         text NOT NULL CHECK (confidence IN ('confirmed_by_supplier', 'supplier_estimate', 'staff_estimate')),
    source             text NOT NULL DEFAULT '' CHECK (char_length(source) <= 300),
    confirmed_at       timestamptz NOT NULL DEFAULT now(),
    recorded_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT supplier_availability_qty CHECK ((estimated_quantity IS NULL) = (unit IS NULL)),
    CONSTRAINT supplier_availability_dates CHECK (available_until IS NULL OR available_from IS NULL OR available_until >= available_from)
);
CREATE INDEX supplier_availability_idx ON supplier_availability (ingredient_id, confirmed_at DESC);

-- ---------------------------------------------------------------- procurement settings

-- One row. Every value starts unset: the business decides them.
CREATE TABLE procurement_settings (
    id                          boolean PRIMARY KEY DEFAULT true CHECK (id),
    -- Above this PO total a second approval (approve_above_limit) is needed. NULL: no limit configured.
    approval_limit_minor        bigint CHECK (approval_limit_minor IS NULL OR approval_limit_minor >= 0),
    -- Over-receiving allowed above the ordered quantity, in percent. NULL: none.
    receiving_tolerance_percent numeric(5, 2) CHECK (receiving_tolerance_percent IS NULL OR receiving_tolerance_percent BETWEEN 0 AND 100),
    -- Currencies POs may use. TTD is the business currency (Sprint 3).
    currencies                  text[] NOT NULL DEFAULT '{TTD}',
    -- Off: unapproved suppliers get no purchase orders.
    allow_unapproved_suppliers  boolean NOT NULL DEFAULT false,
    unapproved_policy_ref       text NOT NULL DEFAULT '' CHECK (char_length(unapproved_policy_ref) <= 200),
    updated_by                  uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT procurement_settings_exception CHECK (NOT allow_unapproved_suppliers OR char_length(unapproved_policy_ref) > 0)
);
INSERT INTO procurement_settings DEFAULT VALUES;

-- ---------------------------------------------------------------- procurement runs

-- The requirement snapshot of one released production plan.
CREATE TABLE procurement_runs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number            text NOT NULL UNIQUE,
    plan_id           uuid NOT NULL REFERENCES production_plans (id) ON DELETE RESTRICT,
    plan_number       text NOT NULL,
    snapshot_id       uuid NOT NULL REFERENCES production_demand_snapshots (id) ON DELETE RESTRICT,
    snapshot_revision integer NOT NULL,
    week_start        date NOT NULL,
    -- The plan's production date: ingredients are needed before it.
    required_by       date NOT NULL,
    -- The event that created it (NULL when a planner created it by hand).
    source_event_id   bigint,
    status            text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'approved', 'closed', 'cancelled')),
    -- Set when a newer demand revision exists for the plan's week.
    review_reason     text,
    approved_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at       timestamptz,
    created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
-- One live run per plan, however often the event is delivered.
CREATE UNIQUE INDEX procurement_runs_plan_key ON procurement_runs (plan_id) WHERE status <> 'cancelled';

-- Gross requirements as consumed, per ingredient and preparation. Never edited.
CREATE TABLE procurement_requirements (
    run_id           uuid NOT NULL REFERENCES procurement_runs (id) ON DELETE CASCADE,
    requirement_id   uuid NOT NULL,
    ingredient_id    uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    preparation      text NOT NULL DEFAULT '',
    quantity         numeric(16, 3),
    unit             text NOT NULL REFERENCES units_of_measure (code),
    status           text NOT NULL,
    approximate      boolean NOT NULL DEFAULT false,
    batch_ids        uuid[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (run_id, requirement_id)
);

-- One line per ingredient: the net calculation and the purchasing decision.
CREATE TABLE procurement_lines (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id             uuid NOT NULL REFERENCES procurement_runs (id) ON DELETE CASCADE,
    ingredient_id      uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    unit               text NOT NULL REFERENCES units_of_measure (code),
    -- NULL when a requirement could not be converted: the line is incomplete.
    gross              numeric(16, 3),
    buffer             numeric(16, 3) NOT NULL DEFAULT 0,
    -- Decision: the supplier product chosen and how many packs, with a reason.
    decision           text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending', 'buy', 'no_purchase', 'deferred')),
    product_id         uuid REFERENCES supplier_products (id) ON DELETE RESTRICT,
    packs              numeric(14, 3) CHECK (packs IS NULL OR packs > 0),
    decision_note      text NOT NULL DEFAULT '' CHECK (char_length(decision_note) <= 1000),
    decided_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    decided_at         timestamptz,
    CONSTRAINT procurement_lines_key UNIQUE (run_id, ingredient_id),
    CONSTRAINT procurement_lines_buy CHECK ((decision = 'buy') = (product_id IS NOT NULL AND packs IS NOT NULL))
);

-- ---------------------------------------------------------------- RFQs and quotations

CREATE TABLE rfqs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number      text NOT NULL UNIQUE,
    status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'closed', 'cancelled')),
    run_id      uuid REFERENCES procurement_runs (id) ON DELETE SET NULL,
    required_by date,
    deadline    timestamptz,
    notes       text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    buyer_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE rfq_lines (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rfq_id        uuid NOT NULL REFERENCES rfqs (id) ON DELETE CASCADE,
    ingredient_id uuid REFERENCES ingredients (id) ON DELETE RESTRICT,
    description   text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    quantity      numeric(14, 3) NOT NULL CHECK (quantity > 0),
    unit          text NOT NULL REFERENCES units_of_measure (code),
    position      smallint NOT NULL DEFAULT 0
);

-- Who was asked, and how staff asked them. Nothing is sent by the system.
CREATE TABLE rfq_suppliers (
    rfq_id      uuid NOT NULL REFERENCES rfqs (id) ON DELETE CASCADE,
    supplier_id uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    channel     text CHECK (channel IN ('phone', 'whatsapp', 'email', 'in_person', 'document', 'other')),
    invited_at  timestamptz,
    invited_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    PRIMARY KEY (rfq_id, supplier_id)
);

-- A quotation as received, recorded by staff. Never edited: a new one replaces it.
CREATE TABLE quotations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rfq_id          uuid REFERENCES rfqs (id) ON DELETE RESTRICT,
    supplier_id     uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    reference       text NOT NULL DEFAULT '' CHECK (char_length(reference) <= 120),
    currency        text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    lead_time_days  smallint CHECK (lead_time_days IS NULL OR lead_time_days BETWEEN 0 AND 365),
    delivery_terms  text NOT NULL DEFAULT '' CHECK (char_length(delivery_terms) <= 500),
    valid_until     date,
    received_via    text NOT NULL CHECK (received_via IN ('phone', 'whatsapp', 'email', 'in_person', 'document', 'other')),
    received_at     timestamptz NOT NULL DEFAULT now(),
    recorded_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER quotations_append_only BEFORE UPDATE OR DELETE ON quotations
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE quotation_lines (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    quotation_id       uuid NOT NULL REFERENCES quotations (id) ON DELETE RESTRICT,
    product_id         uuid NOT NULL REFERENCES supplier_products (id) ON DELETE RESTRICT,
    packs              numeric(14, 3) NOT NULL CHECK (packs > 0),
    unit_price_minor   bigint NOT NULL CHECK (unit_price_minor >= 0),
    position           smallint NOT NULL DEFAULT 0
);
CREATE TRIGGER quotation_lines_append_only BEFORE UPDATE OR DELETE ON quotation_lines
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

ALTER TABLE supplier_prices ADD CONSTRAINT supplier_prices_quotation_fk FOREIGN KEY (quotation_id) REFERENCES quotations (id) ON DELETE RESTRICT;

-- ---------------------------------------------------------------- purchase orders

CREATE TABLE storage_locations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code             text NOT NULL UNIQUE CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{0,19}$'),
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    type             text NOT NULL CHECK (type IN ('receiving', 'dry', 'chilled', 'frozen', 'preparation', 'quarantine', 'other')),
    capacity_note    text NOT NULL DEFAULT '' CHECK (char_length(capacity_note) <= 300),
    -- Only as approved in a procedure.
    temperature_note text NOT NULL DEFAULT '' CHECK (char_length(temperature_note) <= 300),
    active           boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE purchase_orders (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number                text NOT NULL UNIQUE,
    supplier_id           uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    run_id                uuid REFERENCES procurement_runs (id) ON DELETE SET NULL,
    status                text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending_approval', 'approved', 'issued',
                              'partially_received', 'received', 'closed', 'cancelled')),
    revision              integer NOT NULL DEFAULT 1,
    currency              text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    expected_delivery     date,
    location_id           uuid REFERENCES storage_locations (id) ON DELETE RESTRICT,
    delivery_fee_minor    bigint NOT NULL DEFAULT 0 CHECK (delivery_fee_minor >= 0),
    tax_minor             bigint NOT NULL DEFAULT 0 CHECK (tax_minor >= 0),
    notes                 text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    -- The documented reason when the exception policy lets an unapproved supplier be ordered from.
    supplier_exception    text,
    buyer_id              uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_by           uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at           timestamptz,
    limit_approved_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    issued_by             uuid REFERENCES users (id) ON DELETE SET NULL,
    issued_at             timestamptz,
    issued_via            text CHECK (issued_via IN ('phone', 'whatsapp', 'email', 'in_person', 'document', 'other')),
    cancel_reason         text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX purchase_orders_status_idx ON purchase_orders (status, expected_delivery);
CREATE INDEX purchase_orders_supplier_idx ON purchase_orders (supplier_id, created_at DESC);

CREATE TABLE purchase_order_lines (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    po_id              uuid NOT NULL REFERENCES purchase_orders (id) ON DELETE CASCADE,
    position           smallint NOT NULL,
    product_id         uuid NOT NULL REFERENCES supplier_products (id) ON DELETE RESTRICT,
    ingredient_id      uuid REFERENCES ingredients (id) ON DELETE RESTRICT,
    description        text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    packs              numeric(14, 3) NOT NULL CHECK (packs > 0),
    pack_quantity      numeric(14, 3) NOT NULL CHECK (pack_quantity > 0),
    pack_unit          text NOT NULL REFERENCES units_of_measure (code),
    -- packs × pack in the ingredient's base unit (NULL for packaging items).
    quantity_base      numeric(16, 3),
    base_unit          text REFERENCES units_of_measure (code),
    unit_price_minor   bigint NOT NULL CHECK (unit_price_minor >= 0),
    line_total_minor   bigint NOT NULL CHECK (line_total_minor >= 0),
    quotation_line_id  uuid REFERENCES quotation_lines (id) ON DELETE RESTRICT,
    -- The planning line this purchase is for; inbound counts only there.
    procurement_line_id uuid REFERENCES procurement_lines (id) ON DELETE SET NULL,
    -- Maintained by receiving, in packs.
    received_packs     numeric(14, 3) NOT NULL DEFAULT 0 CHECK (received_packs >= 0),
    accepted_packs     numeric(14, 3) NOT NULL DEFAULT 0 CHECK (accepted_packs >= 0),
    CONSTRAINT purchase_order_lines_position UNIQUE (po_id, position)
);
CREATE INDEX purchase_order_lines_ingredient_idx ON purchase_order_lines (ingredient_id);
CREATE INDEX purchase_order_lines_procurement_idx ON purchase_order_lines (procurement_line_id);

-- The PO as approved, per revision. Never changed.
CREATE TABLE purchase_order_revisions (
    po_id       uuid NOT NULL REFERENCES purchase_orders (id) ON DELETE RESTRICT,
    revision    integer NOT NULL,
    snapshot    jsonb NOT NULL,
    total_minor bigint NOT NULL,
    reason      text,
    approved_by uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (po_id, revision)
);
CREATE TRIGGER purchase_order_revisions_append_only BEFORE UPDATE OR DELETE ON purchase_order_revisions
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE purchase_order_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    po_id       uuid NOT NULL REFERENCES purchase_orders (id) ON DELETE RESTRICT,
    from_status text,
    to_status   text NOT NULL,
    note        text,
    actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX purchase_order_events_idx ON purchase_order_events (po_id, id);
CREATE TRIGGER purchase_order_events_append_only BEFORE UPDATE OR DELETE ON purchase_order_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- inventory policy

-- Allocation per ingredient category. No row: FIFO, shown as the default.
CREATE TABLE inventory_policies (
    category   text PRIMARY KEY,
    method     text NOT NULL CHECK (method IN ('fifo', 'fefo')),
    note       text NOT NULL DEFAULT '' CHECK (char_length(note) <= 300),
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- What receiving inspects, from the approved procedure. None shipped: without
-- one, deliveries can be quarantined or rejected but not accepted.
CREATE TABLE receiving_checklist_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    criteria      text NOT NULL CHECK (char_length(criteria) BETWEEN 1 AND 1000),
    -- Empty: every ingredient category.
    categories    text[] NOT NULL DEFAULT '{}',
    procedure_ref text NOT NULL DEFAULT '' CHECK (char_length(procedure_ref) <= 120),
    position      smallint NOT NULL DEFAULT 0,
    active        boolean NOT NULL DEFAULT true,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- receiving

CREATE TABLE goods_receipts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number          text NOT NULL UNIQUE,
    po_id           uuid NOT NULL REFERENCES purchase_orders (id) ON DELETE RESTRICT,
    supplier_id     uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    -- One form submission is one receipt, however often it is sent.
    idempotency_key text NOT NULL UNIQUE CHECK (char_length(idempotency_key) BETWEEN 8 AND 100),
    delivery_ref    text NOT NULL DEFAULT '' CHECK (char_length(delivery_ref) <= 120),
    notes           text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    received_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX goods_receipts_po_idx ON goods_receipts (po_id);
CREATE TRIGGER goods_receipts_append_only BEFORE UPDATE OR DELETE ON goods_receipts
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE goods_receipt_lines (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id         uuid NOT NULL REFERENCES goods_receipts (id) ON DELETE RESTRICT,
    po_line_id         uuid NOT NULL REFERENCES purchase_order_lines (id) ON DELETE RESTRICT,
    ingredient_id      uuid REFERENCES ingredients (id) ON DELETE RESTRICT,
    -- In packs, as on the PO line. delivered = accepted + quarantined + rejected.
    delivered_packs    numeric(14, 3) NOT NULL CHECK (delivered_packs > 0),
    accepted_packs     numeric(14, 3) NOT NULL DEFAULT 0 CHECK (accepted_packs >= 0),
    quarantined_packs  numeric(14, 3) NOT NULL DEFAULT 0 CHECK (quarantined_packs >= 0),
    rejected_packs     numeric(14, 3) NOT NULL DEFAULT 0 CHECK (rejected_packs >= 0),
    supplier_lot_ref   text NOT NULL DEFAULT '' CHECK (char_length(supplier_lot_ref) <= 120),
    expiry_date        date,
    location_id        uuid REFERENCES storage_locations (id) ON DELETE RESTRICT,
    -- [{item_id, name, result: pass|fail|not_applicable, note}]
    inspection         jsonb NOT NULL DEFAULT '[]',
    temperature_c      numeric(5, 1),
    discrepancy        text NOT NULL DEFAULT 'none' CHECK (discrepancy IN ('none', 'short', 'over', 'damaged', 'wrong_item',
                           'specification', 'expiry', 'temperature', 'documentation', 'other')),
    discrepancy_note   text NOT NULL DEFAULT '' CHECK (char_length(discrepancy_note) <= 1000),
    unit_cost_minor    bigint NOT NULL CHECK (unit_cost_minor >= 0),
    currency           text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT goods_receipt_lines_balance CHECK (delivered_packs = accepted_packs + quarantined_packs + rejected_packs),
    CONSTRAINT goods_receipt_lines_discrepancy CHECK (discrepancy = 'none' OR char_length(discrepancy_note) > 0)
);
CREATE INDEX goods_receipt_lines_po_line_idx ON goods_receipt_lines (po_line_id);
CREATE TRIGGER goods_receipt_lines_append_only BEFORE UPDATE OR DELETE ON goods_receipt_lines
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- lots and the ledger

CREATE TABLE inventory_lots (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Internal identifier; the supplier's own lot reference is kept separately.
    code              text NOT NULL UNIQUE,
    ingredient_id     uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    supplier_id       uuid NOT NULL REFERENCES suppliers (id) ON DELETE RESTRICT,
    po_line_id        uuid NOT NULL REFERENCES purchase_order_lines (id) ON DELETE RESTRICT,
    receipt_line_id   uuid NOT NULL REFERENCES goods_receipt_lines (id) ON DELETE RESTRICT,
    supplier_lot_ref  text NOT NULL DEFAULT '',
    received_at       timestamptz NOT NULL,
    unit              text NOT NULL REFERENCES units_of_measure (code),
    received_quantity numeric(16, 3) NOT NULL CHECK (received_quantity > 0),
    -- Maintained from the ledger in the same transaction.
    on_hand           numeric(16, 3) NOT NULL CHECK (on_hand >= 0),
    reserved          numeric(16, 3) NOT NULL DEFAULT 0 CHECK (reserved >= 0),
    status            text NOT NULL CHECK (status IN ('available', 'quarantined', 'on_hold', 'rejected')),
    expiry_date       date,
    -- recorded: a date from the product or supplier; not_recorded: unknown, cannot be allocated;
    -- none_applies: a person confirmed no date applies.
    expiry_status     text NOT NULL CHECK (expiry_status IN ('recorded', 'not_recorded', 'none_applies')),
    expiry_note       text NOT NULL DEFAULT '' CHECK (char_length(expiry_note) <= 300),
    location_id       uuid NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT,
    unit_cost_minor   bigint,
    currency          text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_lots_reserved CHECK (reserved <= on_hand),
    CONSTRAINT inventory_lots_expiry CHECK ((expiry_status = 'recorded') = (expiry_date IS NOT NULL)),
    CONSTRAINT inventory_lots_usable_reserved CHECK (status = 'available' OR reserved = 0)
);
CREATE INDEX inventory_lots_ingredient_idx ON inventory_lots (ingredient_id, status) WHERE on_hand > 0;

-- The stock ledger. Every change to a lot's on-hand or reserved quantity is
-- one row; the lot's figures equal the sums of its rows.
CREATE TABLE inventory_movements (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lot_id          uuid NOT NULL REFERENCES inventory_lots (id) ON DELETE RESTRICT,
    ingredient_id   uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    type            text NOT NULL CHECK (type IN ('receipt', 'reserve', 'release', 'issue', 'return', 'transfer', 'adjustment',
                        'waste', 'supplier_return', 'status')),
    on_hand_delta   numeric(16, 3) NOT NULL DEFAULT 0,
    reserved_delta  numeric(16, 3) NOT NULL DEFAULT 0,
    on_hand_after   numeric(16, 3) NOT NULL,
    reserved_after  numeric(16, 3) NOT NULL,
    unit            text NOT NULL REFERENCES units_of_measure (code),
    reason          text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 1000),
    from_location   uuid REFERENCES storage_locations (id) ON DELETE RESTRICT,
    to_location     uuid REFERENCES storage_locations (id) ON DELETE RESTRICT,
    from_status     text,
    to_status       text,
    batch_id        uuid REFERENCES production_batches (id) ON DELETE RESTRICT,
    plan_id         uuid REFERENCES production_plans (id) ON DELETE RESTRICT,
    reference_type  text,
    reference_id    uuid,
    actor_id        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX inventory_movements_lot_idx ON inventory_movements (lot_id, id);
CREATE INDEX inventory_movements_created_idx ON inventory_movements (created_at);
CREATE INDEX inventory_movements_batch_idx ON inventory_movements (batch_id) WHERE batch_id IS NOT NULL;
CREATE TRIGGER inventory_movements_append_only BEFORE UPDATE OR DELETE ON inventory_movements
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- Stock held for a released production plan, lot by lot.
CREATE TABLE inventory_reservations (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id        uuid NOT NULL REFERENCES production_plans (id) ON DELETE RESTRICT,
    ingredient_id  uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    lot_id         uuid NOT NULL REFERENCES inventory_lots (id) ON DELETE RESTRICT,
    quantity       numeric(16, 3) NOT NULL CHECK (quantity > 0),
    -- What is still held (issues draw it down).
    open_quantity  numeric(16, 3) NOT NULL CHECK (open_quantity >= 0),
    status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'issued', 'released')),
    release_reason text,
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_reservations_open CHECK (open_quantity <= quantity),
    CONSTRAINT inventory_reservations_active CHECK (status = 'active' OR open_quantity = 0)
);
CREATE INDEX inventory_reservations_plan_idx ON inventory_reservations (plan_id, ingredient_id) WHERE status = 'active';
CREATE INDEX inventory_reservations_lot_idx ON inventory_reservations (lot_id) WHERE status = 'active';

-- Which lots went into which batch (the relationship Sprint 5 anticipated).
CREATE TABLE batch_ingredient_lots (
    batch_id      uuid NOT NULL REFERENCES production_batches (id) ON DELETE RESTRICT,
    lot_id        uuid NOT NULL REFERENCES inventory_lots (id) ON DELETE RESTRICT,
    ingredient_id uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    unit          text NOT NULL REFERENCES units_of_measure (code),
    issued        numeric(16, 3) NOT NULL DEFAULT 0 CHECK (issued >= 0),
    returned      numeric(16, 3) NOT NULL DEFAULT 0 CHECK (returned >= 0),
    first_issued_at timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (batch_id, lot_id),
    CONSTRAINT batch_ingredient_lots_returned CHECK (returned <= issued)
);
CREATE INDEX batch_ingredient_lots_lot_idx ON batch_ingredient_lots (lot_id);

CREATE TABLE stock_adjustments (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lot_id           uuid NOT NULL REFERENCES inventory_lots (id) ON DELETE RESTRICT,
    category         text NOT NULL CHECK (category IN ('count_discrepancy', 'damage', 'spoilage', 'measurement_correction', 'other')),
    previous_on_hand numeric(16, 3) NOT NULL,
    new_on_hand      numeric(16, 3) NOT NULL CHECK (new_on_hand >= 0),
    reason           text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 1000),
    evidence         text NOT NULL DEFAULT '' CHECK (char_length(evidence) <= 500),
    count_line_id    uuid,
    actor_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stock_adjustments_lot_idx ON stock_adjustments (lot_id);
CREATE TRIGGER stock_adjustments_append_only BEFORE UPDATE OR DELETE ON stock_adjustments
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE stock_counts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number      text NOT NULL UNIQUE,
    location_id uuid NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT,
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'submitted', 'posted', 'cancelled')),
    notes       text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    started_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    submitted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    posted_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    submitted_at timestamptz,
    posted_at   timestamptz
);
-- One open count per location at a time.
CREATE UNIQUE INDEX stock_counts_open_key ON stock_counts (location_id) WHERE status IN ('open', 'submitted');

CREATE TABLE stock_count_lines (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    count_id          uuid NOT NULL REFERENCES stock_counts (id) ON DELETE CASCADE,
    lot_id            uuid NOT NULL REFERENCES inventory_lots (id) ON DELETE RESTRICT,
    -- The ledger balance when the line was counted, and the last movement then.
    expected          numeric(16, 3) NOT NULL,
    last_movement_id  bigint NOT NULL,
    counted           numeric(16, 3) NOT NULL CHECK (counted >= 0),
    counted_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    counted_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stock_count_lines_key UNIQUE (count_id, lot_id)
);

ALTER TABLE stock_adjustments ADD CONSTRAINT stock_adjustments_count_fk FOREIGN KEY (count_line_id) REFERENCES stock_count_lines (id) ON DELETE RESTRICT;

-- ---------------------------------------------------------------- costing

-- Where a reference cost came from: quoted, ordered and received costs stay
-- distinct and are never overwritten.
ALTER TABLE ingredient_cost_references
    ADD COLUMN origin text NOT NULL DEFAULT 'reference' CHECK (origin IN ('reference', 'supplier_quote', 'purchase_order', 'receipt')),
    ADD COLUMN origin_id uuid;

-- ---------------------------------------------------------------- event consumers

-- How far each consumer has handed domain events to the job queue.
CREATE TABLE event_consumer_offsets (
    consumer      text PRIMARY KEY,
    last_event_id bigint NOT NULL DEFAULT 0,
    updated_at    timestamptz NOT NULL DEFAULT now()
);
INSERT INTO event_consumer_offsets (consumer) VALUES ('procurement');

-- ---------------------------------------------------------------- roles and permissions

INSERT INTO roles (key, name, description) VALUES
    ('procurement_manager',  'Procurement manager',  'Plans purchasing, approves suppliers and purchase orders.'),
    ('procurement_officer',  'Procurement officer',  'Prepares purchasing, quotations and purchase orders.'),
    ('inventory_manager',    'Inventory manager',    'Runs stores: receiving, lots, reservations, issues, adjustments and counts.'),
    ('receiving_staff',      'Receiving staff',      'Receives and inspects deliveries.'),
    ('supplier_coordinator', 'Supplier coordinator', 'Keeps supplier and farmer records and their products up to date.');

INSERT INTO permissions (key, description) VALUES
    ('suppliers.read',                        'View suppliers, farmers, their contacts and documents.'),
    ('suppliers.manage',                      'Create and edit suppliers, farmer profiles, documents and communication notes.'),
    ('suppliers.approve',                     'Review, approve, reject and suspend suppliers.'),
    ('supplier_products.read',                'View supplier products, prices and availability.'),
    ('supplier_products.manage',              'Edit supplier products, ingredient mappings, prices and availability.'),
    ('procurement.read',                      'View procurement runs, requirements, recommendations, RFQs and quotations.'),
    ('procurement.plan',                      'Run procurement planning, make purchasing decisions, RFQs and quotations.'),
    ('procurement.approve',                   'Approve purchasing decisions for a production plan.'),
    ('procurement.configure',                 'Set procurement policies: approval limit, tolerances, currencies, exceptions.'),
    ('purchase_orders.read',                  'View purchase orders and their prices.'),
    ('purchase_orders.create',                'Create and edit draft purchase orders.'),
    ('purchase_orders.approve',               'Approve purchase orders someone else prepared.'),
    ('purchase_orders.approve_above_limit',   'Approve purchase orders above the configured approval limit.'),
    ('purchase_orders.issue',                 'Issue approved purchase orders to suppliers.'),
    ('receiving.read',                        'View deliveries received and their inspections.'),
    ('receiving.manage',                      'Record deliveries against purchase orders.'),
    ('receiving.inspect',                     'Inspect deliveries, accept, quarantine or reject stock and review lot expiry.'),
    ('inventory.read',                        'View stock, lots and the movement ledger.'),
    ('inventory.reserve',                     'Reserve and release stock for production plans.'),
    ('inventory.issue',                       'Issue stock to production batches and record returns.'),
    ('inventory.adjust',                      'Adjust stock with a reason, transfer lots and record store waste.'),
    ('inventory.count',                       'Count stock and post count variances.'),
    ('inventory.configure',                   'Configure storage locations, allocation policies and receiving checklists.'),
    ('traceability.read',                     'Follow ingredient lots from supplier to production batch.'),
    ('supplier_reports.read',                 'View procurement, inventory and supplier reports.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM unnest(ARRAY['super_admin', 'admin']) AS r,
     unnest(ARRAY['suppliers.read', 'suppliers.manage', 'suppliers.approve', 'supplier_products.read', 'supplier_products.manage',
                  'procurement.read', 'procurement.plan', 'procurement.approve', 'procurement.configure', 'purchase_orders.read',
                  'purchase_orders.create', 'purchase_orders.approve', 'purchase_orders.approve_above_limit', 'purchase_orders.issue',
                  'receiving.read', 'receiving.manage', 'receiving.inspect', 'inventory.read', 'inventory.reserve', 'inventory.issue',
                  'inventory.adjust', 'inventory.count', 'inventory.configure', 'traceability.read', 'supplier_reports.read']) AS p;

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('procurement_manager', 'admin.access'), ('procurement_manager', 'suppliers.read'), ('procurement_manager', 'suppliers.manage'),
    ('procurement_manager', 'suppliers.approve'), ('procurement_manager', 'supplier_products.read'),
    ('procurement_manager', 'supplier_products.manage'), ('procurement_manager', 'procurement.read'),
    ('procurement_manager', 'procurement.plan'), ('procurement_manager', 'procurement.approve'),
    ('procurement_manager', 'procurement.configure'), ('procurement_manager', 'purchase_orders.read'),
    ('procurement_manager', 'purchase_orders.create'), ('procurement_manager', 'purchase_orders.approve'),
    ('procurement_manager', 'purchase_orders.issue'), ('procurement_manager', 'receiving.read'),
    ('procurement_manager', 'inventory.read'), ('procurement_manager', 'inventory.reserve'),
    ('procurement_manager', 'traceability.read'), ('procurement_manager', 'supplier_reports.read'),
    ('procurement_manager', 'ingredients.read'), ('procurement_manager', 'production.read'),

    ('procurement_officer', 'admin.access'), ('procurement_officer', 'suppliers.read'), ('procurement_officer', 'suppliers.manage'),
    ('procurement_officer', 'supplier_products.read'), ('procurement_officer', 'supplier_products.manage'),
    ('procurement_officer', 'procurement.read'), ('procurement_officer', 'procurement.plan'),
    ('procurement_officer', 'purchase_orders.read'), ('procurement_officer', 'purchase_orders.create'),
    ('procurement_officer', 'receiving.read'), ('procurement_officer', 'inventory.read'),
    ('procurement_officer', 'supplier_reports.read'), ('procurement_officer', 'ingredients.read'),
    ('procurement_officer', 'production.read'),

    ('supplier_coordinator', 'admin.access'), ('supplier_coordinator', 'suppliers.read'), ('supplier_coordinator', 'suppliers.manage'),
    ('supplier_coordinator', 'supplier_products.read'), ('supplier_coordinator', 'supplier_products.manage'),
    ('supplier_coordinator', 'ingredients.read'),

    ('inventory_manager', 'admin.access'), ('inventory_manager', 'inventory.read'), ('inventory_manager', 'inventory.reserve'),
    ('inventory_manager', 'inventory.issue'), ('inventory_manager', 'inventory.adjust'), ('inventory_manager', 'inventory.count'),
    ('inventory_manager', 'inventory.configure'), ('inventory_manager', 'receiving.read'), ('inventory_manager', 'receiving.manage'),
    ('inventory_manager', 'receiving.inspect'), ('inventory_manager', 'traceability.read'),
    ('inventory_manager', 'purchase_orders.read'), ('inventory_manager', 'suppliers.read'),
    ('inventory_manager', 'supplier_reports.read'), ('inventory_manager', 'ingredients.read'),
    ('inventory_manager', 'production.read'),

    ('receiving_staff', 'admin.access'), ('receiving_staff', 'receiving.read'), ('receiving_staff', 'receiving.manage'),
    ('receiving_staff', 'receiving.inspect'), ('receiving_staff', 'inventory.read'), ('receiving_staff', 'ingredients.read'),

    ('kitchen_staff', 'inventory.read'), ('kitchen_staff', 'inventory.issue'),
    ('finance', 'purchase_orders.read'), ('finance', 'purchase_orders.approve_above_limit'),
    ('finance', 'procurement.read'), ('finance', 'supplier_reports.read'),
    ('quality_inspector', 'receiving.read'), ('quality_inspector', 'receiving.inspect'),
    ('quality_inspector', 'inventory.read'), ('quality_inspector', 'traceability.read')
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('suppliers.read', 'suppliers.manage', 'suppliers.approve',
    'supplier_products.read', 'supplier_products.manage', 'procurement.read', 'procurement.plan', 'procurement.approve',
    'procurement.configure', 'purchase_orders.read', 'purchase_orders.create', 'purchase_orders.approve',
    'purchase_orders.approve_above_limit', 'purchase_orders.issue', 'receiving.read', 'receiving.manage', 'receiving.inspect',
    'inventory.read', 'inventory.reserve', 'inventory.issue', 'inventory.adjust', 'inventory.count', 'inventory.configure',
    'traceability.read', 'supplier_reports.read');
DELETE FROM role_permissions WHERE role_key IN ('procurement_manager', 'procurement_officer', 'inventory_manager', 'receiving_staff',
    'supplier_coordinator');
DELETE FROM permissions WHERE key IN ('suppliers.read', 'suppliers.manage', 'suppliers.approve',
    'supplier_products.read', 'supplier_products.manage', 'procurement.read', 'procurement.plan', 'procurement.approve',
    'procurement.configure', 'purchase_orders.read', 'purchase_orders.create', 'purchase_orders.approve',
    'purchase_orders.approve_above_limit', 'purchase_orders.issue', 'receiving.read', 'receiving.manage', 'receiving.inspect',
    'inventory.read', 'inventory.reserve', 'inventory.issue', 'inventory.adjust', 'inventory.count', 'inventory.configure',
    'traceability.read', 'supplier_reports.read');
-- Rolling back removes the roles, so pending invitations to them go too (Sprint 9 fix:
-- an outstanding invitation used to block the rollback).
DELETE FROM staff_invitations WHERE role_key IN ('procurement_manager', 'procurement_officer', 'inventory_manager', 'receiving_staff',
    'supplier_coordinator');
DELETE FROM user_roles WHERE role_key IN ('procurement_manager', 'procurement_officer', 'inventory_manager', 'receiving_staff',
    'supplier_coordinator');
DELETE FROM roles WHERE key IN ('procurement_manager', 'procurement_officer', 'inventory_manager', 'receiving_staff',
    'supplier_coordinator');

DROP TABLE event_consumer_offsets;
ALTER TABLE ingredient_cost_references DROP COLUMN origin_id, DROP COLUMN origin;
ALTER TABLE stock_adjustments DROP CONSTRAINT stock_adjustments_count_fk;
DROP TABLE stock_count_lines;
DROP TABLE stock_counts;
DROP TABLE stock_adjustments;
DROP TABLE batch_ingredient_lots;
DROP TABLE inventory_reservations;
DROP TABLE inventory_movements;
DROP TABLE inventory_lots;
DROP TABLE goods_receipt_lines;
DROP TABLE goods_receipts;
DROP TABLE receiving_checklist_items;
DROP TABLE inventory_policies;
DROP TABLE purchase_order_events;
DROP TABLE purchase_order_revisions;
DROP TABLE purchase_order_lines;
DROP TABLE purchase_orders;
DROP TABLE storage_locations;
ALTER TABLE supplier_prices DROP CONSTRAINT supplier_prices_quotation_fk;
DROP TABLE quotation_lines;
DROP TABLE quotations;
DROP TABLE rfq_suppliers;
DROP TABLE rfq_lines;
DROP TABLE rfqs;
DROP TABLE procurement_lines;
DROP TABLE procurement_requirements;
DROP TABLE procurement_runs;
DROP TABLE procurement_settings;
DROP TABLE supplier_availability;
DROP TABLE supplier_prices;
DROP TABLE supplier_products;
DROP TABLE supplier_communications;
DROP TABLE supplier_documents;
DROP TABLE farmer_profiles;
DROP TABLE supplier_reviews;
DROP TABLE supplier_qualification_criteria;
DROP TABLE supplier_status_events;
DROP TABLE suppliers;
DROP TABLE supply_counters;
