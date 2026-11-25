# Stripe

**Status: implemented, not verified against Stripe.** No test-mode keys exist (owner decision, 25 Nov 2026), and a Stripe account
needs a business entity in a Stripe-supported country. The adapter was built to Stripe's public documentation (docs.stripe.com,
read 25 November 2026) and is tested against a local server that follows those documented requests and responses.

Code: `services/api/internal/payments/stripe/stripe.go`, tests `stripe_test.go`; server tests `internal/server/payment_providers_test.go`.
Architecture: [PAYMENT_ARCHITECTURE.md](../architecture/PAYMENT_ARCHITECTURE.md).

## Provider facts (from official sources, 25 Nov 2026)

| Topic | What we found | Consequence for Mise |
|---|---|---|
| Merchant jurisdiction | **Trinidad and Tobago is not on Stripe's list of supported countries** (stripe.com/global). | External blocker: the business needs an entity (and bank account) in a supported country, or Stripe cannot be used. |
| Transaction currencies | Stripe lists many presentment currencies; whether **TTD** is accepted could not be confirmed from the published table. | The adapter offers only currencies set in `STRIPE_CURRENCIES` after Stripe confirms them for the account. |
| Settlement currencies | Depend on the account's country and bank account. | Unknown until an account exists. Settlement figures are recorded only when Stripe reports them. |
| Payment methods | Cards through Stripe Checkout (hosted page). Other methods depend on the account and are not enabled by Mise. | Card only. |
| Recurring | Off-session PaymentIntents on a card saved with `setup_future_usage=off_session`. Stripe Billing (subscriptions) exists but is not used. | Mise-managed renewals (§6 of the architecture). |
| Refunds | `POST /v1/refunds` (full or partial, by PaymentIntent). | Supported. |
| Disputes | Raised as `charge.dispute.*` events and handled in the Stripe Dashboard. | Verified dispute events are stored and listed in the admin webhooks view; Mise takes no automatic action (deferred). |
| Webhooks | `Stripe-Signature: t=…,v1=…`; HMAC-SHA256 over `t + "." + body` with the endpoint secret; Stripe's libraries use a 5-minute tolerance. | Implemented exactly, constant-time comparison, several `v1` signatures accepted (secret rotation). |
| Tokenisation | Card data stays with Stripe; Mise stores the customer id and payment method id. | Stored as `cus_…|pm_…` in `payment_methods.provider_ref`. |
| Idempotency | `Idempotency-Key` header on POST requests. | Sent on every create, charge, refund and expire call. |
| Onboarding | Stripe account with business verification in a supported country. | Business action. |

## What the adapter does

| Operation | Stripe call | Notes |
|---|---|---|
| Start checkout | `POST /v1/checkout/sessions` (`mode=payment`, one line item, `client_reference_id`, `metadata[mise_checkout]`, `payment_intent_data[setup_future_usage]=off_session` + `customer_creation=always` when the method is saved) | Idempotency key `mise-checkout-<checkout id>`. Amount from the bill. |
| Check a checkout | `GET /v1/checkout/sessions/{id}?expand[]=payment_intent&expand[]=payment_intent.payment_method` | Maps the PaymentIntent: succeeded → succeeded; processing → processing; requires_action → requires action; canceled → cancelled; `requires_payment_method` with an error → failed; expired session without a payment → cancelled. |
| Check a payment | `GET /v1/payment_intents/{id}?expand[]=payment_method` | Used by reconciliation. |
| Renewal charge | `POST /v1/payment_intents` with `off_session=true`, `confirm=true`, customer and payment method | `authentication_required` and declines are failures; the customer then pays that week through checkout. A network or 5xx error is an unknown outcome, retried with the same key, never through another provider. |
| Refund | `POST /v1/refunds` | `pending` is not final (retried with the same key); `failed`/`canceled` are reported to staff. |
| Close a checkout | `POST /v1/checkout/sessions/{id}/expire` | Used before a different provider is tried; "already complete/expired" is not an error. |
| Webhooks | Signature check, then the session or PaymentIntent id is read; the status is fetched from Stripe. | Event types that name neither are stored and not acted on. |

Not yet: fetching the balance transaction (fees, settlement amount, exchange rate). Columns exist; Stripe values stay empty.

## Sandbox verification checklist (when test keys exist)

1. Set `STRIPE_SECRET_KEY=sk_test_…`, create a webhook endpoint for `https://<api>/api/v1/webhooks/payments/stripe` with
   `checkout.session.completed`, `checkout.session.expired`, `payment_intent.succeeded`, `payment_intent.payment_failed`,
   `charge.refunded`; set `STRIPE_WEBHOOK_SECRET`; set `STRIPE_CURRENCIES` to the confirmed currency.
2. Enable Stripe in test mode on `/payments/providers`.
3. Subscribe with test card `4242 4242 4242 4242`; confirm activation and the saved card label.
4. Decline (`4000 0000 0000 0002`), 3-D Secure (`4000 0027 6000 3184`), cancel on the hosted page, let a session expire.
5. Run a renewal with an off-session card that needs authentication (`4000 0027 6000 3184`): expect a failure and a customer checkout.
6. Refund fully and partially; replay a webhook from the Dashboard (no double effect); send one with a wrong secret (refused).
7. Record evidence (screens, event ids) in the Sprint report and move Stripe to **Verified (test mode)**.
