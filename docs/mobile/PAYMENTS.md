# Mise app: payments

The app reuses the payment orchestration built in Sprints 3 and 9.1. It never sees card details, never computes an amount and never
decides that a payment worked.

## Flow

1. **Options.** `GET /payment-options?currency=TTD` lists only providers that are configured, enabled by staff, able to charge the
   currency and in the right mode. A production build additionally hides any option marked `test` (`Environment.allowsTestPayments`).
2. **Server total.** `POST /me/subscriptions/quote` gives the amounts to show (`due_now`, `renewal`) with currency.
3. **Start.** `POST /me/subscriptions` (first week) or `POST /checkout/sessions` (a due or failed week), with `client: "app"` and an
   `Idempotency-Key` that is the same for every retry of one tap. The server picks the return URL: `MOBILE_PAYMENT_RETURN_URL`
   (e.g. `mise://checkout/return`), never one sent by the app. Unknown `client` values are refused before anything is created; if
   no app return is configured the answer is `503 app_checkout_unavailable` and nothing is created.
4. **Authorize.** The provider's hosted page opens with `flutter_web_auth_2` (ASWebAuthenticationSession on iOS, Custom Tabs on
   Android). The app cannot read the page. Only `https` pages are opened (plain `http` only outside production, for the local test
   provider).
5. **Verify.** Whatever happens in the browser — return, cancel, app killed — the app opens `/checkout/result/<id>` and polls
   `GET /checkout/sessions/{id}` every 2 s for up to 2 minutes. The server asks the provider itself while the customer waits. Outcomes:
   `succeeded` (confirmation), `failed`/`expired` ("nothing was charged", try again), still `pending` after 2 minutes ("we are still
   confirming — do not pay again").

**Duplicates.** The pay button is disabled while busy; the idempotency key makes a repeated request return the first answer; the
server refuses a second checkout while one is unresolved (`409 payment_in_progress` / `payment_status_unknown`) and a second
subscription (`409 subscription_exists`). After an uncertain outcome the app never switches provider on its own.

**Store policy.** Mise sells physical meal kits and delivery, so Apple In-App Purchase and Google Play Billing are not used (Apple App
Review Guideline 3.1.3(e) and Google Play's payments policy exempt physical goods; re-check at submission).

## Providers

| Provider | Server | App | Status |
|---|---|---|---|
| testpay (local test provider) | Built | Hosted test page, return, server verification | **Implemented, not verified on a device** (Go test `TestAppCheckoutReturnsToTheAppAndIsVerifiedServerSide` verifies the server side) |
| PayPal | Adapter (Orders v2), tested against a fake | Same hosted flow (PayPal approval page in the system browser) | **Blocked**: no sandbox credentials; PayPal cannot charge TTD |
| Stripe | Adapter (Checkout Session), tested against a fake | Same hosted flow; native PaymentSheet, Apple Pay, Google Pay not added | **Blocked**: no keys; Trinidad and Tobago is not a Stripe country |
| Scotiabank | Configuration and capability only | — | **Blocked**: no public specification or merchant credentials. No mobile flow was invented. |

Live payments stay off: the server's provider registry requires a super admin's recorded approval to go live (Sprint 9.1).

## Security notes

No card data or provider secrets in the app. Amounts and provider references are recorded by the server. Logs carry method, path and
status only. The return URL is fixed server-side and validated at start-up (`MOBILE_PAYMENT_RETURN_URL` must be
`<scheme>://checkout/return` or https; production must use `mise://` or https).
