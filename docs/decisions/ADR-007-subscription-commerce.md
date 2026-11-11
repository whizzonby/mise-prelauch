# ADR-007: Subscription commerce and payments without a chosen provider

Status: accepted, 11 November 2026 (Sprint 3).

## Context

Sprint 3 needed subscriptions, weekly billing and checkout, but no payment provider has
been approved for Mise in Trinidad and Tobago. The brief forbids inventing a provider's
capabilities and forbids real-money processing without approval.

The owner decided on 11 November 2026:
- no provider yet
- prepaid weekly billing
- a selection cutoff of Thursday 23:59 Trinidad and Tobago time
- tax and delivery fees undecided

## Decision

1. **A provider-agnostic `payments.Provider` interface.**
   - The interface covers hosted checkout, status lookup, off-session charge, refund and
     webhook parsing.
   - Each provider declares its real `Capabilities`. Renewals charge a saved method only
     when the provider says it can; otherwise the customer is emailed to pay.
2. **`testpay`, a simulator, is the only implementation.**
   - It has a hosted page (served by the customer app) with buttons instead of card
     fields, signed (HMAC) webhooks, saved test cards that approve or decline, and
     refunds.
   - Configuration refuses it in production. With no other provider, production checkout
     answers 503 "payments unavailable".
3. **The outcome comes from the provider, never the browser.**
   - Webhooks are verified, stored once per event id, and processed by a job.
   - Processing re-reads the payment's status from the provider (reconcile), so events
     that arrive late or out of order are harmless.
   - Payment status only moves forward.
4. **Every lifecycle is separate:** plan price, subscription, cycle (week), billing cycle,
   invoice, payment and order. Each is unique per its natural key, so the scheduler and
   retried jobs never duplicate them.
5. **Workflows live in `commerce`.** They call the owning modules inside one transaction
   and queue emails and charges through the existing job table (outbox). `payments` knows
   nothing about subscriptions; it reports outcomes through `Hooks`.
6. **Unanswered policies are configuration with safe defaults:**
   - no tax and no delivery fee
   - no automatic retries
   - no proration (changes apply from the first week not billed)
   - no automatic refunds
   - a paid week with no confirmed meals becomes `needs_resolution` (no meals are
     invented)
7. **sqlc for every new module**, as ADR-002 requires. The simulator's two tables use
   hand-written SQL, because it plays an external system.
8. **No new Go dependency.** The invoice PDF is a small, deterministic writer using the
   standard PDF fonts.

## Consequences

- **Adding a real provider** means implementing `Provider` against its documented API,
  adding its webhook secret to Secrets Manager, and registering it in
  `server.NewCommerce`. Nothing else changes.
- **Going live** still needs the open decisions (tax, fees, refunds, retries, segments)
  and a PCI scope review of the chosen integration. Hosted checkout keeps card data off
  Mise's systems, but no compliance is claimed.
- **Frontend stack:** the customer app keeps Sprint 2's choices: server actions instead of
  React Hook Form and TanStack Query, and no shadcn layer (ADR-006). Meal selection is the
  one client-heavy screen, and it shows a choice as confirmed only when the server says
  so.
