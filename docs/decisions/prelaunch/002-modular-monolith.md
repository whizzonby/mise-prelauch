# ADR-002: A modular monolith

Status: accepted

## Context

The platform will grow to cover ordering, payments, inventory, procurement and more. The
brief rules out microservices.

## Decision

One Go module, `services/api`, with one package per business area under `internal/`:
`leads`, `referrals`, `events`, `notifications`, `admin`, and shared plumbing under
`internal/platform`. It builds three binaries from the same code: `api` (HTTP), `worker`
(background jobs) and `misectl` (operator commands).

Rules that keep the modules separable:

- A module owns its tables. Other modules read them through that module's exported
  functions. (One deliberate exception: `referrals` reads `leads.referral_code` directly
  to resolve a code, to avoid an import cycle.)
- Dependencies point one way: `notifications` and `admin` depend on `leads`; `leads`
  depends on `referrals`; nothing depends on `admin`.
- Modules talk to each other across time through the job queue, not through shared state.

## Deviations from the brief's suggested layout

- **`services/worker` is not a separate module.** The worker uses every domain package the
  API uses. A second module would need a third, shared one, and version management between
  them, for no gain. It is a second binary instead.
- **There is no `newsletter` module.** A waitlist lead who has given marketing consent *is*
  the newsletter subscriber. A module will be added when there is a second audience.

## Consequences

One deployable codebase and one database transaction scope, which is what makes "create
the lead and queue its email atomically" trivial. If a module ever needs to be split out,
its package boundary and its tables are already the seam.
