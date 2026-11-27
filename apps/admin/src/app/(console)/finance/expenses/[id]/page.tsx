import { ApiError } from "@mise/api-client";
import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { DetailList } from "@/components/panel";
import { formatDate, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { expenseTransition } from "../../../mi-actions";

export const metadata: Metadata = { title: "Expense" };

const NEXT: Record<string, string[]> = { draft: ["submit", "void"], submitted: ["approve", "reject", "void"], approved: ["void"], rejected: ["void"] };

/** One expense with its audit history. Approved expenses cannot be edited, only voided with a reason. */
export default async function ExpensePage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "finance_expenses.read")) notFound();
  const { id } = await params;
  let e;
  try {
    e = await api.finance.expense(token, id);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  }
  const categories = (await api.finance.categories(token)).items;
  const actions = (NEXT[e.status] ?? []).filter((a) => (a === "approve" || a === "reject" ? can(admin, "finance_expenses.approve") : can(admin, "finance_expenses.manage")));
  const history = e.history ?? [];
  return (
    <>
      <PageHeader title={e.description} back={{ href: "/finance/expenses", label: "Expenses" }} />
      <div className="grid gap-5 lg:grid-cols-2">
        <Panel title="Expense">
          <DetailList
            items={[
              ["Status", <Tag key="s" tone={e.status === "approved" ? "success" : e.status === "rejected" ? "error" : "neutral"}>{humanize(e.status)}</Tag>],
              ["Category", categories.find((c) => c.key === e.category)?.name ?? e.category],
              ["Amount", money(e.amount_minor, e.currency)],
              ["Kind", e.kind === "estimated" ? "Estimate" : "Actual"],
              ["Expense date", formatDate(e.expense_date)],
              ["Payment date", e.payment_date ? formatDate(e.payment_date) : "Not paid yet"],
              ["Reference", e.reference || "—"],
              ["Approved", e.approved_at ? formatDateTime(e.approved_at) : "—"],
            ]}
          />
        </Panel>
        {actions.length ? (
          <Panel title="Next step" note="Approving needs a second person (only a super admin may approve their own). Rejecting or voiding needs a reason. Audited.">
            <KForm action={expenseTransition} submit="Apply" pending="Saving…" label="Expense step">
              <input type="hidden" name="id" value={e.id} />
              <Labelled label="Step">
                <SelectInput name="action" defaultValue={actions[0]}>
                  {actions.map((a) => (
                    <option key={a} value={a}>
                      {humanize(a)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Note">
                <TextInput name="note" maxLength={1000} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
      <div className="mt-6">
        <Panel title="History">
          <Timeline
            items={history.map((h) => ({
              at: h.created_at,
              title: `${humanize(h.action)}${h.actor_name ? ` by ${h.actor_name}` : ""}`,
              detail: [h.from_status && h.to_status ? `${humanize(h.from_status)} → ${humanize(h.to_status)}` : "", h.note].filter(Boolean).join(" · "),
            }))}
          />
        </Panel>
      </div>
    </>
  );
}
