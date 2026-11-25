# Performance, load and concurrency results (Sprint 9)

Measured 25 November 2026 on the developer PC. **These are local measurements, not production capacity figures.** The load
generator, browser, Next.js servers, API, worker and PostgreSQL all share one laptop, so throughput is bounded by that machine and
the numbers say nothing about AWS instance sizes. No load was run against any production or third-party system.

## Environment

| | |
|---|---|
| Machine | Intel Core i7-8850H (6 cores / 12 threads), 15.8 GB RAM, Windows 10 |
| Containers | Docker Desktop (WSL 2), 12 CPUs / 7.6 GB available; API and worker images built from the Sprint 9 tree; PostgreSQL 17 |
| Next.js apps | production builds (`next build` + `next start`) on the host |
| Data | development database: 6 published demo meals, 206 subscriptions, 109 orders, 1,111 domain events, every sprint's test data |
| Settings | `RATE_LIMITS_DISABLED=true` (local only; refused in staging and production) so the limiter does not cap the test |
| Tools | `e2e/tests/performance.spec.ts` (`E2E_PERF=1`), `e2e/tools/load.mjs`, `e2e/tools/concurrency.mjs` |

## 1. Page load (Chromium, median of 5)

Each load in a fresh browser context (empty HTTP cache, same session cookies), 1366×850, local network.

| Page | TTFB ms | DOMContentLoaded ms | Load ms | LCP ms | Transferred kB |
|---|---|---|---|---|---|
| Landing (marketing `/`) | 19 | 62 | 338 | 284 | 603 |
| Catalogue (`/meals`) | 18 | 72 | 191 | 156 | 415 |
| Meal detail | 16 | 49 | 181 | 148 | 275 |
| Plans | 15 | 59 | 175 | 148 | 215 |
| Checkout review (signed in) | 19 | 52 | 181 | 140 | 211 |
| Customer dashboard | 24 | 75 | 241 | 176 | 510 |
| Admin overview | 56 | 87 | 207 | 184 | 211 |
| Admin analytics overview | 84 | 110 | 246 | 264 | 212 |
| Admin orders | 31 | 58 | 172 | 200 | 206 |
| Admin production | 27 | 56 | 178 | 164 | 206 |

No network throttling was applied. Real-world mobile timings on cellular networks will be much slower; they were **not measured**
(no real devices, no throttled profile in this run).

## 2. HTTP load

Closed-loop virtual users, each requesting the listed URLs round-robin as fast as responses return.

| Scenario | Users | Duration | Requests | Throughput | Errors | p50 | p90 | p95 | p99 | max (ms) |
|---|---|---|---|---|---|---|---|---|---|---|
| Catalogue API: `/meals`, `/plans`, 3 meal pages | 20 | 30 s | 39,252 | 1,308/s | 0 % | 14 | 21 | 25 | 35 | 129 |
| Catalogue API, same | 50 | 30 s | 39,764 | 1,324/s | 0 % | 36 | 48 | 53 | 69 | 266 |
| Customer app `/meals` + `/plans` (server-rendered) | 10 | 20 s | 1,569 | 78/s | 0 % | 122 | 180 | 215 | 266 | 298 |
| Marketing home (server-rendered) | 10 | 20 s | 1,388 | 69/s | 0 % | 131 | 213 | 236 | 287 | 310 |
| Admin analytics: overview, revenue, procurement, delivery | 5 | 20 s | 3,356 | 168/s | 0 % | 30 | 49 | 59 | 80 | 263 |

Resource use during the 50-user catalogue run (`docker stats`): API 227 % CPU (of 1,200 %) and 20 MiB; PostgreSQL 344 % CPU and
272 MiB; worker idle. Throughput did not rise from 20 to 50 users while latency grew roughly in proportion: the laptop as a whole
(load generator included) is saturated around 1,300 requests per second, with PostgreSQL the busiest process.

Analytics refresh under load: `POST /api/v1/admin/analytics/refresh` is accepted asynchronously (202 in 15 ms) and runs on the worker;
dashboards kept answering during the run.

## 3. Concurrency (commerce)

`node e2e/tools/concurrency.mjs` against the local API, run repeatedly after the fixes below. Each run: a new confirmed customer.

| Scenario | Expected | Result (4 consecutive runs) |
|---|---|---|
| 20 simultaneous checkout requests | one live subscription, no server errors, no hung requests | 1 created; the others `409 subscription_exists` (or the replaced attempt `409`); no timeouts |
| Every opened checkout paid 5× at once (second tabs, provider retries) | one payment taken, one invoice, subscription active | 1 succeeded payment, 0 flagged, 1 invoice, `active` (checked in the database) |
| 20 simultaneous meal confirmations for one week | one confirmed choice within the allowance | 20 × 200, 3 kits (= allowance) |

Other concurrency is covered by Go integration tests: two dispatchers assigning to a run with capacity 1 (one wins), five managers
creating a production plan at once (one plan), two clerks recording the same delivery (recorded once), five workers handling the same
batch (one fulfillment obligation per order); duplicate and out-of-order payment webhooks are idempotent (sequential test).

### Defects found by the concurrency runs, and fixed

1. **Requests hung indefinitely under simultaneous saves (High).** Meal confirmation built its response page inside the transaction,
   and checkout checked delivery coverage inside the transaction, each through a *second* pool connection while holding row locks.
   With 20 simultaneous requests every pooled connection waited on the lock and the lock holder waited for a free connection, so
   all requests hung (observed: 19 of 20 meal confirmations and up to 20 of 20 checkouts never answered). Fixed: the selection page is
   read after commit (`commerce/selection.go`), and coverage is checked through the caller's transaction (`commerce.zones`). A scan
   of every transaction closure in `internal/` found no other pool use inside a transaction.
2. **A replaced checkout could still take money (High).** When a customer starts subscribing again (a second tab, a double click),
   the unfinished subscription is replaced and its bill voided. Two gaps let the old checkout still be paid: the provider's hosted
   session stayed open, and `startCheckout` marked the bill "due" outside a transaction, so its row lock did nothing and it could
   overwrite the replacement's "void". Result before the fix: TTD 330.00 taken on a void bill (flagged for a staff refund, never
   silent). Fixed: replaced checkouts are closed at the provider by a queued job (`payments.cancel_checkout`; new optional
   `CheckoutCanceller` provider interface, implemented by the test provider and required of the real one where possible),
   `MarkDue` runs in a transaction, and `StartCheckout` locks the bill and refuses one that can no longer be paid. Regression test:
   `TestReplacedCheckoutCannotBePaidAtTheProvider`.

## Not measured

Production hardware, AWS networking, CDN behaviour, real mobile devices and networks, database performance at production data
volumes, sustained (hours-long) load, and background job throughput beyond the runs above.
