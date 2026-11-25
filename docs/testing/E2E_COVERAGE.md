# End-to-end coverage (Sprint 9)

How the brief's required customer journey (§38) and negative paths (§39) are covered, and by which test. Playwright specs live in
`e2e/tests/`; Go integration tests in `services/api/internal/server/` run the real HTTP handlers against PostgreSQL.

No test fakes a transition by writing a status directly. Every state change goes through the public or admin API (in the browser or
through the in-process router), so the same validation, permissions, outbox events and audit entries apply.

## Why the journey is in two halves

A customer's order exists only once the week's menu has closed (the cutoff), and a recipe published today applies only to future
weeks. On the real clock, no single test run can take a brand-new customer from sign-up to a delivered box. So:

- **Front half (steps 1–13, 20)**: one new visitor in one browser session, `journey.spec.ts`, real clock.
- **Back half (steps 14–19)**: orders placed by `misectl production seed-demo`, which registers demo customers, subscribes them, pays
  through the test provider and chooses meals **through the same public API** with the clock moved to just before a future week's
  cutoff, then creates, approves and releases the production plan through the admin API. `misectl delivery seed-demo -ready` completes
  those batches through the admin API. `chef-production.spec.ts` and `delivery-fulfillment.spec.ts` take it from there in the browser.

## §38 required scenario

| # | Step | Covered by |
|---|---|---|
| 1 | Landing page | `journey.spec.ts`; `launch.spec.ts` (site mode, SEO); `experience.spec.ts` |
| 2 | Browse published meal kits | `journey.spec.ts`; `launch.spec.ts` (visitor catalogue); Go `TestCatalogShowsOnlyPublishedMeals` |
| 3 | Meal detail | `journey.spec.ts`; `launch.spec.ts` |
| 4 | Select a plan | `journey.spec.ts` (visitor → sign-in with `next`); `launch.spec.ts` (plan survives registration) |
| 5 | Create an account | `journey.spec.ts` (register, email confirmation from Mailpit, sign-in returns to the plan); `accounts.spec.ts` |
| 6 | Select meals | `journey.spec.ts`; `subscription-commerce.spec.ts` (allowance enforced on screen and server) |
| 7 | Delivery details | `journey.spec.ts` (adds an address from the checkout and returns to it) |
| 8 | Sandbox payment | `journey.spec.ts`, `subscription-commerce.spec.ts` via **testpay** (the simulator; no real provider is approved) |
| 9 | Webhook-confirmed payment | `journey.spec.ts` (result page waits for the worker to process the signed webhook); Go `TestWebhooksAreAuthenticatedIdempotentAndOrderIndependent` |
| 10 | Order creation | Orders are created at the cutoff: Go commerce tests; `misectl production seed-demo`; `journey.spec.ts` checks the honest "No orders yet" state before it |
| 11 | Customer dashboard | `journey.spec.ts`; `customer-platform.spec.ts` |
| 12 | Order details | `delivery-fulfillment.spec.ts` (customer opens the delivered order) |
| 13 | Subscription status | `journey.spec.ts` (Active, pause available); `subscription-commerce.spec.ts` (pause, resume, cancel) |
| 14 | Production demand linkage | `chef-production.spec.ts`; seed: snapshot → plan → approve → release through the admin API |
| 15 | Complete production | `chef-production.spec.ts` (tasks, output, quality hold, packing, completion) |
| 16 | Fulfillment | `delivery-fulfillment.spec.ts` (packer verifies every kit, manager releases) |
| 17 | Dispatch | `delivery-fulfillment.spec.ts` (schedule, run, manifest, release, handoff) |
| 18 | Proof of delivery | `delivery-fulfillment.spec.ts` (courier records proof, completes); Go `TestDispatchCustodyTrackingProofAndCompletion` |
| 19 | Customer-facing completion | `delivery-fulfillment.spec.ts` (customer sees "Delivered" in plain words, no partner/internal data) |
| 20 | Analytics and audit history | `journey.spec.ts` (staff API: customer record, succeeded payment of TTD 330.00 (33000 minor units), activity); `intelligence.spec.ts`; Go commerce-event and analytics reconciliation tests |

## §39 negative paths

| Path | Covered by |
|---|---|
| Unpublished meal | Go `TestCatalogShowsOnlyPublishedMeals`; `customer-platform.spec.ts` (a draft needs a photograph first); `launch.spec.ts` (sitemap lists only the public catalogue) |
| Out-of-stock ingredient | Go `supply_inventory_test.go` (`insufficient_stock`: nothing issued, clear message) |
| Invalid address | `customer-platform.spec.ts` ("Some details need attention"); Go `TestAddressesOwnershipAndDefault` |
| Unsupported delivery zone | Go `TestReleaseSnapshotsTheAddressAndFlagsCoverage`, `TestSchedulingCoverageAndCapacity`; subscribe page disables unserved addresses |
| Invalid discount | Go `TestPromotionsAreApprovedAndComputedOnTheServer`; `TestQuoteDiscountsAndTax` |
| Payment failure | `subscription-commerce.spec.ts` ("a declined first payment can be paid again"); Go `TestFailedFirstPaymentCanBeRetried`, `TestPaymentFailedRuleReactsToTheEvent` |
| Duplicate payment callback | Go `TestWebhooksAreAuthenticatedIdempotentAndOrderIndependent`, `TestCommerceEventsArePublishedOnceWithTheChange`, `TestReplacedCheckoutCannotBePaidAtTheProvider`; `e2e/tools/concurrency.mjs` (every checkout paid 5× at once → one payment) |
| Expired session | Go `TestSessionsEndWhenIdleOrExpired`, `TestPasswordResetEndsEverySession`; `customer-platform.spec.ts` (signed-out visitor sent to sign-in and back) |
| Unauthorized order access | Go `TestCustomersCannotReachEachOthersRecords`, `TestEveryRoleReachesExactlyTheAdminEndpointsItIsGranted`, `TestInvoicePDFAndOwnership` |
| Cancelled subscription | `subscription-commerce.spec.ts`; Go `TestPlanChangePauseResumeAndCancel`, `TestCancellingAnUnpaidWeekTakesEffectAtOnce` |
| Production quality hold | `chef-production.spec.ts`; Go `TestIncompleteProductionAndHoldsNeverMakeAnObligation` |
| Missing fulfillment package | Go `TestVerificationNeedsTheRightKitsAndRecordsDiscrepancies` |
| Failed delivery | `delivery-fulfillment.spec.ts` (failed attempt → exception → quarantined return → reconciled); Go `TestFailedDeliveryExceptionReturnAndResolution` |
| Delivery outcome correction | Go `TestReschedulingCancellationAndCorrections`, `TestCorrectedOutcomeIsADisputeInCommerceAndReports` |
| Missing proof of delivery | Go `delivery_ops_test.go` (`proof_required`: completion refused) |
| Notification failure | Go `TestFailingEmailAndConsumerJobsRetryThenStayVisible` (new: retried with backoff, then failed with the SMTP error, counted on System); `TestDeliveryEmailsAreRecordedHonestly` |
| Analytics consumer failure | Go `TestFailingEmailAndConsumerJobsRetryThenStayVisible` (a panicking consumer job is retried, ends failed and visible; the event replays); `TestSystemStatusShowsWorkerQueuesAndConsumers` |

## Gaps

- The front and back halves use different customers (see above). A single customer through all 20 steps would need a controllable
  clock in the browser stack; not built.
- Payments use the test provider only. No real-provider sandbox exists, so "sandbox payment" means the simulator (`payment_provider`
  gate stays blocked).
- Emails are verified in Mailpit only; no production email provider evidence.
