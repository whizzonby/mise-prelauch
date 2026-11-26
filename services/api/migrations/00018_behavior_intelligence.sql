-- Sprint 9.2: first-party customer behaviour events (customer app) and the
-- Mise Intelligence permissions.
--
-- behavior_events follows the privacy rules of Sprint 1's lead_events: a
-- fixed contract of event names (versioned), short identifiers only (no
-- form contents, no names, emails or card data), nothing recorded under
-- Global Privacy Control or Do Not Track (enforced by the apps), no cookies.
-- event_id is chosen by the sender, so a retried event is stored once.
-- Business facts (payments, renewals, pauses, cancellations, support cases)
-- are not re-recorded here: they are read from domain_events and their tables.

-- +goose Up

CREATE TABLE behavior_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id     uuid NOT NULL UNIQUE,
    name         text NOT NULL CHECK (name IN ('catalogue_viewed', 'meal_viewed', 'plan_viewed', 'plan_selected', 'meal_selected', 'checkout_started')),
    version      smallint NOT NULL CHECK (version >= 1),
    customer_id  uuid REFERENCES customers (id) ON DELETE CASCADE,
    anonymous_id text CHECK (anonymous_id IS NULL OR anonymous_id ~ '^[A-Za-z0-9_-]{8,64}$'),
    session_id   text CHECK (session_id IS NULL OR session_id ~ '^[A-Za-z0-9_-]{8,64}$'),
    properties   jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(properties) = 'object'),
    occurred_at  timestamptz NOT NULL DEFAULT now(),
    received_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT behavior_events_actor CHECK (customer_id IS NOT NULL OR anonymous_id IS NOT NULL)
);
CREATE INDEX behavior_events_name_time_idx ON behavior_events (name, occurred_at);
CREATE INDEX behavior_events_customer_idx ON behavior_events (customer_id, occurred_at) WHERE customer_id IS NOT NULL;

INSERT INTO permissions (key, description) VALUES
    ('intelligence.read',                   'Open Mise Intelligence (the executive command center).'),
    ('intelligence.ask',                    'Ask Mise AI. Answers use only the domains your other permissions allow.'),
    ('intelligence_executive.read',         'Executive overview, briefings and business risks.'),
    ('intelligence_financial.read',         'Financial intelligence: management summary, cash view, financial risks.'),
    ('intelligence_customer.read',          'Customer intelligence: funnels, segments, retention.'),
    ('intelligence_operational.read',       'Meal, supply, production and delivery intelligence.'),
    ('intelligence_scenario.run',           'Run and save financial scenarios.'),
    ('intelligence_recommendation.review',  'Save, dismiss and assign intelligence recommendations; review meal proposals.'),
    ('intelligence.configure',              'Configure intelligence: risk thresholds, AI provider limits and budgets, briefing schedules.'),
    ('intelligence.export',                 'Export intelligence reports.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('super_admin', ARRAY['intelligence.read', 'intelligence.ask', 'intelligence_executive.read', 'intelligence_financial.read',
                          'intelligence_customer.read', 'intelligence_operational.read', 'intelligence_scenario.run',
                          'intelligence_recommendation.review', 'intelligence.configure', 'intelligence.export']),
    ('admin',       ARRAY['intelligence.read', 'intelligence.ask', 'intelligence_executive.read', 'intelligence_financial.read',
                          'intelligence_customer.read', 'intelligence_operational.read', 'intelligence_scenario.run',
                          'intelligence_recommendation.review', 'intelligence.export']),
    ('finance',     ARRAY['intelligence.read', 'intelligence.ask', 'intelligence_financial.read', 'intelligence_scenario.run', 'intelligence.export']),
    ('operations',  ARRAY['intelligence.read', 'intelligence.ask', 'intelligence_operational.read']),
    ('marketing',   ARRAY['intelligence.read', 'intelligence.ask', 'intelligence_customer.read']),
    ('analyst',     ARRAY['intelligence.read', 'intelligence_customer.read', 'intelligence_operational.read'])
) AS g(r, perms), unnest(perms) AS p;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key LIKE 'intelligence%';
DELETE FROM permissions WHERE key LIKE 'intelligence%';
DROP TABLE behavior_events;
