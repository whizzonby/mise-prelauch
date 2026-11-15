-- +goose Up

-- Sprint 7 review fixes. Additive only.

-- 1. Which dates may deliver an order: an approved rule relative to the
-- order's week (Monday = day 0). Unset: nothing can be scheduled, because
-- delivery days are not decided yet. It is never assumed.
ALTER TABLE delivery_settings
    ADD COLUMN delivery_offset_min_days integer,
    ADD COLUMN delivery_offset_max_days integer,
    ADD COLUMN delivery_week_rule_ref   text NOT NULL DEFAULT '' CHECK (char_length(delivery_week_rule_ref) <= 200),
    ADD CONSTRAINT delivery_settings_week_rule CHECK (
        (delivery_offset_min_days IS NULL AND delivery_offset_max_days IS NULL)
        OR (delivery_offset_min_days BETWEEN 0 AND 27 AND delivery_offset_max_days BETWEEN delivery_offset_min_days AND 27
            AND delivery_week_rule_ref <> ''));

-- The order's week, copied onto the delivery when it is released (the
-- address and the week are fixed for the delivery's life).
ALTER TABLE deliveries ADD COLUMN order_week date;
UPDATE deliveries d SET order_week = o.week_start FROM orders o WHERE o.id = d.order_id;
ALTER TABLE deliveries ALTER COLUMN order_week SET NOT NULL;

-- 2. The commerce side of a corrected delivery outcome. Commerce never
-- rewrites an order's status or payment history; instead it records, append
-- only, that the delivered outcome is disputed (or confirmed again). Order
-- views, customer pages and reports read the latest entry.
CREATE TABLE order_delivery_outcomes (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id      uuid NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    state         text NOT NULL CHECK (state IN ('disputed', 'confirmed')),
    reason        text NOT NULL CHECK (char_length(reason) BETWEEN 3 AND 1000),
    source        text NOT NULL DEFAULT 'delivery' CHECK (source IN ('delivery', 'staff')),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX order_delivery_outcomes_order_idx ON order_delivery_outcomes (order_id, id DESC);
CREATE TRIGGER order_delivery_outcomes_append_only BEFORE UPDATE OR DELETE ON order_delivery_outcomes
    FOR EACH ROW EXECUTE FUNCTION forbid_change();

-- +goose Down

DROP TABLE order_delivery_outcomes;
ALTER TABLE deliveries DROP COLUMN order_week;
ALTER TABLE delivery_settings
    DROP CONSTRAINT delivery_settings_week_rule,
    DROP COLUMN delivery_week_rule_ref,
    DROP COLUMN delivery_offset_max_days,
    DROP COLUMN delivery_offset_min_days;
