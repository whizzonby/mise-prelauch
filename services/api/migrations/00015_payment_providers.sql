-- Sprint 9.1: several payment providers behind the one payments module.
--
-- payment_provider_settings: what staff have decided about each provider
-- (enabled for checkout, test or live mode, merchant verification). Secrets
-- are never stored here: they stay in the environment / Secrets Manager.
-- payment_provider_events: append-only audit of every change to a setting.
--
-- subscriptions gain an explicit record of who bills each renewal, so two
-- systems can never charge the same cycle. payments gain the fields needed
-- when the charged or settled amount differs from the order amount; they are
-- filled only from provider data, never by converting money in Mise.

-- +goose Up

CREATE TABLE payment_provider_settings (
    provider          text PRIMARY KEY CHECK (provider ~ '^[a-z][a-z0-9_]{1,30}$'),
    enabled           boolean NOT NULL DEFAULT false,
    mode              text NOT NULL DEFAULT 'test' CHECK (mode IN ('test', 'live')),
    merchant_status   text NOT NULL DEFAULT 'not_started'
                      CHECK (merchant_status IN ('not_started', 'applied', 'verified', 'rejected')),
    merchant_reference text NOT NULL DEFAULT '' CHECK (char_length(merchant_reference) <= 200),
    -- Live mode needs a recorded approval by someone with payment_providers.activate.
    live_approved_by  uuid,
    live_approved_at  timestamptz,
    live_approval_reference text NOT NULL DEFAULT '' CHECK (char_length(live_approval_reference) <= 300),
    updated_by        uuid,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payment_provider_live_approved CHECK (mode = 'test' OR (live_approved_by IS NOT NULL AND live_approved_at IS NOT NULL))
);

-- The simulator is enabled by default so local development keeps working;
-- production refuses it in configuration regardless of this row.
INSERT INTO payment_provider_settings (provider, enabled) VALUES
    ('testpay', true), ('stripe', false), ('paypal', false), ('scotiabank', false);

CREATE TABLE payment_provider_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider    text NOT NULL,
    action      text NOT NULL CHECK (action ~ '^[a-z_]{2,40}$'),
    from_value  text NOT NULL DEFAULT '',
    to_value    text NOT NULL DEFAULT '',
    note        text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    actor_id    uuid,
    request_id  text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_provider_events_provider_idx ON payment_provider_events (provider, id DESC);
CREATE TRIGGER payment_provider_events_immutable BEFORE UPDATE OR DELETE ON payment_provider_events
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- Who bills each renewal. 'mise': Mise charges a saved, authorised payment
-- method (the only model implemented). 'provider': a provider-run
-- subscription (not implemented). 'manual': the customer pays each week
-- through checkout (only if the business approves it).
ALTER TABLE subscriptions
    ADD COLUMN billing_provider text,
    ADD COLUMN billing_authority text NOT NULL DEFAULT 'mise' CHECK (billing_authority IN ('mise', 'provider', 'manual')),
    ADD COLUMN external_subscription_ref text,
    ADD COLUMN renewal_status text NOT NULL DEFAULT 'automatic'
        CHECK (renewal_status IN ('automatic', 'manual', 'requires_action', 'disabled')),
    ADD COLUMN consent_ref text,
    -- A provider-run subscription must name its reference; Mise-billed ones must not have one.
    ADD CONSTRAINT subscriptions_billing_ref CHECK (
        (billing_authority = 'provider') = (external_subscription_ref IS NOT NULL));

-- Existing subscriptions were all billed by Mise through the provider that
-- took their first payment.
UPDATE subscriptions s SET billing_provider = pm.provider
FROM payment_methods pm WHERE pm.id = s.payment_method_id AND s.billing_provider IS NULL;

ALTER TABLE payments
    ADD COLUMN charged_currency text CHECK (charged_currency ~ '^[A-Z]{3}$'),
    ADD COLUMN charged_amount_minor bigint CHECK (charged_amount_minor >= 0),
    ADD COLUMN settlement_currency text CHECK (settlement_currency ~ '^[A-Z]{3}$'),
    ADD COLUMN settlement_amount_minor bigint CHECK (settlement_amount_minor >= 0),
    -- Exact decimal; never a float.
    ADD COLUMN fx_rate numeric(20, 10) CHECK (fx_rate > 0),
    ADD COLUMN fx_source text,
    ADD COLUMN fx_at timestamptz,
    ADD COLUMN fee_minor bigint CHECK (fee_minor >= 0),
    ADD COLUMN fee_currency text CHECK (fee_currency ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT payments_fx_complete CHECK ((fx_rate IS NULL) = (fx_source IS NULL));

INSERT INTO permissions (key, description) VALUES
    ('payment_providers.read',     'View payment providers, their status and health.'),
    ('payment_providers.manage',   'Enable or disable payment providers in test mode and record merchant status.'),
    ('payment_providers.activate', 'Approve a payment provider for live payments.');

INSERT INTO role_permissions (role_key, permission_key)
SELECT r, p FROM (VALUES
    ('super_admin', ARRAY['payment_providers.read', 'payment_providers.manage', 'payment_providers.activate']),
    ('admin',       ARRAY['payment_providers.read', 'payment_providers.manage']),
    ('finance',     ARRAY['payment_providers.read'])
) AS g(r, perms), unnest(perms) AS p;

-- +goose Down

DELETE FROM role_permissions WHERE permission_key LIKE 'payment_providers.%';
DELETE FROM permissions WHERE key LIKE 'payment_providers.%';
ALTER TABLE payments
    DROP CONSTRAINT payments_fx_complete,
    DROP COLUMN fee_currency, DROP COLUMN fee_minor, DROP COLUMN fx_at, DROP COLUMN fx_source, DROP COLUMN fx_rate,
    DROP COLUMN settlement_amount_minor, DROP COLUMN settlement_currency, DROP COLUMN charged_amount_minor, DROP COLUMN charged_currency;
ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_billing_ref,
    DROP COLUMN consent_ref, DROP COLUMN renewal_status, DROP COLUMN external_subscription_ref,
    DROP COLUMN billing_authority, DROP COLUMN billing_provider;
DROP TABLE payment_provider_events;
DROP TABLE payment_provider_settings;
