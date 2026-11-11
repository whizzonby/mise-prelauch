-- Queries for billing: billing cycles, invoices, promotions (sqlc).

-- ---------------------------------------------------------------- billing cycles

-- name: CreateBillingCycle :one
INSERT INTO billing_cycles (subscription_id, subscription_cycle_id, customer_id, kind, period_start, period_end, due_at,
                            currency, subtotal_minor, discount_minor, delivery_fee_minor, tax_minor, total_minor,
                            tax_snapshot, discount_snapshot, promotion_id, status)
VALUES (@subscription_id, @subscription_cycle_id, @customer_id, @kind, @period_start, @period_end, @due_at,
        @currency, @subtotal_minor, @discount_minor, @delivery_fee_minor, @tax_minor, @total_minor,
        @tax_snapshot, @discount_snapshot, @promotion_id, @status)
ON CONFLICT (subscription_cycle_id) DO NOTHING
RETURNING *;

-- name: GetBillingCycle :one
SELECT * FROM billing_cycles WHERE id = @id;

-- name: GetBillingCycleForUpdate :one
SELECT * FROM billing_cycles WHERE id = @id FOR UPDATE;

-- name: BillingCycleForCycle :one
SELECT * FROM billing_cycles WHERE subscription_cycle_id = @subscription_cycle_id;

-- name: SetBillingStatus :one
UPDATE billing_cycles SET
    status = @status,
    attempts = attempts + CASE WHEN @count_attempt::bool THEN 1 ELSE 0 END,
    next_attempt_at = sqlc.narg(next_attempt_at),
    paid_at = CASE WHEN @status::text = 'paid' THEN @now::timestamptz ELSE paid_at END,
    voided_at = CASE WHEN @status::text = 'void' THEN @now::timestamptz ELSE voided_at END,
    void_reason = CASE WHEN @status::text = 'void' THEN sqlc.narg(void_reason) ELSE void_reason END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: MoveBillingCycle :exec
-- Before an initial payment completes, its week can move (the cutoff of the
-- planned first week passed while the customer was paying).
UPDATE billing_cycles SET subscription_cycle_id = @subscription_cycle_id, period_start = @period_start,
    period_end = @period_end, updated_at = now()
WHERE id = @id AND status IN ('pending', 'due', 'failed');

-- name: RetryDueBillingCycles :many
SELECT id FROM billing_cycles
WHERE status = 'failed' AND next_attempt_at IS NOT NULL AND next_attempt_at <= @now::timestamptz
ORDER BY next_attempt_at LIMIT 100;

-- name: ListBillingCyclesForCustomer :many
SELECT b.*, i.id AS invoice_id, i.number AS invoice_number
FROM billing_cycles b LEFT JOIN invoices i ON i.billing_cycle_id = b.id
WHERE b.customer_id = @customer_id AND b.status <> 'pending'
ORDER BY b.period_start DESC, b.created_at DESC
LIMIT 52;

-- name: ListBillingCyclesForSubscription :many
SELECT * FROM billing_cycles WHERE subscription_id = @subscription_id ORDER BY period_start DESC, created_at DESC;

-- ---------------------------------------------------------------- invoices

-- name: NextInvoiceNumber :one
INSERT INTO invoice_counters (year, last) VALUES (@year, 1)
ON CONFLICT (year) DO UPDATE SET last = invoice_counters.last + 1
RETURNING last;

-- name: CreateInvoice :one
INSERT INTO invoices (number, customer_id, subscription_id, billing_cycle_id, status, currency,
                      subtotal_minor, discount_minor, delivery_fee_minor, tax_minor, total_minor,
                      period_start, period_end, issued_at, due_at, paid_at,
                      customer_snapshot, business_snapshot, tax_snapshot)
VALUES (@number, @customer_id, @subscription_id, @billing_cycle_id, @status, @currency,
        @subtotal_minor, @discount_minor, @delivery_fee_minor, @tax_minor, @total_minor,
        @period_start, @period_end, @issued_at, @due_at, sqlc.narg(paid_at),
        @customer_snapshot, @business_snapshot, @tax_snapshot)
RETURNING *;

-- name: AddInvoiceItem :exec
INSERT INTO invoice_items (invoice_id, position, kind, description, quantity, unit_amount_minor, amount_minor)
VALUES (@invoice_id, @position, @kind, @description, @quantity, @unit_amount_minor, @amount_minor);

-- name: InvoiceForBillingCycle :one
SELECT * FROM invoices WHERE billing_cycle_id = @billing_cycle_id;

-- name: SetInvoiceStatus :exec
UPDATE invoices SET status = @status,
    paid_at = CASE WHEN @status::text = 'paid' THEN @now::timestamptz ELSE paid_at END,
    voided_at = CASE WHEN @status::text = 'void' THEN @now::timestamptz ELSE voided_at END
WHERE id = @id;

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id = @id;

-- name: GetInvoiceForCustomer :one
SELECT * FROM invoices WHERE id = @id AND customer_id = @customer_id;

-- name: InvoiceItems :many
SELECT * FROM invoice_items WHERE invoice_id = @invoice_id ORDER BY position;

-- name: ListInvoicesForCustomer :many
SELECT * FROM invoices WHERE customer_id = @customer_id ORDER BY issued_at DESC LIMIT @lim OFFSET @off;

-- ---------------------------------------------------------------- promotions

-- name: PromotionByCode :one
SELECT * FROM promotions WHERE code = @code;

-- name: GetPromotion :one
SELECT * FROM promotions WHERE id = @id;

-- name: ListPromotions :many
SELECT * FROM promotions ORDER BY created_at DESC LIMIT 200;

-- name: CreatePromotion :one
INSERT INTO promotions (code, name, kind, percent_bps, amount_minor, currency, cycles, max_redemptions,
                        valid_from, valid_until, plan_ids, created_by)
VALUES (@code, @name, @kind, @percent_bps, @amount_minor, @currency, @cycles, @max_redemptions,
        @valid_from, @valid_until, @plan_ids, @actor)
RETURNING *;

-- name: SetPromotionStatus :one
UPDATE promotions SET status = @status,
    activated_by = CASE WHEN @status::text = 'active' THEN sqlc.narg(actor) ELSE activated_by END,
    activated_at = CASE WHEN @status::text = 'active' THEN now() ELSE activated_at END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: TakeRedemption :one
-- Counts a use, unless the promotion has run out. Concurrent checkouts are
-- serialised by the row lock the UPDATE takes.
UPDATE promotions SET redemptions = redemptions + 1, updated_at = now()
WHERE id = @id AND status = 'active' AND (max_redemptions IS NULL OR redemptions < max_redemptions)
RETURNING redemptions;

-- name: GiveBackRedemption :exec
UPDATE promotions SET redemptions = greatest(redemptions - 1, 0), updated_at = now() WHERE id = @id;

-- name: CreateRedemption :one
INSERT INTO promotion_redemptions (promotion_id, subscription_id, customer_id, cycles_total)
VALUES (@promotion_id, @subscription_id, @customer_id, @cycles_total)
RETURNING *;

-- name: RedemptionForSubscription :one
SELECT r.*, p.code, p.name, p.kind, p.percent_bps, p.amount_minor, p.currency
FROM promotion_redemptions r JOIN promotions p ON p.id = r.promotion_id
WHERE r.subscription_id = @subscription_id;

-- name: CustomerUsedPromotion :one
SELECT EXISTS (SELECT 1 FROM promotion_redemptions
               WHERE promotion_id = @promotion_id AND customer_id = @customer_id AND status <> 'released');

-- name: UseRedemptionCycle :exec
UPDATE promotion_redemptions SET cycles_used = least(cycles_used + 1, cycles_total), status = 'redeemed', updated_at = now()
WHERE subscription_id = @subscription_id AND status <> 'released';

-- name: ReleaseRedemption :one
UPDATE promotion_redemptions SET status = 'released', updated_at = now()
WHERE subscription_id = @subscription_id AND status = 'reserved'
RETURNING promotion_id;
