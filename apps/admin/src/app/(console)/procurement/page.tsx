import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Panel } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createRun } from "../supply-actions";

export const metadata: Metadata = { title: "Procurement planning" };

/**
 * One procurement run per released production plan: its requirements as
 * released, the stock reserved for it, what is ordered, and what is short.
 */
export default async function ProcurementPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "procurement.read")) notFound();
  const [runs, pending] = await Promise.all([api.supply.runs(token), api.supply.pendingPlans(token)]);
  return (
    <>
      <PageHeader
        title="Procurement planning"
        description="Released production plans arrive here on their own (from the plan-released event). Purchasing works only from released plans: never from carts, unpaid weeks or unapproved plans."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6">
        {pending.items.length ? (
          <Panel title="Released plans without a run" note="Normally picked up automatically within a minute or two. Create the run by hand if one has waited longer.">
            <ul className="grid gap-3">
              {pending.items.map((p) => (
                <li key={p.id} className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
                  <span className="type-body-sm">
                    <strong>Plan {p.number}</strong> · delivery week of {formatDay(p.week)} · production {formatDay(p.production_date)}
                  </span>
                  {can(admin, "procurement.plan") ? (
                    <KForm action={createRun} submit="Create the run" pending="Creating…" label={`Create run for ${p.number}`} variant="secondary" inline>
                      <input type="hidden" name="plan_id" value={p.id} />
                    </KForm>
                  ) : null}
                </li>
              ))}
            </ul>
          </Panel>
        ) : null}
        <Panel title="Runs">
          {runs.items.length ? (
            <DataTable head={["Run", "Plan", "Needed by", "Ingredients", "Short or incomplete", "Undecided", "Status"]} minWidth="48rem">
              {runs.items.map((r) => (
                <tr key={r.id}>
                  <td className="p-3">
                    <Link href={`/procurement/${r.id}`} className="font-semibold underline underline-offset-4">
                      {r.number}
                    </Link>
                    {r.review_reason ? <span className="type-caption block text-muted">Reopened: {r.review_reason}</span> : null}
                  </td>
                  <td className="p-3">
                    {r.plan_number}
                    <span className="type-caption block text-muted">week of {formatDay(r.week)}</span>
                  </td>
                  <td className="p-3">{formatDay(r.required_by)}</td>
                  <td className="p-3">{r.lines}</td>
                  <td className="p-3">{r.shortages ? <strong className="text-error">{r.shortages}</strong> : "0"}</td>
                  <td className="p-3">{r.pending}</td>
                  <td className="p-3">
                    <StatusTag status={r.status === "open" ? "in_progress" : r.status} label={r.status === "open" ? "Open" : undefined} />
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No runs yet. They appear when production plans are released.</EmptyState>
          )}
        </Panel>
      </div>
    </>
  );
}
