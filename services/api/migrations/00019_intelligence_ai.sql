-- Sprint 9.2 Stage 5: the AI assistant (Ask Mise AI). Settings, limits and
-- budgets; the history of every intelligence run.
--
-- The provider, model and credentials come only from the environment; the
-- database holds the owner's switch, limits, budget and prices. AI ships
-- switched off with no budget: nothing reaches a provider until an owner
-- sets a budget, prices and turns it on.
--
-- intelligence_runs keeps what is needed to audit an answer (question,
-- tools called with their arguments, evidence, validation, the answer,
-- tokens and cost), never raw tool output: tool results are aggregates and
-- are recomputed from the authoritative tables when needed. Runs older than
-- the retention setting are deleted by the worker.

-- +goose Up

CREATE TABLE intelligence_ai_settings (
    id                        boolean PRIMARY KEY DEFAULT true CHECK (id),
    enabled                   boolean NOT NULL DEFAULT false,
    -- Organisation budget per calendar month, in millionths of a US dollar
    -- (providers bill in USD). NULL = no budget set = AI unavailable.
    monthly_budget_usd_micros bigint CHECK (monthly_budget_usd_micros IS NULL OR monthly_budget_usd_micros > 0),
    -- Provider prices per million tokens, in millionths of a US dollar,
    -- copied by the owner from the provider's price list. NULL = unknown =
    -- the budget cannot be enforced = AI unavailable.
    input_price_usd_micros_per_mtok  bigint CHECK (input_price_usd_micros_per_mtok IS NULL OR input_price_usd_micros_per_mtok >= 0),
    output_price_usd_micros_per_mtok bigint CHECK (output_price_usd_micros_per_mtok IS NULL OR output_price_usd_micros_per_mtok >= 0),
    price_reference           text NOT NULL DEFAULT '' CHECK (char_length(price_reference) <= 300),
    user_daily_requests       integer NOT NULL DEFAULT 20 CHECK (user_daily_requests BETWEEN 1 AND 1000),
    max_output_tokens         integer NOT NULL DEFAULT 1500 CHECK (max_output_tokens BETWEEN 200 AND 8000),
    max_input_tokens          integer NOT NULL DEFAULT 30000 CHECK (max_input_tokens BETWEEN 2000 AND 200000),
    max_tool_calls            integer NOT NULL DEFAULT 6 CHECK (max_tool_calls BETWEEN 1 AND 20),
    max_seconds               integer NOT NULL DEFAULT 60 CHECK (max_seconds BETWEEN 10 AND 300),
    retention_days            integer NOT NULL DEFAULT 180 CHECK (retention_days BETWEEN 7 AND 3650),
    approval_ref              text NOT NULL DEFAULT '' CHECK (char_length(approval_ref) <= 300),
    updated_by                uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at                timestamptz NOT NULL DEFAULT now()
);
INSERT INTO intelligence_ai_settings DEFAULT VALUES;

CREATE TABLE intelligence_runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            text NOT NULL CHECK (kind IN ('ask', 'briefing', 'meal_concepts')),
    user_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    job             text NOT NULL DEFAULT '' CHECK (char_length(job) <= 100),
    provider        text NOT NULL DEFAULT '',
    model           text NOT NULL DEFAULT '',
    prompt_version  text NOT NULL,
    question        text NOT NULL DEFAULT '' CHECK (char_length(question) <= 2000),
    period_from     date,
    period_to       date,
    status          text NOT NULL CHECK (status IN ('completed', 'partial', 'unavailable', 'refused', 'failed')),
    -- {answer, claims, tables, charts, assumptions, limitations, follow_ups}
    output          jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(output) = 'object'),
    -- [{ref, tool, args, ok, error}]
    tool_calls      jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(tool_calls) = 'array'),
    evidence        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(evidence) = 'array'),
    limitations     text[] NOT NULL DEFAULT '{}',
    -- {claims_checked, numbers_checked, unsupported, regenerated, removed}
    validation      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(validation) = 'object'),
    review_status   text NOT NULL DEFAULT 'unreviewed' CHECK (review_status IN ('unreviewed', 'reviewed', 'flagged')),
    review_note     text NOT NULL DEFAULT '' CHECK (char_length(review_note) <= 1000),
    reviewed_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at     timestamptz,
    model_calls     integer NOT NULL DEFAULT 0 CHECK (model_calls >= 0),
    input_tokens    integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens   integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    -- Estimated from the configured prices; NULL when no model was called.
    cost_usd_micros bigint CHECK (cost_usd_micros IS NULL OR cost_usd_micros >= 0),
    duration_ms     integer NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    error           text NOT NULL DEFAULT '' CHECK (char_length(error) <= 500),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX intelligence_runs_user_idx ON intelligence_runs (user_id, created_at DESC);
CREATE INDEX intelligence_runs_created_idx ON intelligence_runs (created_at);

-- +goose Down

DROP TABLE intelligence_runs;
DROP TABLE intelligence_ai_settings;
