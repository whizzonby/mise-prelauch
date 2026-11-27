-- Sprint 9.2 Stage 6: saved financial scenarios and their versions.
--
-- A version stores the fully resolved inputs (each with its source:
-- observed, preset, assumption or what-if), the observed snapshot they
-- started from, the engine version, an input hash and the output. Versions
-- are never changed; saving identical inputs again returns the existing
-- version instead of a new one. Scenarios are management planning only:
-- nothing here changes prices, orders or any other record.

-- +goose Up

CREATE TABLE finance_scenarios (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    preset      text NOT NULL CHECK (preset IN ('base', 'conservative', 'optimistic', 'custom')),
    archived_at timestamptz,
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE finance_scenario_versions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id    uuid NOT NULL REFERENCES finance_scenarios (id) ON DELETE CASCADE,
    version        integer NOT NULL CHECK (version >= 1),
    horizon_days   integer NOT NULL CHECK (horizon_days IN (30, 60, 90, 182, 365)),
    inputs         jsonb NOT NULL CHECK (jsonb_typeof(inputs) = 'object'),
    changes        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(changes) = 'array'),
    observed       jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(observed) = 'object'),
    engine_version text NOT NULL,
    input_hash     text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    output         jsonb NOT NULL CHECK (jsonb_typeof(output) = 'object'),
    note           text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scenario_id, version),
    UNIQUE (scenario_id, input_hash)
);

-- +goose Down

DROP TABLE finance_scenario_versions;
DROP TABLE finance_scenarios;
