# Customer behaviour events (contract v1)

Sprint 9.2. First-party behaviour events from the customer app (`apps/web`),
used by Mise Intelligence for the customer funnel and checkout abandonment.
The marketing site keeps its own taxonomy (`packages/analytics`, `lead_events`).

## Privacy rules

- **Fixed contract.** Only the events below, each with only its listed
  properties. The API refuses anything else: unknown events, extra
  properties, malformed values, other contract versions.
- **Identifiers only.** Properties are slugs, a week date or a small count.
  Never form contents, search text, names, emails, phone numbers,
  addresses, allergy details or card data.
- **Privacy signals.** Nothing is sent when the browser reports Global
  Privacy Control or Do Not Track (`lib/behavior.ts`). The app's relay and
  the API also store nothing if a request carries `Sec-GPC: 1` or `DNT: 1`.
- **No cookies.** A random visitor id lives in `localStorage`, a session id
  in `sessionStorage`. A signed-in customer's events are attributed on the
  server (`/api/behavior` relay), so the session token never reaches the
  browser's tracking code.
- **Deduplicated.** Every event has a sender-chosen UUID (`event_id`); a
  retried event is stored once.
- **Business facts are not re-recorded.** Payments, renewals, pauses,
  cancellations (with the reason the customer chose), deliveries and support
  cases are read from their own records and `domain_events`.

## Events

| Event | When | Properties |
| --- | --- | --- |
| `catalogue_viewed` | The meal catalogue is shown | `category` (slug, optional), `filters` (number of active filters, 0–50) |
| `meal_viewed` | A meal page is shown | `meal` (slug) |
| `plan_viewed` | A plan page is shown | `plan` (slug) |
| `plan_selected` | Subscribe step 1 is shown for a plan | `plan` (slug) |
| `meal_selected` | A week's selection is saved with a meal newly added | `meal` (slug), `week` (YYYY-MM-DD) |
| `checkout_started` | The subscription review (payment) step is shown | `kind` (`subscription` or `week`), `plan` (slug, optional) |

Each is sent as `{ event_id, name, version: 1, properties }` in a batch
`{ anonymous_id, session_id, events[1..20] }` to `POST /api/v1/behavior/events`
(through the app's `/api/behavior` relay).

## Derived intelligence

- **Funnel** (`GET /api/v1/admin/intelligence/funnel`): distinct people per
  step. "Paid" comes from the payment ledger (succeeded checkout payments),
  not from a browser event. Test-provider payments are flagged.
- **Checkout abandonment:** a `checkout_started` by a signed-in customer
  with no successful payment by that customer within 24 hours. Checkouts
  younger than 24 hours are reported as pending, not abandoned.
- **Segments** (`/segments`): rules over orders, payments, deliveries and
  selections, each published with its definition. No sensitive attributes,
  no scores, no predictions. Listing members (names, emails) also needs
  `customers.read`.

| Segment | Rule |
| --- | --- |
| New customers | First paid week within the last 4 weeks |
| Repeat customers | Two or more paid weeks |
| Long-term subscribers | Twelve or more paid weeks |
| Inactive customers | A paid week, none in the last 4 weeks, no live subscription |
| Customers with renewal failures | A renewal charge failed in the last 60 days |
| Customers with repeated delivery issues | Two or more failed or returned deliveries in the last 90 days |
| Prefer *category* | At least 3 kits chosen, half or more in that category |

- **Cancellation reasons** (`/cancellation-reasons`): the reasons customers
  chose. A cancellation without one is counted as "not recorded"; no reason
  is inferred.

## Limitations

- A visitor is counted per browser; one person on two devices counts twice,
  and a visitor who signs in is counted again as a customer.
- Visitors who send GPC or DNT are not counted at all, so funnel counts are
  lower than true traffic.
- Recording started with Sprint 9.2; earlier periods have no behaviour data,
  and the funnel says so.

## Changing the contract

Add an event or property by raising the version: update `Contract` in
`services/api/internal/behavior/behavior.go`, the database check on
`behavior_events.name`, `BehaviorEvents` in `apps/web/src/lib/behavior.ts`,
the OpenAPI `BehaviorEvent` schema and this page, in the same change.
