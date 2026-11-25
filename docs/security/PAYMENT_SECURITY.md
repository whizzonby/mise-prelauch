# Payment security

Sprint 9.1. How payments are protected across providers. Architecture: [PAYMENT_ARCHITECTURE.md](../architecture/PAYMENT_ARCHITECTURE.md).
Provider notes: [Stripe](../integrations/STRIPE.md), [PayPal](../integrations/PAYPAL.md), [Scotiabank](../integrations/SCOTIABANK.md).

## Controls

| Control (brief §15) | How | Tested by |
|---|---|---|
| Card data never reaches Mise | Hosted pages only (Stripe Checkout, PayPal approval page). Mise stores opaque references (`cus_…|pm_…`), never a card number or CVV. No card field exists in any Mise form. | Adapter tests; code review (no PAN/CVV fields in schema or API). |
| Server-authoritative amounts | Amount and currency come from the bill; the browser sends only the bill id and the chosen provider. | `payment_providers_test.go` (checkout), Sprint 3 commerce tests. |
| Provider amount must match | A provider-reported amount or currency that differs from the checkout is recorded, flagged `needs_attention` for finance, and **not** applied: no activation, no paid bill. | `TestProviderAmountMismatchIsHeld`. |
| Idempotency keys | Every provider call that creates or moves money carries a key (`Idempotency-Key`, `PayPal-Request-Id`): checkout, capture, charge, refund, expire. Webhooks are stored once per (provider, event id). | `stripe_test.go`, `paypal_test.go` (headers asserted); Sprint 3 duplicate-webhook tests. |
| Unique provider references | `payments.provider_ref` and checkout `provider_ref` are unique per provider. | Schema (Sprint 3), reconciliation tests. |
| Verified webhooks | Stripe: HMAC-SHA256 of `t.body`, constant time, 5-minute tolerance. PayPal: postback verification with the webhook id, byte-exact event. testpay: HMAC. Unverifiable → `400`, nothing stored. Scotiabank: always refused. | `TestParseWebhook` (both adapters: wrong secret, stale, future, missing parts, tampered body, missing headers, verification outage); `TestCheckoutWithChosenProvider` (bad signature refused). |
| Replay protection | Stripe timestamp window; event ids stored once (a replayed event does nothing); statuses only move forward. | Replay case in `TestCheckoutWithChosenProvider`; Sprint 3 out-of-order tests. |
| Event content not trusted | A webhook only identifies the object; the status is fetched from the provider. | Adapter design; server tests use fake provider state. |
| Browser return is not proof | The return page asks the provider (throttled); the outcome comes from the provider. | `TestReturnPageAsksTheProvider`. |
| Transactional state changes | Status, history, audit and outbox event are written in one transaction with a row lock on the payment. | Sprint 3 tests; `TestCommerceEventsArePublishedOnceWithTheChange`. |
| Concurrent attempts / no duplicate charges across providers | One open checkout per bill; switching provider first settles the original attempt with its own provider; pending/processing elsewhere blocks (`409`); renewals lock the bill and skip if anything succeeded or is in flight. | `TestCheckoutWithChosenProvider`; Sprint 9 concurrency tool (`e2e/tools/concurrency.mjs`). |
| No silent cross-provider retry | An unknown outcome (timeout, 5xx) is an error retried with the same provider and key; another provider is refused until it is resolved (`409 payment_status_unknown`). | Adapter tests (5xx → error, not failure); `settleOtherAttempts`. |
| Two billers can never charge one cycle | `billing_authority` per subscription; the scheduler charges only `mise` + `automatic`, checked at queue and charge time; provider-run subscriptions are not implemented. | Commerce renewal tests; schema constraint `subscriptions_billing_ref`. |
| Refund authorisation | `payments.refund` permission; partial refunds bounded by what remains; refund keys idempotent. | Sprint 3/4 refund tests. |
| Reconciliation | Webhooks, return-page checks, expiry reconciliation and the admin view of webhook failures and last reconciliation per provider. | Sprint 3 reconciliation tests; provider health on the admin page. |
| Secret management | Credentials only from the environment (secret store in production); validated at start-up; live keys refused outside production; test-only API bases refused in production; never stored in the database; masked reference only in the admin. | `TestPaymentProviderConfigurationIsChecked`; `TestPaymentOptionsAndProviderSettings` (no secret in admin JSON). |
| Production activation needs approval | Live mode needs `payment_providers.activate`, live credentials, a verified merchant and a written reference; recorded with who and when; append-only audit trail. | `TestPaymentOptionsAndProviderSettings` (test keys refused, events immutable, live without approval refused by the database). |
| Redacted logs | Logs carry ids, provider names, statuses and error codes only: no amounts with card data (none exist), no tokens, no webhook bodies, no credentials. Access logs record paths, not bodies. | Code review of `slog` calls in `internal/payments`. |
| No floating-point money | Integer minor units everywhere; PayPal decimal strings converted exactly; FX rate stored as `numeric(20,10)` text from the provider. | `TestMoneyConversionIsExact`. |

## Residual risks and open items

| Risk | Status |
|---|---|
| Adapters have not met the real providers (sandbox) | **Open** until credentials exist; each provider document has a checklist. |
| Stripe fees, settlement and FX not fetched | Low; columns exist; finance reads them in the Stripe Dashboard meanwhile. |
| Disputes are not acted on automatically | Medium for operations; events are stored and visible; handled in provider dashboards (deferred). |
| PayPal approval-link relation not confirmed | Low; both documented relations are accepted; confirm in the sandbox. |
| In-process throttles (return-page checks) are per API instance | Low; they protect provider rate limits, not correctness. |
| Consent wording for saved-card renewals (`consent_ref`) | Business/legal: needed before live recurring charges. |
