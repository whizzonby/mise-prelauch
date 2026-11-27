# Management finance

Sprint 9.2 (`internal/finance`, migration 00017, admin page `/finance/expenses`). A **lightweight** record of the operating expenses
the platform does not otherwise hold, so that management figures can be complete. It is **not an accounting system** and its figures
are **not audited financial statements**: no ledger, no double entry, no tax, no revenue recognition (there is no approved policy, so
recognised revenue is reported as not calculated).

## 1. What is recorded here, and what is not

| Comes from its own records (never entered here) | Entered here |
|---|---|
| Customer payments, refunds, settlements (`payments`, `refunds`) | Salaries and wages |
| Payment provider fees reported per payment (`payments.fee_minor`) | Kitchen rent, utilities, insurance, equipment maintenance, professional services, other operating |
| Ingredient consumption and waste (inventory movements × lot unit cost) | Packaging (no other record exists) |
| Ingredient purchases (goods receipts) | Marketing |
| Delivery costs (`delivery_cost_records`) | Transportation other than delivery (delivery partner costs are on deliveries) |
| | Payment processing fees a provider does **not** report per payment (e.g. a monthly gateway fee) |

The category descriptions in the console repeat these rules, so a cost is never counted twice.

## 2. Expenses

Fields: category, description, amount (minor units) and currency (TTD; other currencies are stored but excluded from TTD figures and
counted as excluded), kind (**actual** or **estimated**), expense date, payment date (if paid), supporting reference, status, source
(manual or a recurring schedule). Every change is an append-only `finance_expense_events` row and an audit-log entry.

Workflow: **draft → submitted → approved** (or **rejected**); any non-void expense can be **voided** with a reason. Only approved
expenses count. Approving needs `finance_expenses.approve`; a person may not approve an expense they recorded, except a super admin.
An approved expense cannot be edited, only voided. Rejecting and voiding need a note.

## 3. Recurring costs

A recurring cost (weekly, monthly, quarterly, yearly; start and optional end) counts once **approved**. For each month it is due it
contributes an **estimate**, until an actual expense for that category and month replaces it: never both.

## 4. Coverage

A category with nothing recorded for a month is **missing**, not zero. A person with `finance_expenses.manage` can confirm that a
category had **nothing to record** for a month (audited); that is the only way a missing category counts as zero. Coverage per
category: complete, estimated (only estimates for some months), partial or missing. Profit-type results are withheld while any needed
category is missing or partial (`FINANCIAL_METRIC_DEFINITIONS.md`).

## 5. Cash balances

Approved opening balances by date and account, entered from bank statements by `finance_expenses.approve` holders. There is no bank
feed. Collected customer payments are **not** bank cash: the cash view says which basis it uses (settlements where providers report
them, otherwise collections labelled "not settlement") and is marked incomplete while supplier payments are not recorded.

## 6. Permissions

| Permission | super_admin | admin | finance |
|---|---|---|---|
| finance_expenses.read | ✓ | ✓ | ✓ |
| finance_expenses.manage (record, edit drafts, submit, void, recurring, coverage) | ✓ | ✓ | ✓ |
| finance_expenses.approve (approve, reject, approve recurring, cash balances) | ✓ | – | ✓ |

## 7. Where it is shown

`/finance/expenses` (expenses, recurring costs, coverage, cash balances, categories; expense detail with its history), the command
center's Finance tab (the summary with coverage and the cash view) and, for AI, the `financial_summary` and `expense_breakdown` tools.
