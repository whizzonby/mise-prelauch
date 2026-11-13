import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createPlan, snapshotDemand } from "../../kitchen-actions";

export const metadata: Metadata = { title: "Production plans" };

type Search = { week?: string; status?: string };

/**
 * Confirmed demand for a delivery week, and the plans made from it. Only
 * orders count: carts, draft selections and unpaid weeks never do.
 */
export default async function PlansPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const sp = await searchParams;
  const [demand, plans] = await Promise.all([api.kitchen.demand(token, sp.week), api.kitchen.plans(token, sp.status)]);
  const plan = can(admin, "production.plan");
  const existing = plans.items.find((p) => p.week === demand.week && p.status !== "cancelled");
  return (
    <>
      <PageHeader title="Production plans" description="Demand is snapshotted after the selection cutoff, so a plan never moves under the kitchen. If orders change later, a new snapshot revision is raised as an exception; the plan is not changed silently." />
      <div className="grid gap-6">
        <Panel title={`Demand for the week of ${formatDay(demand.week)}`} note={demand.definition}>
          <FilterBar>
            <Labelled label="Delivery week (Monday)">
              <TextInput type="date" name="week" defaultValue={demand.week} />
            </Labelled>
          </FilterBar>
          <p className="type-body-sm mb-3">
            {demand.final ? <StatusTag status="finalized" label="Final" /> : <StatusTag status="scheduled" label="Selection still open" />} Selection closes {formatDateTime(demand.cutoff_at)} ·{" "}
            <strong>{demand.kits}</strong> kits in <strong>{demand.orders}</strong> orders
            {demand.snapshot ? (
              <>
                {" "}
                · snapshot revision {demand.snapshot.revision} ({demand.snapshot.kits} kits, {formatDateTime(demand.snapshot.created_at)})
              </>
            ) : null}
          </p>
          {demand.changed ? <Notice tone="warning" title="Orders changed since the last snapshot" items={["Take a new snapshot to see the difference. Existing plans are not changed automatically."]} /> : null}
          {demand.meals.length ? (
            <DataTable head={["Meal", "Kits", "Servings per kit", "Orders", "Recipe used", "Customer allergies", "Notes"]} minWidth="52rem">
              {demand.meals.map((m) => (
                <tr key={m.meal_id}>
                  <td className="p-3 font-semibold">{m.meal_name}</td>
                  <td className="p-3">{m.kits}</td>
                  <td className="p-3">{m.servings_per_kit}</td>
                  <td className="p-3">{m.orders}</td>
                  <td className="p-3">{m.recipe_name ? `${m.recipe_name} v${m.recipe_version}` : <StatusTag status="failed" label="None published" />}</td>
                  <td className="p-3">
                    {Object.entries(m.customer_allergies).length
                      ? Object.entries(m.customer_allergies)
                          .map(([k, n]) => `${k} (${n})`)
                          .join(", ")
                      : "—"}
                  </td>
                  <td className="p-3">{m.notes.join(" ")}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No confirmed orders for this week.</EmptyState>
          )}
          {plan && demand.final ? (
            <div className="mt-5 flex flex-wrap items-start gap-6">
              <KForm action={snapshotDemand} submit="Take snapshot" pending="Taking…" label="Snapshot demand" variant="secondary" inline>
                <input type="hidden" name="week" value={demand.week} />
              </KForm>
              {existing ? (
                <Link href={`/production/plans/${existing.id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
                  Open plan {existing.number}
                </Link>
              ) : demand.kits > 0 ? (
                <KForm action={createPlan} submit="Create draft plan" pending="Creating…" label="Create a plan" inline>
                  <input type="hidden" name="week" value={demand.week} />
                  <Labelled label="Production date" hint="Blank: the first proposed preparation day.">
                    <TextInput type="date" name="production_date" />
                  </Labelled>
                </KForm>
              ) : null}
            </div>
          ) : null}
        </Panel>
        <Panel title="Plans">
          {plans.items.length ? (
            <DataTable head={["Plan", "Delivery week", "Production date", "Kits (planned / ordered)", "Status", "Snapshot"]} minWidth="44rem">
              {plans.items.map((p) => (
                <tr key={p.id}>
                  <td className="p-3">
                    <Link href={`/production/plans/${p.id}`} className="font-semibold underline underline-offset-4">
                      {p.number}
                    </Link>
                  </td>
                  <td className="p-3">{formatDay(p.week)}</td>
                  <td className="p-3">{formatDay(p.production_date)}</td>
                  <td className="p-3">
                    {p.planned_kits} / {p.demand_kits}
                  </td>
                  <td className="p-3">
                    <StatusTag status={p.status} />
                  </td>
                  <td className="p-3">Revision {p.snapshot_revision}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No production plans yet.</EmptyState>
          )}
        </Panel>
      </div>
    </>
  );
}
