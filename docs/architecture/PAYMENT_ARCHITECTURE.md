# Payment architecture

Sprint 9.1 (25 November 2026). Mise takes payments through several providers behind the one payments module that Sprint 3 built.
There is no second commerce system: subscriptions, billing cycles, orders, invoices and refunds are unchanged, and every provider
plugs into the same checkout, webhook, reconciliation and refund paths.

**Status in one line.** The orchestration, the PayPal and Stripe adapters and the Scotiabank registration are built and tested against
local servers that follow each provider's documented contract. **No real provider has been exercised**: there are no sandbox
credentials (owner decision, 25 Nov 2026), Stripe needs a merchant entity in a supported country, PayPal cannot charge TTD, and
Scotiabank has published no integration specification. Production payments remain blocked (`docs/launch/GO_LIVE_CHECKLIST.md` #10).

Provider details: [PayPal](../integrations/PAYPAL.md) · [Stripe](../integrations/STRIPE.md) · [Scotiabank](../integrations/SCOTIABANK.md).
Security controls: [PAYMENT_SECURITY.md](../security/PAYMENT_SECURITY.md).

## 1. Where the code is

| Path | What it does |
|---|---|
| `services/api/internal/payments/provider.go` | The `Provider` interface, `Capabilities` (currencies, off-session charges, refunds, method), `Readiness`, `MoneyDetail`. |
| `payments/payments.go` | Checkout sessions, the payment ledger, webhooks (stored once per provider event), forward-only statuses, reconciliation, off-session charges, expiry. |
| `payments/providers.go` | The registry: eligibility, `GET /payment-options`, staff settings and live activation (audited), cross-provider attempt rules. |
| `payments/refunds.go` | Refunds (unchanged from Sprint 3; provider-agnostic). |
| `payments/paypal`, `payments/stripe`, `payments/scotiabank`, `payments/testpay` | Adapters. Each has tests against an `httptest` fake of the provider's API. |
| `migrations/00015_payment_providers.sql` | Provider settings and their append-only audit trail; billing ownership on subscriptions; charged/settlement/FX/fee columns on payments; permissions. |
| `platform/config/config.go` (`loadProviders`) | Provider credentials from the environment, validated at start-up. |
| `apps/web/src/components/commerce-bits.tsx` (`PaymentChoice`) | The customer's choice of method at checkout. |
| `apps/admin/src/app/(console)/payments/providers` | The admin page. |

The brief suggested `internal/commerce/payments/...`. The existing module lives at `internal/payments` and owns these concerns already;
moving it would have been churn with no benefit, so the providers are subpackages of it.

## 2. Capabilities, not assumptions

Every adapter declares what it can do; nothing is assumed from another provider.

| | testpay (simulator) | Stripe | PayPal | Scotiabank |
|---|---|---|---|---|
| Moves real money | Never | With live keys, in production | With `PAYPAL_ENV=live`, in production | Never (blocked) |
| Currencies | TTD, USD | Only `STRIPE_CURRENCIES` (confirmed with Stripe for the account) | Only `PAYPAL_CURRENCIES`, each validated against PayPal's list; **TTD is rejected** | None |
| Hosted checkout | Simulator page | Stripe Checkout | PayPal approval page (Orders v2) | — |
| Saved method and later charges (Mise-managed renewals) | Yes | Yes (off-session PaymentIntent) | **No** (vaulting needs separate PayPal approval) | — |
| Refunds | Yes | Yes | Yes (capture refunds) | — |
| Webhook verification | HMAC (local secret) | `Stripe-Signature` HMAC-SHA256, 5-minute tolerance | Postback to `verify-webhook-signature` | — |
| Status | Local only; refused in production | Implemented, **not verified** | Implemented, **not verified** | **Blocked** |

## 3. Which providers a customer sees

A provider is offered for a bill only when **all** of these hold (`eligible` in `providers.go`):

1. its adapter is registered and **ready** (credentials present and well formed; Scotiabank is never ready);
2. staff have **enabled** it (`payment_provider_settings.enabled`, `payment_providers.manage`);
3. it can charge the bill's **exact currency** for this merchant account (no conversion);
4. its **mode matches its credentials**: test credentials only in test mode, live credentials only in live mode, live mode only with a
   recorded approval, and in production only live providers.

`GET /api/v1/payment-options?currency=TTD` returns exactly that list, in registration order (testpay, Stripe, PayPal, Scotiabank).
Checkout (`POST /me/subscriptions`, `POST /checkout/sessions`) takes an optional `provider` and checks it again on the server: an
ineligible or unknown provider is refused with `422 payment_method_unavailable`; none named means the first eligible one.

The customer app shows the list as a "Pay with" choice only when there is more than one method; with one, nothing extra is shown. A
method that cannot be charged automatically later is labelled "You'll pay each week yourself" (see §6).

**Card routing.** If Stripe and Scotiabank were both enabled for the same currency, the customer would see both, each labelled with its
processor. No automatic routing policy exists; the business must choose one (or a rule) before enabling two card processors.

## 4. Payment flow

1. The API creates a checkout session with the chosen provider (amount, currency and description come from the bill, never from the
   browser) and returns the provider's hosted-page URL. Card data is entered only on the provider's page.
2. The customer pays (or cancels) on the provider's page and returns to `/subscribe/result`.
3. The outcome is taken **from the provider**, never from the browser's return:
   - by a **signed webhook** (`POST /api/v1/webhooks/payments/{provider}`), stored once per provider event id, then the status is
     fetched from the provider (the event body is not trusted beyond identifying the object);
   - by the **return page**: while the customer waits, polling `GET /checkout/sessions/{id}` asks the provider directly (at most every
     5 seconds per checkout). This matters for PayPal, where the money moves only when Mise captures the approved order;
   - by **expiry reconciliation**: before an open checkout expires, the provider is asked once more, so a last-moment payment is
     recorded rather than lost.
4. The provider's amount and currency must equal the checkout's. A mismatch is recorded on the payment and **flagged for finance**
   (`needs_attention`); the bill is not paid and the subscription does not activate.
5. Payment status only moves forward (`advance`), so late or duplicate events cannot undo a later state. Each status change writes
   history, an audit entry and one outbox event (`payment.succeeded`, …) for the existing analytics and commerce consumers.

States the customer can see: pending (still on the provider's page or processing), succeeded, failed (declined, authentication
failed), cancelled, expired. "Try again" opens a new checkout for the same bill.

## 5. One attempt at a time, across providers

- One open checkout per bill. Starting a checkout with a different provider first **asks the original provider**: if the customer
  has paid there, that payment is recorded; if not, the original checkout is closed at the provider (Stripe `expire`, testpay cancel).
- A payment that is pending or processing at another provider **blocks** a new attempt (`409 payment_in_progress`); if the original
  provider cannot be reached, the attempt is refused (`409 payment_status_unknown`). Nothing is ever retried through a different
  provider after a timeout.
- A renewal charge takes a row lock on the bill and does nothing if the bill already has a payment that succeeded or is in flight
  (any provider, any attempt). Idempotency keys make retries of the same attempt safe at the provider.

## 6. Who bills renewals

Mise's commerce module stays the owner of subscriptions and weekly cycles. Each subscription records:

| Column | Meaning |
|---|---|
| `billing_provider` | The provider whose saved method pays renewals (set when the first payment saves a method). |
| `billing_authority` | `mise` (Mise charges an authorised saved method — the only model implemented), `provider` (a provider-run subscription — **not implemented**), `manual` (the customer pays each week through checkout — only if the business approves it). |
| `external_subscription_ref` | Required exactly when the authority is `provider`; none today. |
| `payment_method_id` / `payment_methods.provider_ref` | The saved method (an opaque provider token). |
| `renewal_status` | `automatic`, `manual`, `requires_action`, `disabled`. |
| `consent_ref` | Where the customer's authorisation to charge later is recorded (to be filled when consent wording is approved). |

The scheduler charges a renewal only when `billing_authority = mise` **and** `renewal_status = automatic`, checked when the job is
queued and again at charge time. PayPal Subscriptions and Stripe Billing are deliberately not used: either would let a second system
charge a cycle Mise also bills. A subscription never moves to another provider automatically; changing the method is a new checkout
the customer authorises.

**PayPal cannot be charged later** (no vaulting approval), so a PayPal customer would have to pay each week through checkout. That is a
`manual` renewal and needs an approved, clearly disclosed manual-renewal policy before PayPal is enabled for subscriptions.

## 7. Money

- All amounts are integer minor units. The PayPal adapter converts to and from PayPal's decimal strings exactly (no floats; zero-decimal
  currencies HUF, JPY, TWD handled).
- A bill has one currency (TTD today). **Mise never converts**: a provider that cannot charge the bill's currency is not offered.
- Each payment can carry what the provider reports: `charged_currency/amount`, `settlement_currency/amount`, `fx_rate` (numeric
  20,10) with `fx_source` and `fx_at` (written together), `fee_minor/currency`. Filled only from provider data (PayPal's
  `seller_receivable_breakdown` today; Stripe's balance transaction is not fetched yet). Refunds keep their own amount and currency.
- The customer sees the exact amount and currency before paying (the API's quote on the review page; the provider's page shows the same).

## 8. Administration

`/payments/providers` (admin) shows every provider: whether it is taking payments and why not, mode, credentials (masked reference
only), currencies, recurring capability, refunds, merchant status and reference, live approval, last successful payment, last
reconciliation, failures in 24 hours, webhooks received and failed, and the last ten audited changes.

| Action | Permission | Rules |
|---|---|---|
| View | `payment_providers.read` (super_admin, admin, finance) | Secrets are never returned. |
| Enable / disable, merchant status and reference | `payment_providers.manage` (super_admin, admin) | Enabling is refused while the adapter is not ready. |
| Approve live mode / return to test | `payment_providers.activate` (super_admin) | Live needs live credentials, a verified merchant and a written approval reference; records who and when. |

Every change writes `payment_provider_events` (append-only, protected by a trigger) and the general audit log. The **Launch** page's
`payment_provider` gate asks the registry when it is checked and passes only when a live provider is taking payments.

## 9. Configuration

Credentials come only from the environment (filled from the secret store in production); the database never holds them.

| Variable | Notes |
|---|---|
| `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_CURRENCIES` | Key must be `sk_test_`/`sk_live_`/`rk_`; the webhook secret is required with a key; `sk_live_` only in production. |
| `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET`, `PAYPAL_ENV`, `PAYPAL_WEBHOOK_ID`, `PAYPAL_CURRENCIES` | Both id and secret or neither; webhook id required; `live` only in production; currencies checked against PayPal's list. |
| `SCOTIABANK_MERCHANT_ID` | Registration only. |
| `STRIPE_API_BASE`, `PAYPAL_API_BASE` | Tests only; refused in production. |
| `PAYMENTS_TEST_PROVIDER` | The simulator; refused in production. |

## 10. What is needed to go live with a provider

1. A merchant account the business holds (and, for Stripe, an entity in a supported country).
2. Sandbox/test credentials → set them, enable the provider in test mode, run the sandbox checklist in the provider's document.
3. The currency decision (TTD or an approved other currency with disclosure) and, for PayPal, a manual-renewal policy.
4. Live credentials in the secret store, the merchant marked verified, then live approval by a super admin with a written reference.
