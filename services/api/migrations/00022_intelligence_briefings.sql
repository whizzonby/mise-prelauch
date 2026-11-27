-- Sprint 9.2 Stage 8: executive briefings.
--
-- A briefing's content is deterministic (metrics, changes, finance, risks,
-- opportunities, actions, limitations), computed for a complete period.
-- Versions are stored with an input hash: identical content is never stored
-- twice. An AI narrative may be added to a version when AI is available.
-- Schedules ship disabled with no recipients; recipients are staff accounts,
-- re-checked when each email is sent.

-- +goose Up

CREATE TABLE intelligence_briefings (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cadence       text NOT NULL CHECK (cadence IN ('daily', 'weekly', 'monthly')),
    period_from   date NOT NULL,
    period_to     date NOT NULL,
    version       integer NOT NULL CHECK (version >= 1),
    input_hash    text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    content       jsonb NOT NULL CHECK (jsonb_typeof(content) = 'object'),
    narrative     jsonb,
    narrative_run_id uuid REFERENCES intelligence_runs (id) ON DELETE SET NULL,
    source        text NOT NULL CHECK (source IN ('manual', 'schedule')),
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (cadence, period_from, version),
    UNIQUE (cadence, period_from, input_hash)
);

CREATE TABLE intelligence_briefing_schedules (
    cadence      text PRIMARY KEY CHECK (cadence IN ('daily', 'weekly', 'monthly')),
    enabled      boolean NOT NULL DEFAULT false,
    recipients   uuid[] NOT NULL DEFAULT '{}',
    approval_ref text NOT NULL DEFAULT '' CHECK (char_length(approval_ref) <= 300),
    last_period_from date,
    updated_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
INSERT INTO intelligence_briefing_schedules (cadence) VALUES ('daily'), ('weekly'), ('monthly');

CREATE TABLE intelligence_briefing_deliveries (
    briefing_id uuid NOT NULL REFERENCES intelligence_briefings (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status      text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed', 'skipped')),
    reason      text CHECK (reason IS NULL OR char_length(reason) <= 300),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (briefing_id, user_id)
);

-- +goose Down

DROP TABLE intelligence_briefing_deliveries;
DROP TABLE intelligence_briefing_schedules;
DROP TABLE intelligence_briefings;
