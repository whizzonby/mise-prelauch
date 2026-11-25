# Scotiabank (Scotia eCom+ Hosted Payments)

**Status: Blocked.** Scotiabank has not published a merchant integration specification for Scotia eCom+ Hosted Payments (endpoints,
authentication, callback verification, refund and status APIs). Without one, any integration would be guesswork with real money, so
Mise registers the provider, shows it to staff as blocked with the reason, and processes nothing.

Code: `services/api/internal/payments/scotiabank/scotiabank.go`, test `scotiabank_test.go`.

## What we found (25 Nov 2026)

| Topic | Finding |
|---|---|
| Product | Scotia eCom+ is Scotiabank Trinidad and Tobago's online payment product for merchants; "Hosted Payments" sends the buyer to a bank-hosted payment page. |
| Onboarding | By application to Scotiabank merchant services (email/branch). Approval and documentation are given to the merchant. |
| Public specification | **None found**: no endpoints, request formats, signing method or webhook/callback documentation are public. |
| Currencies | Not stated publicly (TTD is expected for a local product; to confirm). |
| Recurring | Not stated. |
| Refunds | The public FAQ mentions refunds for terminal (card-present) payments only; online refunds are not described. |
| Webhooks / callbacks | Not described publicly. |
| Disputes | Not described publicly. |

## What exists in Mise

- A registered adapter that reports **not ready** with the reason, declares no currencies, no saved methods and no refunds, and returns
  a "blocked" error from every operation. Webhooks to `/api/v1/webhooks/payments/scotiabank` are always refused.
- `SCOTIABANK_MERCHANT_ID` can be set; the admin page shows only that it is set, never the value.
- Staff cannot enable it (`409 provider_not_ready`), so customers never see it.
- Tests prove every operation refuses and nothing claims a capability.

## What is needed

1. The merchant application and approval (business).
2. Scotiabank's integration guide and test environment: payment page request format, signing/verification method, the result
   callback and how to confirm a payment server-side, refunds, transaction status lookup, supported currencies, idempotency.
3. Then: implement the adapter to that guide, test it against a fake server built from the guide and against Scotiabank's test
   environment, and decide the card routing policy if Stripe is also enabled.
