# PayPal

**Status: implemented, not verified against PayPal.** No sandbox credentials exist (owner decision, 25 Nov 2026). The adapter was
built to PayPal's developer documentation (developer.paypal.com, read 25 November 2026) and is tested against a local server that
follows those documented requests and responses. **PayPal does not support TTD**, so it is never offered for Mise's TTD bills.

Code: `services/api/internal/payments/paypal/paypal.go`, tests `paypal_test.go`. Architecture:
[PAYMENT_ARCHITECTURE.md](../architecture/PAYMENT_ARCHITECTURE.md).

## Provider facts (from official sources, 25 Nov 2026)

| Topic | What we found | Consequence for Mise |
|---|---|---|
| Transaction currencies | PayPal's list of 24 currency codes: AUD, BRL, CAD, CNY, CZK, DKK, EUR, HKD, HUF, ILS, JPY, MYR, MXN, TWD, NZD, NOK, PHP, PLN, GBP, SGD, SEK, CHF, THB, USD. **TTD is not on it.** BRL, CNY and MYR are for in-country accounts only. | PayPal can only charge a non-TTD currency. That needs an approved currency and disclosure policy, which does not exist; until then PayPal is not offered. `PAYPAL_CURRENCIES` refuses codes not on the list. |
| Merchant jurisdiction | Not checked for Trinidad and Tobago business accounts. | To confirm with PayPal during onboarding. |
| Settlement currencies | Account-dependent (holding currencies, conversion by PayPal). | Unknown; recorded only from PayPal's capture breakdown. |
| Payment methods | PayPal wallet (and cards through PayPal's page, depending on the account). | Offered as "PayPal". |
| Recurring | Saving a PayPal account for later charges (vaulting) needs separate PayPal approval. PayPal Subscriptions would make PayPal bill renewals. | Neither is used: PayPal pays one week at a time (`OffSessionCharges=false`). Renewal would be manual and needs business approval. |
| Refunds | `POST /v2/payments/captures/{id}/refund`. | Supported. |
| Disputes | Raised as `CUSTOMER.DISPUTE.*` events and handled in PayPal's Resolution Center. | Verified events are stored and listed in the admin webhooks view; no automatic action (deferred). |
| Webhooks | Verified by postback: `POST /v1/notifications/verify-webhook-signature` with the five `PAYPAL-*` transmission headers, the webhook id and the event, answered `verification_status: SUCCESS`. | Implemented. The event is posted back **byte for byte** as received (re-encoding it would make verification fail; a bug found and fixed in testing). |
| Tokenisation | No card data reaches Mise; no vault token is kept. | — |
| Idempotency | `PayPal-Request-Id` header. | `mise-order-<checkout>`, `mise-capture-<order>`, refund keys. |
| Onboarding | PayPal business account, REST app (client id and secret), webhook registration. | Business action. |

## What the adapter does

| Operation | PayPal call | Notes |
|---|---|---|
| Access token | `POST /v1/oauth2/token` (Basic client id/secret, `grant_type=client_credentials`) | Cached until a minute before expiry. Bad credentials fail clearly. |
| Start checkout | `POST /v2/checkout/orders` (`intent=CAPTURE`, one purchase unit, reference and custom id, `user_action=PAY_NOW`, no shipping) | Returns the approval link (`payer-action`, or `approve`; both documented forms accepted — to confirm in the sandbox). |
| Check a checkout | `GET /v2/checkout/orders/{id}`; if `APPROVED`, `POST …/capture` (idempotent) | **Money moves at capture.** The capture happens when Mise checks the order: on PayPal's webhook, on the customer's return page, or at expiry. `422 INSTRUMENT_DECLINED` → failed (the buyer can pick another funding source); a 5xx is an unknown outcome, retried later with the same request id. |
| Check a payment | `GET /v2/payments/captures/{id}` | Reconciliation. |
| Refund | `POST /v2/payments/captures/{id}/refund` | `PENDING` is not final; `422` refusals are reported to staff. |
| Renewal charge | — | Not available (no vault). |
| Money | Decimal strings ↔ integer minor units, exactly; zero-decimal HUF, JPY, TWD. | Fees, net amount and exchange rate from `seller_receivable_breakdown` are recorded on the payment. |

## Sandbox verification checklist (when credentials exist)

1. Create a sandbox REST app; set `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET`, `PAYPAL_ENV=sandbox`.
2. Register a webhook for `https://<api>/api/v1/webhooks/payments/paypal` with `CHECKOUT.ORDER.APPROVED`,
   `PAYMENT.CAPTURE.COMPLETED`, `PAYMENT.CAPTURE.DENIED`, `PAYMENT.CAPTURE.REFUNDED`; set `PAYPAL_WEBHOOK_ID`.
3. Agree a currency for the test (for example USD) and set `PAYPAL_CURRENCIES`; create a test plan priced in it.
4. Enable PayPal in test mode; pay with a sandbox buyer; confirm the capture, the activation and the fee breakdown.
5. Cancel on PayPal's page; use a sandbox buyer whose funding is declined; let an order expire.
6. Refund fully and partially; resend a webhook from the dashboard (no double effect); confirm the approval link relation used.
7. Record evidence and move PayPal to **Verified (sandbox)**. Live use still needs the currency and manual-renewal decisions.
