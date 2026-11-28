# Mise app: API integration

The app calls `https://<api host>/api/v1` directly with `Authorization: Bearer <access token>` (`docs/mobile/AUTHENTICATION.md`).
Contract: `docs/api/openapi.yaml` (1.13.0). Conventions: `docs/api/API_STANDARDS.md`.

## Client

`ApiClient` (`lib/core/api/api_client.dart`) wraps Dio: unwraps `{data, meta}`, turns `{error: {code, message, fields}}` and network
failures into `AppError` (safe human messages; server errors never shown verbatim), sends `Idempotency-Key` on money-bearing POSTs,
and logs only method, path and status. Models are hand-written and tolerant of new fields (API v1 changes are additive). Paths used by
the app are all documented in OpenAPI; `TestRoutesMatchOpenAPI` keeps the server and the document in step.

## Endpoints the app uses

| Area | Endpoints |
|---|---|
| Auth | `/auth/mobile/login`, `/auth/mobile/refresh`, `/auth/mobile/logout` *(new)*, `/auth/register`, `/auth/verify-email`, `/auth/password/forgot`, `/auth/password/reset`, `/me`, `/me/sessions[/{id}]` |
| Catalogue | `/meals`, `/meals/{slug}`, `/meal-categories`, `/reference`, `/weekly-menus[/{week}]`, `/me/favorites[/{mealId}]` |
| Commerce | `/plans[/{slug}]`, `/subscription-info`, `/me/subscriptions/quote`, `/me/subscriptions` (+ `client: app`), `/me/subscription`, `/me/subscriptions/{id}/pause|resume|cancel|change-plan|address`, `/me/subscription-cycles`, `/me/meal-selections/{cycleId}[/finalize]` |
| Payments | `/payment-options`, `/checkout/sessions` (+ `client: app`), `/checkout/sessions/{id}` |
| Orders | `/me/orders[/{id}]`, `/me/orders/{id}/delivery`, `/me/orders/{id}/meals/{mealId}/recipe` *(new)* |
| Support | `/chat/availability`, `/me/chat/conversations[...]` incl. `/stream`, `/help/categories`, `/help/articles[/{slug}]` |
| Account | `/me/profile`, `/me/addresses[/{id}]`, `/me/preferences`, `/me/notification-preferences` |
| App only *(new)* | `/me/notifications[/{id}/read | /read-all]`, `/me/devices[/{installationId}]`, `/me/account/deletion-request` |

## Business rules stay on the server

Prices, currency, tax, delivery fees, eligibility, availability, deadlines, allowances, per-meal limits, payment, order and delivery
status, and which subscription actions are allowed (`actions` in the subscription) all come from the API. The app only stops taps the
server would certainly refuse (for example a quantity above the allowance) and always shows the server's answer when it disagrees.
