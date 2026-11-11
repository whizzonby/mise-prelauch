-- +goose Up

-- Sprint 2: the customer's own records (profile, household, preferences,
-- allergies, addresses, onboarding, notification choices), the customer-facing
-- meal catalogue, and favourites.
--
-- Additive: nothing existing is dropped or rewritten. Leads, referrals and
-- their history are not touched. Value lists (dietary keys, allergens,
-- regions, topics) are checked in Go, where they are documented, so adding a
-- value needs no migration; the database checks shape and ownership.

-- ---------------------------------------------------------------- profile

ALTER TABLE customers
    ADD COLUMN phone            text CHECK (phone IS NULL OR char_length(phone) BETWEEN 7 AND 20),
    ADD COLUMN contact_channel  text NOT NULL DEFAULT 'email' CHECK (contact_channel IN ('email', 'sms', 'whatsapp')),
    ADD COLUMN language         text NOT NULL DEFAULT 'en' CHECK (language IN ('en')),
    -- Where the customer expects delivery, from onboarding. A region key
    -- (internal/customers/vocab.go); not a promise that Mise delivers there.
    ADD COLUMN delivery_country text CHECK (delivery_country IS NULL OR delivery_country ~ '^[A-Z]{2}$'),
    ADD COLUMN delivery_region  text CHECK (delivery_region IS NULL OR char_length(delivery_region) <= 60);

-- ---------------------------------------------------------------- household

-- Who the customer cooks for. Counts only: no names, ages or accounts for the
-- other people, and nothing about children beyond how many there are.
CREATE TABLE customer_households (
    customer_id uuid PRIMARY KEY REFERENCES customers (id) ON DELETE CASCADE,
    name        text CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 60),
    size        smallint NOT NULL CHECK (size BETWEEN 1 AND 20),
    adults      smallint CHECK (adults IS NULL OR adults BETWEEN 1 AND 20),
    children    smallint CHECK (children IS NULL OR children BETWEEN 0 AND 19),
    source      text NOT NULL DEFAULT 'customer' CHECK (source IN ('customer', 'waitlist')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT customer_households_counts CHECK (coalesce(adults, 0) + coalesce(children, 0) <= size)
);

-- ---------------------------------------------------------------- preferences

-- Food preferences: what the customer would like. Not safety information;
-- allergies are kept apart in customer_allergies.
CREATE TABLE customer_preferences (
    customer_id           uuid PRIMARY KEY REFERENCES customers (id) ON DELETE CASCADE,
    dietary               text[] NOT NULL DEFAULT '{}',
    meal_interests        text[] NOT NULL DEFAULT '{}',
    proteins              text[] NOT NULL DEFAULT '{}',
    max_cook_minutes      smallint CHECK (max_cook_minutes IS NULL OR max_cook_minutes BETWEEN 10 AND 240),
    other_restrictions    text CHECK (other_restrictions IS NULL OR char_length(other_restrictions) <= 300),
    -- 'waitlist' while the values are the ones copied from the lead; the
    -- first edit by the customer makes it 'customer'.
    source                text NOT NULL DEFAULT 'customer' CHECK (source IN ('customer', 'waitlist')),
    -- When the customer last answered the allergy question, including "none".
    -- NULL means never asked, which is not the same as no allergies.
    allergies_reviewed_at timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- Allergies are safety-relevant, sensitive account data. One row per allergen;
-- 'other' carries a short description.
CREATE TABLE customer_allergies (
    customer_id uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    allergen    text NOT NULL CHECK (allergen ~ '^[a-z][a-z-]{1,30}$'),
    detail      text CHECK (detail IS NULL OR char_length(detail) BETWEEN 1 AND 200),
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (customer_id, allergen),
    CONSTRAINT customer_allergies_detail_only_for_other CHECK ((allergen = 'other') = (detail IS NOT NULL))
);

-- ---------------------------------------------------------------- addresses

CREATE TABLE customer_addresses (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id    uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    label          text CHECK (label IS NULL OR char_length(label) BETWEEN 1 AND 40),
    recipient_name text NOT NULL CHECK (char_length(recipient_name) BETWEEN 1 AND 120),
    phone          text CHECK (phone IS NULL OR char_length(phone) BETWEEN 7 AND 20),
    line1          text NOT NULL CHECK (char_length(line1) BETWEEN 1 AND 120),
    line2          text CHECK (line2 IS NULL OR char_length(line2) BETWEEN 1 AND 120),
    area           text CHECK (area IS NULL OR char_length(area) BETWEEN 1 AND 80),
    city           text NOT NULL CHECK (char_length(city) BETWEEN 1 AND 80),
    region         text NOT NULL CHECK (char_length(region) BETWEEN 1 AND 60),
    country        text NOT NULL DEFAULT 'TT' CHECK (country ~ '^[A-Z]{2}$'),
    instructions   text CHECK (instructions IS NULL OR char_length(instructions) <= 300),
    is_default     boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_addresses_customer_idx ON customer_addresses (customer_id, created_at);
-- At most one default address per customer.
CREATE UNIQUE INDEX customer_addresses_one_default ON customer_addresses (customer_id) WHERE is_default;

-- Where Mise delivers. Empty until operations configures it; with no rows,
-- every address is "availability pending".
CREATE TABLE delivery_zones (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    country    text NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    region     text NOT NULL CHECK (char_length(region) BETWEEN 1 AND 60),
    status     text NOT NULL CHECK (status IN ('active', 'planned', 'unavailable')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT delivery_zones_region_key UNIQUE (country, region)
);

-- ---------------------------------------------------------------- onboarding

-- Where the customer is in onboarding. The answers are not kept here: each
-- step writes straight to the customer's own records, so there is one copy.
CREATE TABLE onboarding_progress (
    customer_id  uuid PRIMARY KEY REFERENCES customers (id) ON DELETE CASCADE,
    step         text NOT NULL CHECK (step IN ('welcome', 'household', 'interests', 'dietary', 'allergies', 'location', 'complete')),
    skipped      text[] NOT NULL DEFAULT '{}',
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT onboarding_complete_has_time CHECK ((step = 'complete') = (completed_at IS NOT NULL))
);

-- ---------------------------------------------------------------- notifications

-- Optional email topics. Account and security email is not optional and is
-- not stored here. source says where a choice came from, for consent records.
CREATE TABLE notification_preferences (
    customer_id uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    topic       text NOT NULL CHECK (topic ~ '^[a-z][a-z_]{2,40}$'),
    enabled     boolean NOT NULL,
    source      text NOT NULL CHECK (source IN ('customer', 'waitlist_consent')),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (customer_id, topic)
);

-- ---------------------------------------------------------------- catalogue

-- The customer-facing catalogue. A meal here is how a dish is presented to
-- customers. The production recipe (quantities, yields, costing, versions)
-- will live in the Chef ERP and reference a meal; it is not modelled here.
-- is_demo marks placeholder content; it is hidden and unpublishable unless
-- CATALOG_DEMO_ENABLED is set.

CREATE TABLE chef_profiles (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 80),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 80),
    role_title   text CHECK (role_title IS NULL OR char_length(role_title) <= 80),
    bio          text NOT NULL DEFAULT '' CHECK (char_length(bio) <= 2000),
    image_src    text CHECK (image_src IS NULL OR char_length(image_src) <= 500),
    image_alt    text CHECK (image_alt IS NULL OR char_length(image_alt) <= 200),
    status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'unpublished')),
    is_demo      boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT chef_profiles_slug_key UNIQUE (slug)
);

CREATE TABLE meal_categories (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 60),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
    position    smallint NOT NULL DEFAULT 0,
    is_demo     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT meal_categories_slug_key UNIQUE (slug)
);

CREATE TABLE meals (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug             text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 80),
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    summary          text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 200),
    description      text NOT NULL DEFAULT '' CHECK (char_length(description) <= 3000),
    cooking_overview text NOT NULL DEFAULT '' CHECK (char_length(cooking_overview) <= 3000),
    category_id      uuid NOT NULL REFERENCES meal_categories (id) ON DELETE RESTRICT,
    chef_id          uuid REFERENCES chef_profiles (id) ON DELETE SET NULL,
    prep_minutes     smallint NOT NULL CHECK (prep_minutes BETWEEN 0 AND 240),
    cook_minutes     smallint NOT NULL CHECK (cook_minutes BETWEEN 0 AND 480),
    servings         smallint NOT NULL CHECK (servings BETWEEN 1 AND 12),
    -- Main protein, a key from internal/catalog/vocab.go ("chicken", "plant", ...).
    protein          text CHECK (protein IS NULL OR char_length(protein) <= 30),
    dietary_tags     text[] NOT NULL DEFAULT '{}',
    -- Allergens the meal is declared to contain. Only meaningful when
    -- allergen_status = 'declared'; never read as "free from" anything.
    allergens        text[] NOT NULL DEFAULT '{}',
    allergen_status  text NOT NULL DEFAULT 'unconfirmed' CHECK (allergen_status IN ('unconfirmed', 'declared')),
    status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'unpublished')),
    published_at     timestamptz,
    is_demo          boolean NOT NULL DEFAULT false,
    -- Optimistic concurrency for the admin editor.
    version          integer NOT NULL DEFAULT 1,
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT meals_slug_key UNIQUE (slug),
    CONSTRAINT meals_published_has_time CHECK (status <> 'published' OR published_at IS NOT NULL)
);

CREATE INDEX meals_published_idx ON meals (status, published_at DESC);
CREATE INDEX meals_category_idx ON meals (category_id);
CREATE INDEX meals_chef_idx ON meals (chef_id);
CREATE INDEX meals_dietary_idx ON meals USING gin (dietary_tags);
-- Search over name, summary and description. Queries must use this exact
-- expression for the index to apply (internal/catalog/queries.sql).
CREATE INDEX meals_search_idx ON meals USING gin ((
    setweight(to_tsvector('english', name), 'A') ||
    setweight(to_tsvector('english', summary), 'B') ||
    setweight(to_tsvector('english', description), 'C')));

CREATE TABLE meal_images (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meal_id    uuid NOT NULL REFERENCES meals (id) ON DELETE CASCADE,
    position   smallint NOT NULL CHECK (position BETWEEN 0 AND 20),
    src        text NOT NULL CHECK (char_length(src) BETWEEN 1 AND 500),
    alt        text NOT NULL CHECK (char_length(alt) BETWEEN 1 AND 200),
    width      integer CHECK (width IS NULL OR width BETWEEN 1 AND 10000),
    height     integer CHECK (height IS NULL OR height BETWEEN 1 AND 10000),
    credit     text CHECK (credit IS NULL OR char_length(credit) <= 200),
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT meal_images_position_key UNIQUE (meal_id, position)
);

-- The ingredients a customer receives, as listed on the meal page. Display
-- text, not production quantities.
CREATE TABLE meal_ingredients (
    meal_id  uuid NOT NULL REFERENCES meals (id) ON DELETE CASCADE,
    position smallint NOT NULL CHECK (position BETWEEN 0 AND 60),
    name     text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    quantity text CHECK (quantity IS NULL OR char_length(quantity) <= 60),

    PRIMARY KEY (meal_id, position)
);

-- Per-serving nutrition. Shown to customers only once verified by someone
-- holding nutrition.verify.
CREATE TABLE meal_nutrition (
    meal_id       uuid PRIMARY KEY REFERENCES meals (id) ON DELETE CASCADE,
    calories      smallint NOT NULL CHECK (calories BETWEEN 0 AND 5000),
    protein_g     numeric(5, 1) NOT NULL CHECK (protein_g BETWEEN 0 AND 500),
    carbs_g       numeric(5, 1) NOT NULL CHECK (carbs_g BETWEEN 0 AND 500),
    fat_g         numeric(5, 1) NOT NULL CHECK (fat_g BETWEEN 0 AND 500),
    fibre_g       numeric(5, 1) CHECK (fibre_g IS NULL OR fibre_g BETWEEN 0 AND 200),
    sodium_mg     smallint CHECK (sodium_mg IS NULL OR sodium_mg BETWEEN 0 AND 20000),
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified')),
    verified_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    verified_at   timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT meal_nutrition_verified CHECK ((status = 'verified') = (verified_at IS NOT NULL))
);

CREATE TABLE customer_favorites (
    customer_id uuid NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    meal_id     uuid NOT NULL REFERENCES meals (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (customer_id, meal_id)
);

CREATE INDEX customer_favorites_recent_idx ON customer_favorites (customer_id, created_at DESC);

-- ---------------------------------------------------------------- email change

ALTER TABLE auth_tokens DROP CONSTRAINT auth_tokens_purpose_check;
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_purpose_check
    CHECK (purpose IN ('verify_email', 'reset_password', 'change_email'));
-- The address a change_email token will switch the account to.
ALTER TABLE auth_tokens ADD COLUMN new_email text
    CHECK (new_email IS NULL OR char_length(new_email) BETWEEN 3 AND 254);
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_new_email_only_for_change
    CHECK ((purpose = 'change_email') = (new_email IS NOT NULL));

-- A password changed while signed in ends the account's other sessions.
ALTER TABLE sessions DROP CONSTRAINT sessions_revoked_reason_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_revoked_reason_check CHECK (revoked_reason IS NULL OR revoked_reason IN
    ('logout', 'user_revoked', 'password_reset', 'password_changed', 'admin_revoked', 'account_disabled', 'mfa_failed', 'mfa_reset'));

-- ---------------------------------------------------------------- permissions

INSERT INTO permissions (key, description) VALUES
    ('catalog.read',     'View meals, categories and chef profiles in the admin, including drafts.'),
    ('catalog.manage',   'Create and edit draft meals, categories and chef profiles.'),
    ('catalog.publish',  'Publish and unpublish meals and chef profiles.'),
    ('nutrition.verify', 'Enter and verify the nutrition figures shown to customers.');

INSERT INTO role_permissions (role_key, permission_key) VALUES
    ('super_admin', 'catalog.read'), ('super_admin', 'catalog.manage'), ('super_admin', 'catalog.publish'),
    ('admin', 'catalog.read'), ('admin', 'catalog.manage'), ('admin', 'catalog.publish'),
    ('marketing', 'catalog.read'), ('marketing', 'catalog.manage'),
    -- Nutritionists sign in to the console to verify nutrition figures. Chefs
    -- get console access with the recipe module, not before.
    ('nutritionist', 'admin.access'), ('nutritionist', 'catalog.read'), ('nutritionist', 'nutrition.verify');

-- +goose Down

DELETE FROM role_permissions WHERE permission_key IN ('catalog.read', 'catalog.manage', 'catalog.publish', 'nutrition.verify');
DELETE FROM role_permissions WHERE role_key = 'nutritionist' AND permission_key = 'admin.access';
DELETE FROM permissions WHERE key IN ('catalog.read', 'catalog.manage', 'catalog.publish', 'nutrition.verify');

UPDATE sessions SET revoked_reason = 'user_revoked' WHERE revoked_reason = 'password_changed';
ALTER TABLE sessions DROP CONSTRAINT sessions_revoked_reason_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_revoked_reason_check CHECK (revoked_reason IS NULL OR revoked_reason IN
    ('logout', 'user_revoked', 'password_reset', 'admin_revoked', 'account_disabled', 'mfa_failed', 'mfa_reset'));

DELETE FROM auth_tokens WHERE purpose = 'change_email';
ALTER TABLE auth_tokens DROP CONSTRAINT auth_tokens_new_email_only_for_change;
ALTER TABLE auth_tokens DROP COLUMN new_email;
ALTER TABLE auth_tokens DROP CONSTRAINT auth_tokens_purpose_check;
ALTER TABLE auth_tokens ADD CONSTRAINT auth_tokens_purpose_check CHECK (purpose IN ('verify_email', 'reset_password'));

DROP TABLE customer_favorites;
DROP TABLE meal_nutrition;
DROP TABLE meal_ingredients;
DROP TABLE meal_images;
DROP TABLE meals;
DROP TABLE meal_categories;
DROP TABLE chef_profiles;
DROP TABLE notification_preferences;
DROP TABLE onboarding_progress;
DROP TABLE delivery_zones;
DROP TABLE customer_addresses;
DROP TABLE customer_allergies;
DROP TABLE customer_preferences;
DROP TABLE customer_households;

ALTER TABLE customers
    DROP COLUMN delivery_region,
    DROP COLUMN delivery_country,
    DROP COLUMN language,
    DROP COLUMN contact_channel,
    DROP COLUMN phone;
