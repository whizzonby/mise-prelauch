import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, Tabs } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { validDay } from "@/components/supply-ui";
import { formatDate, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { confirmCoverage, createExpense, createRecurring, recordCashBalance, recurringStatus } from "../../mi-actions";

export const metadata: Metadata = { title: "Expenses" };

type SP = { tab?: string; status?: string; category?: string; from?: string; to?: string };

const statusTone = { draft: "neutral", submitted: "accent", approved: "success", rejected: "error", void: "neutral", ended: "neutral" } as const;

/**
 * Management finance: operating expenses the platform does not otherwise
 * record. Payments, refunds, fees, ingredients and delivery costs come from
 * their own records and are never entered here. Not an accounting system and
 * not audited statements.
 */
export default async function ExpensesPage({ searchParams }: { searchParams: Promise<SP> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "finance_expenses.read")) notFound();
  const sp = await searchParams;
  const tab = ["expenses", "recurring", "coverage", "cash", "categories"].includes(sp.tab ?? "") ? sp.tab! : "expenses";
  const manage = can(admin, "finance_expenses.manage");
  const categories = (await api.finance.categories(token)).items;
  const catName = new Map(categories.map((c) => [c.key, c.name]));
  const catOptions = categories.map((c) => (
    <option key={c.key} value={c.key}>
      {c.name}
    </option>
  ));
  const today = new Date().toISOString().slice(0, 10);
  return (
    <>
      <PageHeader
        title="Expenses"
        description="Operating expenses for management figures. Payment provider fees reported per payment, ingredient costs and delivery costs come from their own records: do not enter them here. Management information, not audited statements."
        actions={
          can(admin, "intelligence_financial.read") ? (
            <Link className="type-body-sm inline-flex min-h-11 items-center rounded-md border border-foreground px-4 font-semibold" href="/intelligence/command-center?tab=finance">
              Financial summary
            </Link>
          ) : undefined
        }
      />
      <Tabs
        tabs={[
          { key: "expenses", label: "Expenses" },
          { key: "recurring", label: "Recurring costs" },
          { key: "coverage", label: "Coverage" },
          { key: "cash", label: "Cash balances" },
          { key: "categories", label: "Categories" },
        ]}
        current={tab}
        base="/finance/expenses"
      />
      <div className="mt-6">
        {tab === "expenses" ? <ExpenseList token={token} sp={sp} catName={catName} catOptions={catOptions} manage={manage} today={today} /> : null}
        {tab === "recurring" ? <Recurring token={token} catName={catName} catOptions={catOptions} manage={manage} approve={can(admin, "finance_expenses.approve")} today={today} /> : null}
        {tab === "coverage" ? <Coverage token={token} catOptions={catOptions} manage={manage} today={today} /> : null}
        {tab === "cash" ? <Cash token={token} approve={can(admin, "finance_expenses.approve")} today={today} /> : null}
        {tab === "categories" ? (
          <DataTable head={["Category", "Cost class", "What belongs here"]} label="Expense categories" minWidth="40rem">
            {categories.map((c) => (
              <tr key={c.key}>
                <td className="p-3 font-semibold">{c.name}</td>
                <td className="p-3">{humanize(c.cost_class)}</td>
                <td className="type-caption p-3 text-muted">{c.description}</td>
              </tr>
            ))}
          </DataTable>
        ) : null}
      </div>
    </>
  );
}

async function ExpenseList({ token, sp, catName, catOptions, manage, today }: { token: string; sp: SP; catName: Map<string, string>; catOptions: React.ReactNode; manage: boolean; today: string }) {
  const list = await api.finance.expenses(token, { status: sp.status || undefined, category: sp.category || undefined, from: validDay(sp.from), to: validDay(sp.to) });
  return (
    <>
      <form className="mb-4 flex flex-wrap items-end gap-3" aria-label="Filter expenses">
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">All</option>
            {["draft", "submitted", "approved", "rejected", "void"].map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Category">
          <SelectInput name="category" defaultValue={sp.category ?? ""}>
            <option value="">All</option>
            {catOptions}
          </SelectInput>
        </Labelled>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={validDay(sp.from)} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={validDay(sp.to)} />
        </Labelled>
        <button className="type-body-sm min-h-11 rounded-md border border-foreground px-4 font-semibold">Filter</button>
      </form>
      {list.items.length ? (
        <DataTable head={["Date", "Category", "Description", "Amount", "Kind", "Status"]} label="Expenses" minWidth="48rem">
          {list.items.map((e) => (
            <tr key={e.id}>
              <td className="p-3">{formatDate(e.expense_date)}</td>
              <td className="p-3">{catName.get(e.category) ?? e.category}</td>
              <td className="p-3">
                <Link className="underline underline-offset-4" href={`/finance/expenses/${e.id}`}>
                  {e.description}
                </Link>
              </td>
              <td className="type-figures p-3">{money(e.amount_minor, e.currency)}</td>
              <td className="p-3">{e.kind === "estimated" ? <Tag tone="accent">Estimate</Tag> : "Actual"}</td>
              <td className="p-3">
                <Tag tone={statusTone[e.status]}>{humanize(e.status)}</Tag>
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No expenses recorded{sp.status || sp.category ? " for this filter" : ""}. Until expenses are recorded or confirmed, profit figures are withheld.</Empty>
      )}
      {manage ? (
        <div className="mt-6">
          <Panel title="Record an expense" note="Saved as a draft; submit it for approval. Only approved expenses count.">
            <KForm action={createExpense} submit="Record expense" pending="Saving…" label="Record an expense" reset>
              <div className="grid items-start gap-4 sm:grid-cols-2 xl:grid-cols-3">
                <Labelled label="Category">
                  <SelectInput name="category" required>
                    {catOptions}
                  </SelectInput>
                </Labelled>
                <Labelled label="Description">
                  <TextInput name="description" maxLength={300} required />
                </Labelled>
                <Labelled label="Amount (TTD)">
                  <TextInput name="amount" inputMode="decimal" required placeholder="0.00" />
                </Labelled>
                <Labelled label="Actual or estimate">
                  <SelectInput name="kind" defaultValue="actual">
                    <option value="actual">Actual</option>
                    <option value="estimated">Estimate</option>
                  </SelectInput>
                </Labelled>
                <Labelled label="Expense date">
                  <TextInput type="date" name="expense_date" defaultValue={today} required />
                </Labelled>
                <Labelled label="Payment date (if paid)">
                  <TextInput type="date" name="payment_date" />
                </Labelled>
              </div>
              <Labelled label="Supporting reference" hint="Invoice or receipt number">
                <TextInput name="reference" maxLength={300} />
              </Labelled>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}

async function Recurring({ token, catName, catOptions, manage, approve, today }: { token: string; catName: Map<string, string>; catOptions: React.ReactNode; manage: boolean; approve: boolean; today: string }) {
  const list = await api.finance.recurring(token);
  return (
    <>
      {list.items.length ? (
        <DataTable head={["Category", "Description", "Amount", "Cadence", "From", "Status", ""]} label="Recurring costs" minWidth="52rem">
          {list.items.map((r) => (
            <tr key={r.id}>
              <td className="p-3">{catName.get(r.category) ?? r.category}</td>
              <td className="p-3">{r.description}</td>
              <td className="type-figures p-3">{money(r.amount_minor, r.currency)}</td>
              <td className="p-3">{humanize(r.cadence)}</td>
              <td className="p-3">
                {formatDate(r.starts_on)}
                {r.ends_on ? ` to ${formatDate(r.ends_on)}` : ""}
              </td>
              <td className="p-3">
                <Tag tone={statusTone[r.status]}>{humanize(r.status)}</Tag>
              </td>
              <td className="p-3">
                {r.status === "draft" && approve ? (
                  <KForm action={recurringStatus} submit="Approve" pending="…" label={`Approve ${r.description}`} variant="secondary" inline>
                    <input type="hidden" name="id" value={r.id} />
                    <input type="hidden" name="action" value="approve" />
                  </KForm>
                ) : r.status === "approved" && manage ? (
                  <KForm action={recurringStatus} submit="End" pending="…" label={`End ${r.description}`} variant="secondary" inline>
                    <input type="hidden" name="id" value={r.id} />
                    <input type="hidden" name="action" value="end" />
                  </KForm>
                ) : null}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No recurring costs.</Empty>
      )}
      <p className="type-caption mt-3 text-muted">An approved recurring cost counts as an estimate for each month until the actual expense for that month replaces it; it is never counted twice.</p>
      {manage ? (
        <div className="mt-6">
          <Panel title="Add a recurring cost">
            <KForm action={createRecurring} submit="Add recurring cost" pending="Saving…" label="Add a recurring cost" reset>
              <div className="grid items-start gap-4 sm:grid-cols-2 xl:grid-cols-3">
                <Labelled label="Category">
                  <SelectInput name="category" required>
                    {catOptions}
                  </SelectInput>
                </Labelled>
                <Labelled label="Description">
                  <TextInput name="description" maxLength={300} required />
                </Labelled>
                <Labelled label="Amount each time (TTD)">
                  <TextInput name="amount" inputMode="decimal" required />
                </Labelled>
                <Labelled label="Cadence">
                  <SelectInput name="cadence" defaultValue="monthly">
                    {["weekly", "monthly", "quarterly", "yearly"].map((c) => (
                      <option key={c} value={c}>
                        {humanize(c)}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Starts on">
                  <TextInput type="date" name="starts_on" defaultValue={today} required />
                </Labelled>
                <Labelled label="Ends on (optional)">
                  <TextInput type="date" name="ends_on" />
                </Labelled>
              </div>
              <Labelled label="Reference (contract, lease)">
                <TextInput name="reference" maxLength={300} />
              </Labelled>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}

async function Coverage({ token, catOptions, manage, today }: { token: string; catOptions: React.ReactNode; manage: boolean; today: string }) {
  const month = today.slice(0, 7);
  const first = `${month}-01`;
  const prevFirst = new Date(Date.UTC(Number(month.slice(0, 4)), Number(month.slice(5, 7)) - 4, 1)).toISOString().slice(0, 10);
  const s = await api.finance.summary(token, prevFirst, new Date(new Date(first).getTime() - 86400000).toISOString().slice(0, 10)).catch(() => null);
  return (
    <>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">
        A category with no expense for a month is missing, not zero, until someone confirms there was nothing to record. Profit and operating results stay withheld while any category is missing.
      </p>
      {s ? (
        <DataTable head={["Category", "Coverage", "Missing months", "Estimated months"]} label="Coverage" minWidth="40rem">
          {s.categories.map((c) => (
            <tr key={c.key}>
              <td className="p-3">{c.name}</td>
              <td className="p-3">
                <Tag tone={c.coverage === "complete" ? "success" : c.coverage === "missing" ? "error" : "warning"}>{humanize(c.coverage)}</Tag>
              </td>
              <td className="type-caption p-3">{c.missing_months.join(", ") || "—"}</td>
              <td className="type-caption p-3">{c.estimate_months.join(", ") || "—"}</td>
            </tr>
          ))}
        </DataTable>
      ) : null}
      <p className="type-caption mt-2 text-muted">The last three complete months.</p>
      {manage ? (
        <div className="mt-6">
          <Panel title="Confirm nothing to record" note="For example: no marketing spend in March. Audited.">
            <KForm action={confirmCoverage} submit="Confirm" pending="Saving…" label="Confirm coverage" reset>
              <div className="grid items-start gap-4 sm:grid-cols-3">
                <Labelled label="Category">
                  <SelectInput name="category" required>
                    {catOptions}
                  </SelectInput>
                </Labelled>
                <Labelled label="Month">
                  <TextInput type="month" name="month" required />
                </Labelled>
                <Labelled label="Note">
                  <TextInput name="note" maxLength={500} />
                </Labelled>
              </div>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}

async function Cash({ token, approve, today }: { token: string; approve: boolean; today: string }) {
  const list = await api.finance.cashBalances(token);
  return (
    <>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">
        Approved balances recorded by finance from bank statements. There is no bank feed; collected customer payments are not cash in the bank.
      </p>
      {list.items.length ? (
        <DataTable head={["As of", "Account", "Balance", "Reference", "Recorded"]} label="Cash balances" minWidth="40rem">
          {list.items.map((b) => (
            <tr key={b.id}>
              <td className="p-3">{formatDate(b.as_of)}</td>
              <td className="p-3">{b.account_label}</td>
              <td className="type-figures p-3">{money(b.amount_minor, b.currency)}</td>
              <td className="type-caption p-3">{b.reference}</td>
              <td className="type-caption p-3">{formatDateTime(b.created_at)}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No balances recorded: cash views show the cash position as not available.</Empty>
      )}
      {approve ? (
        <div className="mt-6">
          <Panel title="Record a balance">
            <KForm action={recordCashBalance} submit="Record balance" pending="Saving…" label="Record a cash balance" reset>
              <div className="grid items-start gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <Labelled label="As of">
                  <TextInput type="date" name="as_of" defaultValue={today} required />
                </Labelled>
                <Labelled label="Account">
                  <TextInput name="account_label" maxLength={100} required placeholder="Operating account" />
                </Labelled>
                <Labelled label="Balance (TTD)">
                  <TextInput name="amount" inputMode="decimal" required />
                </Labelled>
                <Labelled label="Reference" hint="Statement reference">
                  <TextInput name="reference" maxLength={300} required />
                </Labelled>
              </div>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}
