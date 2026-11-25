-- +goose Up

-- Sprint 5: the Chef and Production ERP. Ingredients, units, recipes and
-- their versions (culinary module); demand snapshots, plans, ingredient
-- requirements, batches, kitchen tasks, quality, packaging, yield and waste
-- (production module); and an append-only outbox of domain events.
--
-- Quantities are numeric, never floating point. Nothing here seeds real
-- recipes, prices, nutrition, procedures or shelf lives: those must come
-- from the business. See docs/architecture/SPRINT_5_PLAN.md.

-- ---------------------------------------------------------------- units

CREATE TABLE units_of_measure (
    code      text PRIMARY KEY CHECK (code ~ '^[a-z][a-z_]{0,19}$'),
    name      text NOT NULL,
    -- mass (base g), volume (base ml), count (base piece), portion (base portion)
    dimension text NOT NULL CHECK (dimension IN ('mass', 'volume', 'count', 'portion')),
    -- How many of the dimension's base unit one of this unit is. Exact.
    to_base   numeric(20, 6) NOT NULL CHECK (to_base > 0),
    position  smallint NOT NULL DEFAULT 0
);

-- Only exact, definitional factors. Kitchen spoons and cups are left out
-- until the business decides it wants them.
INSERT INTO units_of_measure (code, name, dimension, to_base, position) VALUES
    ('g',       'Grams',       'mass',    1,    1),
    ('kg',      'Kilograms',   'mass',    1000, 2),
    ('ml',      'Millilitres', 'volume',  1,    3),
    ('l',       'Litres',      'volume',  1000, 4),
    ('piece',   'Pieces',      'count',   1,    5),
    ('portion', 'Portions',    'portion', 1,    6);

-- ---------------------------------------------------------------- ingredients

CREATE TABLE ingredients (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description       text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    category          text NOT NULL CHECK (category IN ('produce', 'meat', 'poultry', 'seafood', 'dairy', 'eggs', 'grains',
                          'legumes', 'herbs_spices', 'condiments', 'oils_fats', 'baking', 'beverages', 'packaged', 'other')),
    -- The canonical unit every requirement for this ingredient is stated in.
    base_unit         text NOT NULL REFERENCES units_of_measure (code),
    purchasing_unit   text CHECK (purchasing_unit IS NULL OR char_length(purchasing_unit) <= 60),
    preparation_state text NOT NULL DEFAULT 'raw' CHECK (preparation_state IN ('raw', 'prepared', 'cooked', 'processed', 'dried', 'frozen', 'canned')),
    -- Allergen keys from internal/vocab (MealAllergens). Empty means none
    -- recorded, never "free from".
    allergens         text[] NOT NULL DEFAULT '{}',
    storage           text NOT NULL DEFAULT 'unspecified' CHECK (storage IN ('unspecified', 'ambient', 'chilled', 'frozen')),
    storage_notes     text NOT NULL DEFAULT '' CHECK (char_length(storage_notes) <= 500),
    -- Only when verified from a supplier or a procedure; free text, not a rule.
    shelf_life_note   text CHECK (shelf_life_note IS NULL OR char_length(shelf_life_note) <= 300),
    active            boolean NOT NULL DEFAULT true,
    created_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- One ingredient per name, whatever the letter case or spacing.
CREATE UNIQUE INDEX ingredients_name_key ON ingredients (lower(regexp_replace(btrim(name), '\s+', ' ', 'g')));
CREATE INDEX ingredients_category_idx ON ingredients (category, active);

CREATE TABLE ingredient_aliases (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ingredient_id uuid NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE,
    alias         text NOT NULL CHECK (char_length(alias) BETWEEN 1 AND 100),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ingredient_aliases_key ON ingredient_aliases (lower(regexp_replace(btrim(alias), '\s+', ' ', 'g')));
CREATE INDEX ingredient_aliases_ingredient_idx ON ingredient_aliases (ingredient_id);

-- Conversions across dimensions (volume to mass, pieces to mass) exist only
-- for one ingredient at a time, with where the figure came from.
-- 1 from_unit = factor to_unit.
CREATE TABLE ingredient_unit_conversions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ingredient_id uuid NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE,
    from_unit     text NOT NULL REFERENCES units_of_measure (code),
    to_unit       text NOT NULL REFERENCES units_of_measure (code),
    factor        numeric(20, 6) NOT NULL CHECK (factor > 0),
    -- Pieces vary in weight: such a conversion is an average and is shown as one.
    approximate   boolean NOT NULL DEFAULT false,
    source        text NOT NULL CHECK (char_length(source) BETWEEN 1 AND 300),
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ingredient_unit_conversions_key UNIQUE (ingredient_id, from_unit, to_unit),
    CONSTRAINT ingredient_unit_conversions_distinct CHECK (from_unit <> to_unit)
);

-- Reference costs for estimates. Never a purchase price: Sprint 6 records
-- those. Empty until someone enters a sourced figure.
CREATE TABLE ingredient_cost_references (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ingredient_id uuid REFERENCES ingredients (id) ON DELETE CASCADE,
    -- A packaging cost has no ingredient; it is per kit.
    kind          text NOT NULL DEFAULT 'ingredient' CHECK (kind IN ('ingredient', 'packaging')),
    label         text NOT NULL DEFAULT '' CHECK (char_length(label) <= 100),
    -- amount_minor buys per_quantity of unit.
    amount_minor  bigint NOT NULL CHECK (amount_minor >= 0),
    currency      text NOT NULL DEFAULT 'TTD' CHECK (currency ~ '^[A-Z]{3}$'),
    per_quantity  numeric(14, 3) NOT NULL CHECK (per_quantity > 0),
    unit          text NOT NULL REFERENCES units_of_measure (code),
    source        text NOT NULL CHECK (char_length(source) BETWEEN 1 AND 300),
    effective_from date NOT NULL,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ingredient_cost_kind CHECK ((kind = 'ingredient') = (ingredient_id IS NOT NULL))
);

CREATE INDEX ingredient_cost_references_idx ON ingredient_cost_references (ingredient_id, effective_from DESC);

-- ---------------------------------------------------------------- chefs

-- The customer-facing chef card (Sprint 2) becomes the chef record too:
-- linked to the person's staff account, with specialities and whether they
-- are working with Mise. No second chef table, no second login.
ALTER TABLE chef_profiles
    ADD COLUMN user_id      uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN specialities text[] NOT NULL DEFAULT '{}',
    ADD COLUMN work_status  text NOT NULL DEFAULT 'active' CHECK (work_status IN ('active', 'inactive'));
CREATE UNIQUE INDEX chef_profiles_user_key ON chef_profiles (user_id) WHERE user_id IS NOT NULL;

-- ---------------------------------------------------------------- recipes

CREATE TABLE recipe_counters (
    id   boolean PRIMARY KEY DEFAULT true CHECK (id),
    last bigint NOT NULL DEFAULT 0
);
INSERT INTO recipe_counters DEFAULT VALUES;

CREATE TABLE recipes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    category    text NOT NULL DEFAULT '' CHECK (char_length(category) <= 60),
    -- The catalogue meal this recipe produces. One recipe per meal.
    meal_id     uuid REFERENCES meals (id) ON DELETE RESTRICT,
    chef_id     uuid REFERENCES chef_profiles (id) ON DELETE SET NULL,
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT recipes_code_key UNIQUE (code)
);

CREATE UNIQUE INDEX recipes_meal_key ON recipes (meal_id) WHERE meal_id IS NOT NULL;
CREATE INDEX recipes_chef_idx ON recipes (chef_id);

-- People who may edit a recipe's drafts besides its chef.
CREATE TABLE recipe_collaborators (
    recipe_id  uuid NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (recipe_id, user_id)
);

CREATE TABLE recipe_versions (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id              uuid NOT NULL REFERENCES recipes (id) ON DELETE RESTRICT,
    number                 integer NOT NULL CHECK (number > 0),
    status                 text NOT NULL DEFAULT 'draft' CHECK (status IN
        ('draft', 'submitted', 'under_review', 'changes_requested', 'approved', 'published', 'archived')),
    -- How many servings the BOM below makes.
    yield_servings         smallint NOT NULL DEFAULT 2 CHECK (yield_servings BETWEEN 1 AND 100),
    serving_size           text NOT NULL DEFAULT '' CHECK (char_length(serving_size) <= 60),
    serving_size_g         numeric(10, 3) CHECK (serving_size_g IS NULL OR serving_size_g > 0),
    kitchen_prep_minutes   smallint CHECK (kitchen_prep_minutes IS NULL OR kitchen_prep_minutes BETWEEN 0 AND 1440),
    customer_cook_minutes  smallint CHECK (customer_cook_minutes IS NULL OR customer_cook_minutes BETWEEN 0 AND 480),
    storage_instructions   text NOT NULL DEFAULT '' CHECK (char_length(storage_instructions) <= 1000),
    packaging_instructions text NOT NULL DEFAULT '' CHECK (char_length(packaging_instructions) <= 1000),
    customer_instructions  text NOT NULL DEFAULT '' CHECK (char_length(customer_instructions) <= 4000),
    dietary_tags           text[] NOT NULL DEFAULT '{}',
    cross_contact_note     text NOT NULL DEFAULT '' CHECK (char_length(cross_contact_note) <= 500),
    change_summary         text NOT NULL DEFAULT '' CHECK (char_length(change_summary) <= 500),
    -- First delivery week this version is used for, once published.
    effective_from         date CHECK (effective_from IS NULL OR extract(isodow FROM effective_from) = 1),
    author_id              uuid REFERENCES users (id) ON DELETE SET NULL,
    based_on               uuid REFERENCES recipe_versions (id) ON DELETE SET NULL,
    submitted_at           timestamptz,
    approved_by            uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at            timestamptz,
    published_by           uuid REFERENCES users (id) ON DELETE SET NULL,
    published_at           timestamptz,
    archived_at            timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT recipe_versions_number_key UNIQUE (recipe_id, number),
    CONSTRAINT recipe_versions_published CHECK (status NOT IN ('published') OR (published_at IS NOT NULL AND effective_from IS NOT NULL)),
    CONSTRAINT recipe_versions_approved CHECK (status NOT IN ('approved', 'published') OR approved_at IS NOT NULL)
);

-- One open revision per recipe at a time.
CREATE UNIQUE INDEX recipe_versions_one_open ON recipe_versions (recipe_id)
    WHERE status IN ('draft', 'submitted', 'under_review', 'changes_requested');
CREATE INDEX recipe_versions_status_idx ON recipe_versions (status, updated_at DESC);
CREATE INDEX recipe_versions_effective_idx ON recipe_versions (recipe_id, effective_from DESC) WHERE status = 'published';

-- The bill of materials of one version.
CREATE TABLE recipe_version_ingredients (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id       uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE CASCADE,
    position         smallint NOT NULL CHECK (position BETWEEN 0 AND 200),
    ingredient_id    uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    -- Net quantity for the whole yield, as it goes into the kit.
    quantity         numeric(14, 3) NOT NULL CHECK (quantity > 0),
    unit             text NOT NULL REFERENCES units_of_measure (code),
    preparation      text NOT NULL DEFAULT '' CHECK (char_length(preparation) <= 100),
    -- Usable share after trimming or cooking loss, when verified (e.g. 85.0).
    -- NULL means no loss is applied.
    yield_percent    numeric(5, 2) CHECK (yield_percent IS NULL OR (yield_percent > 0 AND yield_percent <= 100)),
    optional         boolean NOT NULL DEFAULT false,
    substitution     text NOT NULL DEFAULT 'none' CHECK (substitution IN ('none', 'equivalent_allowed', 'approval_required')),
    note             text NOT NULL DEFAULT '' CHECK (char_length(note) <= 300),

    CONSTRAINT recipe_version_ingredients_position UNIQUE (version_id, position)
);

CREATE INDEX recipe_version_ingredients_ingredient_idx ON recipe_version_ingredients (ingredient_id);

CREATE TABLE recipe_steps (
    version_id uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE CASCADE,
    -- preparation: in the kitchen; assembly: putting the kit together.
    kind       text NOT NULL CHECK (kind IN ('preparation', 'assembly')),
    position   smallint NOT NULL CHECK (position BETWEEN 0 AND 100),
    body       text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 1000),
    PRIMARY KEY (version_id, kind, position)
);

CREATE TABLE recipe_allergens (
    version_id uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE CASCADE,
    allergen   text NOT NULL CHECK (char_length(allergen) BETWEEN 1 AND 30),
    -- contains: an ingredient contains it; may_contain: cross-contact risk.
    kind       text NOT NULL CHECK (kind IN ('contains', 'may_contain')),
    PRIMARY KEY (version_id, allergen)
);

-- Per serving. Every figure is optional until review: incomplete data is
-- shown as incomplete, never filled in.
CREATE TABLE recipe_nutrition (
    version_id    uuid PRIMARY KEY REFERENCES recipe_versions (id) ON DELETE CASCADE,
    energy_kcal   numeric(7, 1) CHECK (energy_kcal IS NULL OR energy_kcal BETWEEN 0 AND 5000),
    protein_g     numeric(6, 1) CHECK (protein_g IS NULL OR protein_g BETWEEN 0 AND 500),
    carbs_g       numeric(6, 1) CHECK (carbs_g IS NULL OR carbs_g BETWEEN 0 AND 500),
    fat_g         numeric(6, 1) CHECK (fat_g IS NULL OR fat_g BETWEEN 0 AND 500),
    sat_fat_g     numeric(6, 1) CHECK (sat_fat_g IS NULL OR sat_fat_g BETWEEN 0 AND 500),
    fibre_g       numeric(6, 1) CHECK (fibre_g IS NULL OR fibre_g BETWEEN 0 AND 200),
    sugar_g       numeric(6, 1) CHECK (sugar_g IS NULL OR sugar_g BETWEEN 0 AND 500),
    sodium_mg     numeric(8, 1) CHECK (sodium_mg IS NULL OR sodium_mg BETWEEN 0 AND 20000),
    source        text CHECK (source IS NULL OR source IN ('laboratory_analysis', 'food_composition_database', 'supplier_specification', 'other')),
    method        text NOT NULL DEFAULT '' CHECK (char_length(method) <= 500),
    entered_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Every review decision, append-only. A material edit sets invalidated_at
-- on the version's earlier reviews; nothing else may change a row.
CREATE TABLE recipe_reviews (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id     uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE RESTRICT,
    stage          text NOT NULL CHECK (stage IN ('culinary', 'nutrition', 'final', 'publish')),
    decision       text NOT NULL CHECK (decision IN ('approved', 'changes_requested', 'rejected', 'published')),
    notes          text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    reviewer_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    invalidated_at timestamptz
);

CREATE INDEX recipe_reviews_version_idx ON recipe_reviews (version_id, created_at);

-- +goose StatementBegin
CREATE FUNCTION recipe_reviews_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.invalidated_at IS NULL AND NEW.invalidated_at IS NOT NULL
       AND (NEW.version_id, NEW.stage, NEW.decision, NEW.notes, NEW.reviewer_id, NEW.created_at)
           IS NOT DISTINCT FROM (OLD.version_id, OLD.stage, OLD.decision, OLD.notes, OLD.reviewer_id, OLD.created_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'recipe_reviews are append-only' USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER recipe_reviews_append_only BEFORE UPDATE OR DELETE ON recipe_reviews
    FOR EACH ROW EXECUTE FUNCTION recipe_reviews_guard();

-- The content of a version can change only while it is a draft or has
-- changes requested. Approved and published versions are immutable.
-- +goose StatementBegin
CREATE FUNCTION recipe_content_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v uuid;
    st text;
BEGIN
    IF TG_OP = 'DELETE' THEN v := OLD.version_id; ELSE v := NEW.version_id; END IF;
    SELECT status INTO st FROM recipe_versions WHERE id = v;
    -- A cascade from deleting the version itself (never done) finds nothing.
    IF st IS NULL OR st IN ('draft', 'changes_requested') THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'recipe version % is % and cannot be edited', v, st USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER recipe_bom_guard BEFORE INSERT OR UPDATE OR DELETE ON recipe_version_ingredients
    FOR EACH ROW EXECUTE FUNCTION recipe_content_guard();
CREATE TRIGGER recipe_steps_guard BEFORE INSERT OR UPDATE OR DELETE ON recipe_steps
    FOR EACH ROW EXECUTE FUNCTION recipe_content_guard();
CREATE TRIGGER recipe_allergens_guard BEFORE INSERT OR UPDATE OR DELETE ON recipe_allergens
    FOR EACH ROW EXECUTE FUNCTION recipe_content_guard();
CREATE TRIGGER recipe_nutrition_guard BEFORE INSERT OR UPDATE OR DELETE ON recipe_nutrition
    FOR EACH ROW EXECUTE FUNCTION recipe_content_guard();

-- +goose StatementBegin
CREATE FUNCTION recipe_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'recipe versions are never deleted' USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.status IN ('draft', 'changes_requested') THEN
        RETURN NEW;
    END IF;
    -- Past the draft stage only the workflow columns move.
    IF (NEW.recipe_id, NEW.number, NEW.yield_servings, NEW.serving_size, NEW.serving_size_g, NEW.kitchen_prep_minutes,
        NEW.customer_cook_minutes, NEW.storage_instructions, NEW.packaging_instructions, NEW.customer_instructions,
        NEW.dietary_tags, NEW.cross_contact_note, NEW.author_id, NEW.based_on, NEW.created_at)
       IS DISTINCT FROM
       (OLD.recipe_id, OLD.number, OLD.yield_servings, OLD.serving_size, OLD.serving_size_g, OLD.kitchen_prep_minutes,
        OLD.customer_cook_minutes, OLD.storage_instructions, OLD.packaging_instructions, OLD.customer_instructions,
        OLD.dietary_tags, OLD.cross_contact_note, OLD.author_id, OLD.based_on, OLD.created_at) THEN
        RAISE EXCEPTION 'recipe version % is % and cannot be edited', OLD.id, OLD.status USING ERRCODE = 'restrict_violation';
    END IF;
    -- An approved or published version never goes back to being editable.
    IF OLD.status IN ('approved', 'published', 'archived') AND NEW.status IN ('draft', 'submitted', 'under_review', 'changes_requested') THEN
        RAISE EXCEPTION 'recipe version % is % and cannot reopen', OLD.id, OLD.status USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.status IN ('published', 'archived') AND NEW.effective_from IS DISTINCT FROM OLD.effective_from THEN
        RAISE EXCEPTION 'the effective week of a published version is fixed' USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER recipe_versions_guard BEFORE UPDATE OR DELETE ON recipe_versions
    FOR EACH ROW EXECUTE FUNCTION recipe_version_guard();

-- ---------------------------------------------------------------- events

-- Transactional outbox of domain events: written in the same transaction as
-- the change. Consumers (procurement, delivery) read by id and must be
-- idempotent; delivery is at least once.
CREATE TABLE domain_events (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type           text NOT NULL CHECK (type ~ '^[a-z_]+\.[a-z_]+$'),
    aggregate_type text NOT NULL,
    aggregate_id   uuid NOT NULL,
    -- Makes a retried write a no-op.
    dedupe_key     text NOT NULL DEFAULT '',
    payload        jsonb NOT NULL DEFAULT '{}',
    request_id     text,
    occurred_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT domain_events_dedupe UNIQUE (type, aggregate_id, dedupe_key)
);

CREATE INDEX domain_events_aggregate_idx ON domain_events (aggregate_type, aggregate_id, id);
CREATE TRIGGER domain_events_append_only BEFORE UPDATE OR DELETE ON domain_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- planning

-- The proposed weekly rhythm. Stored, not assumed: status says whether the
-- business has approved it.
CREATE TABLE production_schedule_slots (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    label            text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 60),
    -- ISO weekdays (1 = Monday) in the delivery week.
    prep_weekday     smallint NOT NULL CHECK (prep_weekday BETWEEN 1 AND 7),
    delivery_weekday smallint NOT NULL CHECK (delivery_weekday BETWEEN 1 AND 7),
    status           text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'approved', 'retired')),
    created_at       timestamptz NOT NULL DEFAULT now()
);

INSERT INTO production_schedule_slots (label, prep_weekday, delivery_weekday, status) VALUES
    ('Monday preparation for Tuesday delivery', 1, 2, 'proposed'),
    ('Wednesday preparation for Thursday delivery', 3, 4, 'proposed');

CREATE TABLE production_settings (
    id                     boolean PRIMARY KEY DEFAULT true CHECK (id),
    -- Kits the kitchen can assemble in a day. NULL: not recorded.
    capacity_kits_per_day  integer CHECK (capacity_kits_per_day IS NULL OR capacity_kits_per_day > 0),
    capacity_note          text NOT NULL DEFAULT '' CHECK (char_length(capacity_note) <= 500),
    -- No foreign key: this one row must survive any user deletion. The audit
    -- log records who changed it.
    updated_by             uuid,
    updated_at             timestamptz NOT NULL DEFAULT now()
);
INSERT INTO production_settings DEFAULT VALUES;

CREATE TABLE production_demand_snapshots (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    week_start   date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    revision     integer NOT NULL CHECK (revision > 0),
    cutoff_at    timestamptz NOT NULL,
    -- sha256 of the sorted (order, meal, quantity, recipe version) lines.
    content_hash text NOT NULL,
    status       text NOT NULL DEFAULT 'current' CHECK (status IN ('current', 'superseded')),
    kits         integer NOT NULL CHECK (kits >= 0),
    orders       integer NOT NULL CHECK (orders >= 0),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT production_demand_snapshots_revision UNIQUE (week_start, revision)
);

CREATE UNIQUE INDEX production_demand_snapshots_current ON production_demand_snapshots (week_start) WHERE status = 'current';

-- One line per order and meal: the source of every kit counted.
CREATE TABLE production_demand_lines (
    snapshot_id        uuid NOT NULL REFERENCES production_demand_snapshots (id) ON DELETE RESTRICT,
    order_id           uuid NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    order_number       text NOT NULL,
    meal_id            uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    meal_version       integer NOT NULL,
    meal_name          text NOT NULL,
    quantity           smallint NOT NULL CHECK (quantity > 0),
    servings           smallint NOT NULL CHECK (servings > 0),
    dietary_tags       text[] NOT NULL DEFAULT '{}',
    customer_allergens text[] NOT NULL DEFAULT '{}',
    -- The recipe version in force for this meal and week, if one is published.
    recipe_version_id  uuid REFERENCES recipe_versions (id) ON DELETE RESTRICT,

    PRIMARY KEY (snapshot_id, order_id, meal_id)
);

CREATE TRIGGER production_demand_lines_immutable BEFORE UPDATE OR DELETE ON production_demand_lines
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE production_plan_counters (
    id   boolean PRIMARY KEY DEFAULT true CHECK (id),
    last bigint NOT NULL DEFAULT 0
);
INSERT INTO production_plan_counters DEFAULT VALUES;

CREATE TABLE production_plans (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number          text NOT NULL,
    week_start      date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    snapshot_id     uuid NOT NULL REFERENCES production_demand_snapshots (id) ON DELETE RESTRICT,
    production_date date NOT NULL,
    status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'approved', 'released', 'cancelled')),
    notes           text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at     timestamptz,
    released_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    released_at     timestamptz,
    cancelled_at    timestamptz,
    cancel_reason   text CHECK (cancel_reason IS NULL OR char_length(cancel_reason) <= 500),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT production_plans_number_key UNIQUE (number)
);

-- One live plan per delivery week: a second create (a double click, a
-- retry, a second manager) finds the first.
CREATE UNIQUE INDEX production_plans_one_live ON production_plans (week_start) WHERE status <> 'cancelled';
CREATE INDEX production_plans_date_idx ON production_plans (production_date);

CREATE TABLE production_plan_items (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id           uuid NOT NULL REFERENCES production_plans (id) ON DELETE RESTRICT,
    meal_id           uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    meal_name         text NOT NULL,
    recipe_version_id uuid REFERENCES recipe_versions (id) ON DELETE RESTRICT,
    servings_per_kit  smallint NOT NULL CHECK (servings_per_kit > 0),
    -- Kits ordered (from the snapshot) and kits to make. They differ only by
    -- a recorded, reasoned adjustment.
    demand_kits       integer NOT NULL CHECK (demand_kits >= 0),
    planned_kits      integer NOT NULL CHECK (planned_kits >= 0),
    adjustment_reason text CHECK (adjustment_reason IS NULL OR char_length(adjustment_reason) BETWEEN 3 AND 500),
    adjusted_by       uuid REFERENCES users (id) ON DELETE SET NULL,

    CONSTRAINT production_plan_items_meal UNIQUE (plan_id, meal_id),
    CONSTRAINT production_plan_items_reason CHECK (planned_kits = demand_kits OR adjustment_reason IS NOT NULL)
);

CREATE TABLE ingredient_requirements (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id       uuid NOT NULL REFERENCES production_plans (id) ON DELETE CASCADE,
    ingredient_id uuid NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    preparation   text NOT NULL DEFAULT '',
    -- In the ingredient's base unit; NULL when a source could not be converted.
    quantity      numeric(16, 3),
    unit          text NOT NULL REFERENCES units_of_measure (code),
    -- ok, or why the figure cannot be trusted.
    status        text NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'unconvertible', 'inactive_ingredient')),
    approximate   boolean NOT NULL DEFAULT false,
    note          text NOT NULL DEFAULT '',

    CONSTRAINT ingredient_requirements_key UNIQUE (plan_id, ingredient_id, preparation)
);

CREATE TABLE ingredient_requirement_sources (
    requirement_id    uuid NOT NULL REFERENCES ingredient_requirements (id) ON DELETE CASCADE,
    plan_item_id      uuid NOT NULL REFERENCES production_plan_items (id) ON DELETE CASCADE,
    recipe_version_id uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE RESTRICT,
    bom_line_id       uuid NOT NULL REFERENCES recipe_version_ingredients (id) ON DELETE RESTRICT,
    -- As written on the BOM line, scaled to the planned kits, before conversion.
    quantity          numeric(16, 3) NOT NULL,
    unit              text NOT NULL REFERENCES units_of_measure (code),
    PRIMARY KEY (requirement_id, bom_line_id, plan_item_id)
);

-- ---------------------------------------------------------------- batches

CREATE TABLE production_batches (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Printed on labels: the traceability identifier.
    code              text NOT NULL,
    plan_id           uuid NOT NULL REFERENCES production_plans (id) ON DELETE RESTRICT,
    plan_item_id      uuid NOT NULL REFERENCES production_plan_items (id) ON DELETE RESTRICT,
    meal_id           uuid NOT NULL REFERENCES meals (id) ON DELETE RESTRICT,
    meal_name         text NOT NULL,
    recipe_version_id uuid NOT NULL REFERENCES recipe_versions (id) ON DELETE RESTRICT,
    production_date   date NOT NULL,
    planned_kits      integer NOT NULL CHECK (planned_kits >= 0),
    -- Set once output is recorded (production_yield_records holds the detail).
    actual_kits       integer CHECK (actual_kits IS NULL OR actual_kits >= 0),
    assigned_team     text NOT NULL DEFAULT '' CHECK (char_length(assigned_team) <= 60),
    status            text NOT NULL DEFAULT 'planned' CHECK (status IN
        ('planned', 'released', 'in_progress', 'awaiting_quality_check', 'quality_hold', 'completed', 'cancelled')),
    quality_status    text NOT NULL DEFAULT 'pending' CHECK (quality_status IN ('pending', 'passed', 'failed', 'on_hold')),
    packaging_status  text NOT NULL DEFAULT 'pending' CHECK (packaging_status IN ('pending', 'in_progress', 'completed')),
    started_at        timestamptz,
    completed_at      timestamptz,
    cancelled_at      timestamptz,
    cancel_reason     text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT production_batches_code_key UNIQUE (code),
    CONSTRAINT production_batches_item_key UNIQUE (plan_item_id)
);

CREATE INDEX production_batches_date_idx ON production_batches (production_date, status);

-- Every status change of a batch, append-only.
CREATE TABLE production_batch_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    batch_id   uuid NOT NULL REFERENCES production_batches (id) ON DELETE RESTRICT,
    from_status text,
    to_status  text NOT NULL,
    note       text,
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX production_batch_events_idx ON production_batch_events (batch_id, id);
CREATE TRIGGER production_batch_events_immutable BEFORE UPDATE OR DELETE ON production_batch_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- tasks

-- Configured by managers from an approved operating procedure. None are seeded.
CREATE TABLE production_task_templates (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    kind             text NOT NULL CHECK (kind IN ('preparation', 'portioning', 'sauce', 'assembly', 'labelling', 'packaging', 'inspection', 'other')),
    instructions     text NOT NULL DEFAULT '' CHECK (char_length(instructions) <= 1000),
    position         smallint NOT NULL DEFAULT 0,
    priority         text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high')),
    -- Due this many minutes after the start of the production day (00:00).
    due_offset_minutes integer CHECK (due_offset_minutes IS NULL OR due_offset_minutes BETWEEN 0 AND 2880),
    active           boolean NOT NULL DEFAULT true,
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE production_tasks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id     uuid NOT NULL REFERENCES production_batches (id) ON DELETE RESTRICT,
    template_id  uuid REFERENCES production_task_templates (id) ON DELETE SET NULL,
    title        text NOT NULL,
    kind         text NOT NULL,
    instructions text NOT NULL DEFAULT '',
    position     smallint NOT NULL DEFAULT 0,
    priority     text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high')),
    status       text NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'in_progress', 'done', 'blocked', 'cancelled')),
    assigned_to  uuid REFERENCES users (id) ON DELETE SET NULL,
    due_at       timestamptz,
    started_at   timestamptz,
    completed_at timestamptz,
    notes        text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    exception    text CHECK (exception IS NULL OR char_length(exception) <= 1000),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Releasing twice never duplicates a template's task.
CREATE UNIQUE INDEX production_tasks_template_key ON production_tasks (batch_id, template_id) WHERE template_id IS NOT NULL;
CREATE INDEX production_tasks_open_idx ON production_tasks (status, due_at);
CREATE INDEX production_tasks_assignee_idx ON production_tasks (assigned_to, status);

CREATE TABLE production_task_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id    uuid NOT NULL REFERENCES production_tasks (id) ON DELETE RESTRICT,
    type       text NOT NULL CHECK (type IN ('created', 'status', 'assignment', 'note', 'exception')),
    from_value text,
    to_value   text,
    body       text,
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX production_task_events_idx ON production_task_events (task_id, id);
CREATE TRIGGER production_task_events_immutable BEFORE UPDATE OR DELETE ON production_task_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- ---------------------------------------------------------------- quality

-- Configured from an approved procedure; none seeded. Criteria are text the
-- inspector follows: no thresholds are invented here.
CREATE TABLE quality_check_templates (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    check_type  text NOT NULL CHECK (check_type IN ('ingredient_verification', 'quantity_verification', 'packaging_integrity',
                    'label_accuracy', 'allergen_declaration', 'storage_temperature', 'general_inspection')),
    criteria    text NOT NULL CHECK (char_length(criteria) BETWEEN 1 AND 2000),
    -- A failed critical check puts the batch on hold.
    critical    boolean NOT NULL DEFAULT false,
    position    smallint NOT NULL DEFAULT 0,
    active      boolean NOT NULL DEFAULT true,
    procedure_ref text NOT NULL DEFAULT '' CHECK (char_length(procedure_ref) <= 200),
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE quality_checks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id     uuid NOT NULL REFERENCES production_batches (id) ON DELETE RESTRICT,
    template_id  uuid NOT NULL REFERENCES quality_check_templates (id) ON DELETE RESTRICT,
    check_type   text NOT NULL,
    critical     boolean NOT NULL,
    result       text NOT NULL CHECK (result IN ('pass', 'fail')),
    notes        text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    evidence_ref text CHECK (evidence_ref IS NULL OR char_length(evidence_ref) <= 500),
    inspector_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX quality_checks_batch_idx ON quality_checks (batch_id, created_at);
CREATE TRIGGER quality_checks_immutable BEFORE UPDATE OR DELETE ON quality_checks
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

CREATE TABLE quality_holds (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id         uuid NOT NULL REFERENCES production_batches (id) ON DELETE RESTRICT,
    check_id         uuid REFERENCES quality_checks (id) ON DELETE RESTRICT,
    reason           text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 1000),
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'released', 'discarded')),
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    resolved_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    resolved_at      timestamptz,
    resolution_notes text CHECK (resolution_notes IS NULL OR char_length(resolution_notes) <= 1000),

    CONSTRAINT quality_holds_resolved CHECK ((status = 'open') = (resolved_at IS NULL))
);

-- At most one open hold per batch.
CREATE UNIQUE INDEX quality_holds_one_open ON quality_holds (batch_id) WHERE status = 'open';

-- ---------------------------------------------------------------- packing, yield, waste

CREATE TABLE packaging_records (
    batch_id        uuid PRIMARY KEY REFERENCES production_batches (id) ON DELETE RESTRICT,
    packaging_type  text NOT NULL DEFAULT '' CHECK (char_length(packaging_type) <= 100),
    required_kits   integer NOT NULL CHECK (required_kits >= 0),
    packed_kits     integer NOT NULL DEFAULT 0 CHECK (packed_kits >= 0),
    labels_verified boolean NOT NULL DEFAULT false,
    assigned_to     uuid REFERENCES users (id) ON DELETE SET NULL,
    exceptions      text NOT NULL DEFAULT '' CHECK (char_length(exceptions) <= 1000),
    completed_at    timestamptz,
    updated_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Planned versus actual. accepted = produced - rejected; reworked kits are
-- part of produced and of accepted, counted once.
CREATE TABLE production_yield_records (
    batch_id      uuid PRIMARY KEY REFERENCES production_batches (id) ON DELETE RESTRICT,
    planned_kits  integer NOT NULL CHECK (planned_kits >= 0),
    produced_kits integer NOT NULL CHECK (produced_kits >= 0),
    rejected_kits integer NOT NULL DEFAULT 0 CHECK (rejected_kits >= 0),
    reworked_kits integer NOT NULL DEFAULT 0 CHECK (reworked_kits >= 0),
    accepted_kits integer NOT NULL CHECK (accepted_kits >= 0),
    notes         text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 1000),
    recorded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT production_yield_reconciled CHECK (accepted_kits = produced_kits - rejected_kits),
    CONSTRAINT production_yield_rework CHECK (reworked_kits <= accepted_kits)
);

CREATE TABLE production_waste (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id      uuid REFERENCES production_batches (id) ON DELETE RESTRICT,
    ingredient_id uuid REFERENCES ingredients (id) ON DELETE RESTRICT,
    -- What was wasted when it is not a catalogued ingredient (a finished kit, packaging).
    item          text NOT NULL DEFAULT '' CHECK (char_length(item) <= 100),
    quantity      numeric(14, 3) NOT NULL CHECK (quantity > 0),
    unit          text NOT NULL REFERENCES units_of_measure (code),
    category      text NOT NULL CHECK (category IN ('preparation_loss', 'spoilage', 'quality_rejection', 'packaging_damage', 'overproduction', 'other')),
    reason        text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    recorded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT production_waste_what CHECK (ingredient_id IS NOT NULL OR char_length(item) > 0)
);
CREATE INDEX production_waste_created_idx ON production_waste (created_at);
CREATE INDEX production_waste_batch_idx ON production_waste (batch_id);

-- ---------------------------------------------------------------- roles and permissions

INSERT INTO roles (key, name, description) VALUES
    ('culinary_director',  'Culinary director',  'Leads recipe development: reviews, approves and publishes recipes.'),
    ('production_manager', 'Production manager', 'Plans production, approves and releases plans, runs the kitchen.'),
    ('quality_inspector',  'Quality inspector',  'Records quality checks and resolves quality holds.');

INSERT INTO permissions (key, description) VALUES
    ('ingredients.read',        'View the ingredient catalogue, units and conversions.'),
    ('ingredients.manage',      'Create and edit ingredients, aliases, conversions and reference costs.'),
    ('recipes.read',            'View recipes, their versions and review history.'),
    ('recipes.create',          'Start new recipes.'),
    ('recipes.edit',            'Edit drafts of recipes you own or collaborate on.'),
    ('recipes.submit',          'Submit a recipe draft for review.'),
    ('recipes.review',          'Carry out the culinary review of submitted recipes.'),
    ('recipes.approve',         'Give final approval to reviewed recipes, and edit any recipe.'),
    ('recipes.publish',         'Publish approved recipes for production and the catalogue.'),
    ('nutrition.read',          'View recipe nutrition and allergen information.'),
    ('nutrition.review',        'Review recipe nutrition and allergens, and request changes.'),
    ('nutrition.approve',       'Sign off a recipe version''s nutrition and allergen information.'),
    ('chefs.manage',            'Link chefs to staff accounts and set their specialities and status.'),
    ('production.read',         'View production plans, batches, tasks and requirements.'),
    ('production.plan',         'Snapshot demand, create and adjust draft production plans.'),
    ('production.approve',      'Approve, release and cancel production plans; cancel batches.'),
    ('production.execute',      'Start batches, work kitchen tasks and record output.'),
    ('production.configure',    'Configure kitchen task templates, quality checklists and capacity.'),
    ('quality.read',            'View quality checks and holds.'),
    ('quality.inspect',         'Record quality checks.'),
    ('quality.release_hold',    'Release or discard a batch on quality hold.'),
    ('packaging.manage',        'Record packaging of batches.'),
    ('waste.record',            'Record production waste.'),
    ('production_reports.read', 'View production, yield, waste and ingredient requirement reports.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM unnest(ARRAY['super_admin', 'admin']) AS r,
     unnest(ARRAY['ingredients.read', 'ingredients.manage', 'recipes.read', 'recipes.create', 'recipes.edit', 'recipes.submit',
                  'recipes.review', 'recipes.approve', 'recipes.publish', 'nutrition.read', 'nutrition.review', 'nutrition.approve',
                  'chefs.manage', 'production.read', 'production.plan', 'production.approve', 'production.execute',
                  'production.configure', 'quality.read', 'quality.inspect', 'quality.release_hold', 'packaging.manage',
                  'waste.record', 'production_reports.read']) AS p;

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('culinary_director', 'admin.access'), ('culinary_director', 'catalog.read'),
    ('culinary_director', 'ingredients.read'), ('culinary_director', 'ingredients.manage'),
    ('culinary_director', 'recipes.read'), ('culinary_director', 'recipes.create'), ('culinary_director', 'recipes.edit'),
    ('culinary_director', 'recipes.submit'), ('culinary_director', 'recipes.review'), ('culinary_director', 'recipes.approve'),
    ('culinary_director', 'recipes.publish'), ('culinary_director', 'nutrition.read'), ('culinary_director', 'chefs.manage'),
    ('culinary_director', 'production.read'), ('culinary_director', 'quality.read'), ('culinary_director', 'production_reports.read'),
    -- Chefs get the console now that there is a recipe workspace.
    ('chef', 'admin.access'), ('chef', 'ingredients.read'), ('chef', 'recipes.read'), ('chef', 'recipes.create'),
    ('chef', 'recipes.edit'), ('chef', 'recipes.submit'), ('chef', 'nutrition.read'), ('chef', 'production.read'),
    ('nutritionist', 'ingredients.read'), ('nutritionist', 'recipes.read'), ('nutritionist', 'nutrition.read'),
    ('nutritionist', 'nutrition.review'), ('nutritionist', 'nutrition.approve'),
    ('production_manager', 'admin.access'), ('production_manager', 'orders.read'), ('production_manager', 'ingredients.read'),
    ('production_manager', 'recipes.read'), ('production_manager', 'production.read'), ('production_manager', 'production.plan'),
    ('production_manager', 'production.approve'), ('production_manager', 'production.execute'),
    ('production_manager', 'production.configure'), ('production_manager', 'quality.read'), ('production_manager', 'quality.inspect'),
    ('production_manager', 'packaging.manage'), ('production_manager', 'waste.record'), ('production_manager', 'production_reports.read'),
    ('kitchen_staff', 'admin.access'), ('kitchen_staff', 'production.read'), ('kitchen_staff', 'production.execute'),
    ('kitchen_staff', 'packaging.manage'), ('kitchen_staff', 'waste.record'), ('kitchen_staff', 'quality.read'),
    ('quality_inspector', 'admin.access'), ('quality_inspector', 'recipes.read'), ('quality_inspector', 'production.read'),
    ('quality_inspector', 'quality.read'), ('quality_inspector', 'quality.inspect'), ('quality_inspector', 'quality.release_hold'),
    ('operations', 'production.read'), ('operations', 'quality.read'), ('operations', 'production_reports.read');

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('ingredients.read', 'ingredients.manage', 'recipes.read', 'recipes.create',
    'recipes.edit', 'recipes.submit', 'recipes.review', 'recipes.approve', 'recipes.publish', 'nutrition.read', 'nutrition.review',
    'nutrition.approve', 'chefs.manage', 'production.read', 'production.plan', 'production.approve', 'production.execute',
    'production.configure', 'quality.read', 'quality.inspect', 'quality.release_hold', 'packaging.manage', 'waste.record',
    'production_reports.read');
DELETE FROM role_permissions WHERE role_key IN ('chef', 'kitchen_staff', 'culinary_director', 'production_manager', 'quality_inspector')
    AND permission_key IN ('admin.access', 'catalog.read', 'orders.read');
DELETE FROM permissions WHERE key IN ('ingredients.read', 'ingredients.manage', 'recipes.read', 'recipes.create',
    'recipes.edit', 'recipes.submit', 'recipes.review', 'recipes.approve', 'recipes.publish', 'nutrition.read', 'nutrition.review',
    'nutrition.approve', 'chefs.manage', 'production.read', 'production.plan', 'production.approve', 'production.execute',
    'production.configure', 'quality.read', 'quality.inspect', 'quality.release_hold', 'packaging.manage', 'waste.record',
    'production_reports.read');
-- Rolling back removes the roles, so pending invitations to them go too (Sprint 9 fix:
-- an outstanding invitation used to block the rollback).
DELETE FROM staff_invitations WHERE role_key IN ('culinary_director', 'production_manager', 'quality_inspector');
DELETE FROM user_roles WHERE role_key IN ('culinary_director', 'production_manager', 'quality_inspector');
DELETE FROM roles WHERE key IN ('culinary_director', 'production_manager', 'quality_inspector');

DROP TABLE production_waste;
DROP TABLE production_yield_records;
DROP TABLE packaging_records;
DROP TABLE quality_holds;
DROP TABLE quality_checks;
DROP TABLE quality_check_templates;
DROP TABLE production_task_events;
DROP TABLE production_tasks;
DROP TABLE production_task_templates;
DROP TABLE production_batch_events;
DROP TABLE production_batches;
DROP TABLE ingredient_requirement_sources;
DROP TABLE ingredient_requirements;
DROP TABLE production_plan_items;
DROP TABLE production_plans;
DROP TABLE production_plan_counters;
DROP TABLE production_demand_lines;
DROP TABLE production_demand_snapshots;
DROP TABLE production_settings;
DROP TABLE production_schedule_slots;
DROP TABLE domain_events;
DROP TABLE recipe_reviews;
DROP FUNCTION recipe_reviews_guard();
DROP TABLE recipe_nutrition;
DROP TABLE recipe_allergens;
DROP TABLE recipe_steps;
DROP TABLE recipe_version_ingredients;
DROP FUNCTION recipe_content_guard();
DROP TABLE recipe_versions;
DROP FUNCTION recipe_version_guard();
DROP TABLE recipe_collaborators;
DROP TABLE recipes;
DROP TABLE recipe_counters;
DROP INDEX chef_profiles_user_key;
ALTER TABLE chef_profiles DROP COLUMN work_status, DROP COLUMN specialities, DROP COLUMN user_id;
DROP TABLE ingredient_cost_references;
DROP TABLE ingredient_unit_conversions;
DROP TABLE ingredient_aliases;
DROP TABLE ingredients;
DROP TABLE units_of_measure;
