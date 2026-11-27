# Financial metric definitions

Sprint 9.2. Every management figure, its formula, its sources and when it is withheld. Computed in Go (`internal/finance/summary.go`,
`internal/finance/scenario`), never by AI. Currency TTD; amounts in minor units (cents) internally.

## 1. Summary lines (`GET /api/v1/admin/finance/summary?from&to`)

Periods are inclusive local dates (America/Port_of_Spain), at most two years.

| Key | Definition | Source | Coverage |
|---|---|---|---|
| gross_collections | Sum of succeeded **real** payments (provider ≠ testpay), TTD | `payments.amount_minor`, `succeeded_at` in range | complete |
| refunds | Sum of succeeded refunds of real payments | `refunds.amount_minor`, `completed_at` in range | complete |
| net_collections | gross_collections − refunds | — | complete |
| provider_fees | Fees providers reported per real payment | `payments.fee_minor` (fee currency TTD) | partial when some real payments have no fee |
| ingredient_consumption | Issue movements × lot unit cost | `inventory_movements` (issue) × `inventory_lots.unit_cost_minor` | partial when a moved lot has no cost |
| ingredient_waste | Waste movements × lot unit cost | `inventory_movements` (waste) × lot cost | as above |
| delivery_costs | Actual and adjustment delivery costs by service date | `delivery_cost_records` | partial when a delivered delivery has no cost |

Test-provider payments are reported separately (`test_payments`, `test_collections`) and never enter any line. Records in other
currencies are excluded and counted (`excluded_other_currency_records`). A refund in the period may relate to a payment collected
earlier.

## 2. Results

| Key | Formula | Withheld when |
|---|---|---|
| net_collections | gross_collections − refunds | never |
| estimated_gross_margin | net_collections − ingredient consumption − waste − packaging | consumption/waste partial, or packaging missing/partial for any month |
| contribution_margin | net_collections − provider fees − consumption − waste − delivery − variable expense categories (packaging, transportation, payment processing fees) | any of those lines partial, or any variable category missing/partial |
| estimated_operating_result | contribution_margin − labour − marketing − fixed expense categories | contribution withheld, or any labour/marketing/fixed category missing/partial |
| recognised_revenue | — | always (no approved revenue recognition policy) |

A withheld result has `amount_minor = null`, `withheld = true` and the reasons. `estimated = true` when estimates (recurring or
estimated expenses) are included. Ownership rules that prevent double counting are in `MANAGEMENT_FINANCE.md` §1.

**Cash view**: inflows (settlements where reported, else collections, with the basis named), outflows (approved expenses with a
payment date in the period), the latest approved recorded balance; `complete = false` while supplier payments are not recorded.

## 3. Scenario engine (`internal/finance/scenario`, engine `scenario-v1`)

Inputs (each labelled observed, preset, assumption or what-if):

| Input | Unit | Observed from (last complete calendar months, default 3) |
|---|---|---|
| active_subscriptions | count | live subscriptions (active, pause or cancel scheduled, past due) with a real paid billing cycle |
| new_subscriptions_per_week | count | subscriptions whose first real paid billing cycle starts in the window ÷ weeks |
| weekly_churn_bps | basis points | real subscriptions cancelled in the window ÷ weeks ÷ today's live subscriptions |
| renewal_success_bps | basis points | succeeded ÷ (succeeded + failed) real off-session charges |
| weekly_value_minor | minor | real paid orders' (total − tax) ÷ orders |
| price_change_bps, ingredient_cost_change_bps | basis points | never observed: 0 (no change) unless entered |
| ingredient_cost_per_kit_minor | minor | consumption (if complete) ÷ all paid kits (test orders used real ingredients) |
| packaging_cost_per_kit_minor | minor | packaging category (if covered) ÷ all paid kits |
| delivery_cost_per_kit_minor | minor | delivery costs (if complete) ÷ delivered deliveries |
| provider_fee_bps | basis points | reported fees ÷ real gross collections (if complete) |
| fixed_costs_monthly_minor, labour_monthly_minor, marketing_monthly_minor | minor | the class's categories ÷ months, only when every category is covered every month |
| capacity_kits_per_week (optional) | count | never observed (not recorded in the platform) |
| opening_cash_minor (optional) | minor | latest approved cash balance |

Anything the records cannot support is `null` with the reason. **Nothing is projected while a required input is missing.**

Weekly step `w` (1 … ⌈days/7⌉): paid kits = round(active × renewal); sales = kits × value × (1 + price change); fees = sales × fee
rate; ingredients = kits × ingredient cost × (1 + change); packaging, delivery = kits × cost; contribution = sales − variable costs;
fixed = round(F × w) − round(F × (w−1)) with F = (fixed + labour + marketing) × 12 / 52 (drift-free); operating result = contribution −
fixed − one-off investments in that week; churned = round(active × churn); next active = active − churned + new. Arithmetic is exact
(`math/big`); each weekly money figure is rounded half away from zero to a cent and totals are sums of the rounded weeks.

**Break-even subscriptions** = ⌈F ÷ (renewal × (value × (1 + price change) × (1 − fee rate) − ingredients × (1 + change) − packaging −
delivery))⌉; none when the denominator is not positive. One-off investments are excluded.

**Cash**: shown only with an opening balance: opening + cumulative operating result per week (assumes cash moves in the week it is
earned or spent; no payment terms, settlement delays, tax or financing). Without one, only the peak cumulative loss (cash need).

**Presets**: base = observed values; conservative = new × 3/4, churn × 5/4, renewal − 2 points, ingredient cost + 5 %; optimistic =
new × 5/4, churn × 3/4, renewal + 1 point (capped at 100 %). These are illustrative engineering multipliers shown as assumptions:
**not probabilities and not calibrated prediction intervals**. **What-ifs** (price increase, customer growth, churn change, ingredient
cost, hire, kitchen expansion, delivery expansion, new meal kit, marketing, supplier change) are assumption deltas, refused when they
touch a missing input.

**Reproducibility**: the input hash is SHA-256 over the engine version, horizon, input values and investments; identical inputs give
identical output, and a stored version re-run from its stored inputs equals its stored output (checked on every version view).
