-- Queries for payments (sqlc). Provider-agnostic: provider names and their
-- references are opaque strings here.

-- ---------------------------------------------------------------- checkout sessions

-- name: OpenCheckoutForBillingCycle :one
SELECT * FROM checkout_sessions WHERE billing_cycle_id = @billing_cycle_id AND status = 'open' FOR UPDATE;

-- name: CreateCheckoutSession :one
INSERT INTO checkout_sessions (customer_id, billing_cycle_id, provider, amount_minor, currency, expires_at)
VALUES (@customer_id, @billing_cycle_id, @provider, @amount_minor, @currency, @expires_at)
RETURNING *;

-- name: SetCheckoutProviderRef :exec
UPDATE checkout_sessions SET provider_ref = @provider_ref, redirect_url = @redirect_url, expires_at = @expires_at, updated_at = now()
WHERE id = @id;

-- name: SetCheckoutStatus :exec
UPDATE checkout_sessions SET status = @status,
    completed_at = CASE WHEN @status::text IN ('completed', 'failed') THEN coalesce(completed_at, now()) ELSE completed_at END,
    updated_at = now()
WHERE id = @id AND status = 'open';

-- name: GetCheckoutSession :one
SELECT * FROM checkout_sessions WHERE id = @id;

-- name: GetCheckoutSessionForCustomer :one
SELECT * FROM checkout_sessions WHERE id = @id AND customer_id = @customer_id;

-- name: CheckoutByProviderRef :one
SELECT * FROM checkout_sessions WHERE provider = @provider AND provider_ref = @provider_ref FOR UPDATE;

-- name: ExpiredOpenCheckouts :many
SELECT id FROM checkout_sessions WHERE status = 'open' AND expires_at <= @now::timestamptz ORDER BY expires_at LIMIT 100;

-- name: CancelOpenCheckouts :exec
UPDATE checkout_sessions SET status = 'cancelled', updated_at = now()
WHERE billing_cycle_id = @billing_cycle_id AND status = 'open';

-- ---------------------------------------------------------------- payments

-- name: InsertPayment :one
INSERT INTO payments (customer_id, billing_cycle_id, checkout_session_id, payment_method_id, provider, provider_ref,
                      kind, amount_minor, currency, status, idempotency_key)
VALUES (@customer_id, @billing_cycle_id, @checkout_session_id, @payment_method_id, @provider, @provider_ref,
        @kind, @amount_minor, @currency, @status, @idempotency_key)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: PaymentByIdempotencyKey :one
SELECT * FROM payments WHERE idempotency_key = @idempotency_key FOR UPDATE;

-- name: PaymentByProviderRef :one
SELECT * FROM payments WHERE provider = @provider AND provider_ref = @provider_ref FOR UPDATE;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = @id;

-- name: GetPaymentForUpdate :one
SELECT * FROM payments WHERE id = @id FOR UPDATE;

-- name: UpdatePayment :one
UPDATE payments SET status = @status, provider_ref = coalesce(provider_ref, sqlc.narg(provider_ref)),
    payment_method_id = coalesce(sqlc.narg(payment_method_id), payment_method_id),
    failure_code = sqlc.narg(failure_code), failure_message = sqlc.narg(failure_message),
    succeeded_at = CASE WHEN @status::text = 'succeeded' THEN coalesce(succeeded_at, now()) ELSE succeeded_at END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: FlagPayment :exec
UPDATE payments SET needs_attention = true, attention_reason = @reason, updated_at = now() WHERE id = @id;

-- name: SucceededPaymentForBillingCycle :one
SELECT * FROM payments WHERE billing_cycle_id = @billing_cycle_id AND status IN ('succeeded', 'refunded', 'partially_refunded') LIMIT 1;

-- name: AddPaymentHistory :exec
INSERT INTO payment_status_history (payment_id, from_status, to_status, source, provider_event_id)
VALUES (@payment_id, @from_status, @to_status, @source, @provider_event_id);

-- name: PaymentHistory :many
SELECT * FROM payment_status_history WHERE payment_id = @payment_id ORDER BY id;

-- name: ListPaymentsForCustomer :many
SELECT * FROM payments WHERE customer_id = @customer_id ORDER BY created_at DESC LIMIT 100;

-- name: ListPayments :many
SELECT * FROM payments
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (NOT @attention_only::bool OR needs_attention)
  AND (sqlc.narg(customer_id)::uuid IS NULL OR customer_id = sqlc.narg(customer_id)::uuid)
ORDER BY created_at DESC
LIMIT 100;

-- name: AddRefunded :one
UPDATE payments SET refunded_minor = refunded_minor + @amount_minor,
    status = CASE WHEN refunded_minor + @amount_minor >= amount_minor THEN 'refunded' ELSE 'partially_refunded' END,
    updated_at = now()
WHERE id = @id AND refunded_minor + @amount_minor <= amount_minor
RETURNING *;

-- ---------------------------------------------------------------- payment methods

-- name: UpsertPaymentMethod :one
INSERT INTO payment_methods (customer_id, provider, provider_ref, label)
VALUES (@customer_id, @provider, @provider_ref, @label)
ON CONFLICT (provider, provider_ref) DO UPDATE SET label = excluded.label, status = 'active', updated_at = now()
RETURNING *;

-- name: GetPaymentMethod :one
SELECT * FROM payment_methods WHERE id = @id;

-- ---------------------------------------------------------------- webhooks

-- name: InsertWebhookEvent :one
INSERT INTO payment_webhook_events (provider, event_id, event_type, ref_kind, provider_ref, payload)
VALUES (@provider, @event_id, @event_type, @ref_kind, @provider_ref, @payload)
ON CONFLICT (provider, event_id) DO NOTHING
RETURNING id;

-- name: GetWebhookEvent :one
SELECT * FROM payment_webhook_events WHERE id = @id FOR UPDATE;

-- name: SetWebhookEventStatus :exec
UPDATE payment_webhook_events SET status = @status, error = sqlc.narg(error), attempts = attempts + 1,
    processed_at = CASE WHEN @status::text IN ('processed', 'ignored') THEN now() ELSE processed_at END
WHERE id = @id;

-- name: ListWebhookEvents :many
SELECT id, provider, event_id, event_type, provider_ref, status, error, attempts, received_at, processed_at
FROM payment_webhook_events
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
ORDER BY received_at DESC
LIMIT 100;

-- ---------------------------------------------------------------- refunds

-- name: InsertRefund :one
INSERT INTO refunds (payment_id, amount_minor, currency, reason, idempotency_key, requested_by)
VALUES (@payment_id, @amount_minor, @currency, @reason, @idempotency_key, @requested_by)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: RefundByIdempotencyKey :one
SELECT * FROM refunds WHERE idempotency_key = @idempotency_key;

-- name: SetRefundResult :one
UPDATE refunds SET status = @status, provider_ref = sqlc.narg(provider_ref), completed_at = now()
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: RefundsForPayment :many
SELECT * FROM refunds WHERE payment_id = @payment_id ORDER BY created_at;

-- name: PendingRefundTotal :one
SELECT coalesce(sum(amount_minor), 0)::bigint FROM refunds WHERE payment_id = @payment_id AND status = 'pending';
