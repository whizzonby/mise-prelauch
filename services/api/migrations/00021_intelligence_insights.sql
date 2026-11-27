-- Sprint 9.2 Stage 7: risk thresholds, recommendations and meal proposals.
--
-- Risk indicators are computed (never stored) from the metric registry and
-- the finance summary; only their thresholds live here. The defaults are
-- engineering defaults, marked as such until an owner changes them.
--
-- Recommendations and meal proposals are advisory records. Nothing here can
-- charge, refund, change prices, approve orders, change production, publish
-- meals or recipes, or send campaigns: a meal proposal handed to culinary
-- only links to a recipe created through the existing recipe workflow.

-- +goose Up

CREATE TABLE intelligence_risk_thresholds (
    key         text PRIMARY KEY,
    enabled     boolean NOT NULL DEFAULT true,
    warn        numeric(14, 4) NOT NULL,
    critical    numeric(14, 4) NOT NULL,
    is_default  boolean NOT NULL DEFAULT true,
    note        text NOT NULL DEFAULT '' CHECK (char_length(note) <= 300),
    updated_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
INSERT INTO intelligence_risk_thresholds (key, warn, critical) VALUES
    ('renewals_declining', -10, -25),
    ('payment_failures', 3, 10),
    ('refunds_growing', 5, 10),
    ('ingredient_costs_rising', 5, 15),
    ('contribution_falling', -5, -10),
    ('inventory_expiry', 1, 5),
    ('supplier_delays', 90, 75),
    ('excessive_waste', 5, 10),
    ('delivery_failures', 95, 85),
    ('incomplete_financial_records', 1, 4),
    ('cash_shortage', 13, 4);

CREATE TABLE intelligence_recommendations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         text NOT NULL CHECK (kind IN ('risk', 'opportunity', 'ai_suggestion', 'proposal')),
    -- observed: computed from records; ai_hypothesis: written by AI, unverified;
    -- business_proposal: entered by a person; validated: a reviewer confirmed it.
    basis        text NOT NULL CHECK (basis IN ('observed', 'ai_hypothesis', 'business_proposal', 'validated')),
    domain       text NOT NULL CHECK (domain IN ('executive', 'financial', 'customer', 'operational', 'general')),
    source_key   text NOT NULL DEFAULT '' CHECK (char_length(source_key) <= 200),
    title        text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    summary      text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 2000),
    evidence     jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(evidence) = 'object'),
    review_href  text NOT NULL DEFAULT '' CHECK (char_length(review_href) <= 200),
    status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'saved', 'assigned', 'dismissed', 'done')),
    assigned_to  uuid REFERENCES users (id) ON DELETE SET NULL,
    run_id       uuid REFERENCES intelligence_runs (id) ON DELETE SET NULL,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
-- One open item per computed risk or opportunity and period.
CREATE UNIQUE INDEX intelligence_recommendations_source_idx ON intelligence_recommendations (source_key)
    WHERE source_key <> '' AND status IN ('open', 'saved', 'assigned');

CREATE TABLE intelligence_recommendation_events (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recommendation_id uuid NOT NULL REFERENCES intelligence_recommendations (id) ON DELETE CASCADE,
    action            text NOT NULL,
    from_status       text NOT NULL DEFAULT '',
    to_status         text NOT NULL,
    note              text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    actor             uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE intelligence_meal_proposals (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    origin             text NOT NULL CHECK (origin IN ('ai', 'manual')),
    name               text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description        text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    target_audience    text NOT NULL DEFAULT '' CHECK (char_length(target_audience) <= 500),
    -- [{name, quantity, note, ingredient_id (when it matches the catalogue)}]
    ingredients        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(ingredients) = 'array'),
    complexity         text NOT NULL DEFAULT 'unknown' CHECK (complexity IN ('low', 'medium', 'high', 'unknown')),
    availability_notes text[] NOT NULL DEFAULT '{}',
    limitations        text[] NOT NULL DEFAULT '{}',
    evidence           jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(evidence) = 'object'),
    status             text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'in_review', 'approved_for_development', 'in_development', 'closed', 'rejected')),
    recipe_id          uuid REFERENCES recipes (id) ON DELETE SET NULL,
    run_id             uuid REFERENCES intelligence_runs (id) ON DELETE SET NULL,
    created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE intelligence_meal_proposal_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    proposal_id uuid NOT NULL REFERENCES intelligence_meal_proposals (id) ON DELETE CASCADE,
    action      text NOT NULL,
    from_status text NOT NULL DEFAULT '',
    to_status   text NOT NULL,
    note        text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    actor       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE intelligence_meal_proposal_events;
DROP TABLE intelligence_meal_proposals;
DROP TABLE intelligence_recommendation_events;
DROP TABLE intelligence_recommendations;
DROP TABLE intelligence_risk_thresholds;
