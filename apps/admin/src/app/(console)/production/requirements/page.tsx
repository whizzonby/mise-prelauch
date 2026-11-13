import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, TextInput } from "@/components/commerce-ui";
import { formatDay, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Ingredient requirements" };

type Search = { plan_id?: string; from?: string; to?: string };

/**
 * What live plans need, per ingredient and preparation, by production date.
 * This is the input Sprint 6 procurement will read; nothing is ordered here.
 */
export default async function RequirementsPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const sp = await searchParams;
  const rows = await api.kitchen.requirements(token, sp);
  const qs = new URLSearchParams(Object.entries(sp).filter(([, v]) => v) as [string, string][]).toString();
  return (
    <>
      <PageHeader
        title="Ingredient requirements"
        description="From approved recipe versions and planned kits, in each ingredient's base unit. No purchase orders or suppliers here: procurement arrives in Sprint 6."
        actions={
          <a href={`/production/requirements/export${qs ? `?${qs}` : ""}`} className="type-body-sm inline-flex min-h-11 items-center rounded-md border border-foreground px-4 font-semibold hover:bg-foreground hover:text-surface">
            Download CSV
          </a>
        }
      />
      <FilterBar>
        {sp.plan_id ? <input type="hidden" name="plan_id" value={sp.plan_id} /> : null}
        <Labelled label="Production from">
          <TextInput type="date" name="from" defaultValue={sp.from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={sp.to} />
        </Labelled>
      </FilterBar>
      {sp.plan_id ? (
        <p className="type-body-sm mb-4">
          One plan only. <Link href="/production/requirements" className="underline underline-offset-4">Show every live plan</Link>
        </p>
      ) : null}
      {rows.items.length ? (
        <DataTable head={["Production date", "Ingredient", "Preparation", "Required", "Status", "Batches", "Meals", "Plan"]} minWidth="60rem">
          {rows.items.map((r, i) => (
            <tr key={i}>
              <td className="p-3">{formatDay(r.production_date)}</td>
              <td className="p-3 font-semibold">{r.ingredient_name}</td>
              <td className="p-3">{r.preparation || "—"}</td>
              <td className="p-3 font-semibold">
                {quantity(r.quantity, r.unit)}
                {r.approximate ? <span className="type-caption block font-normal text-muted">Approximate conversion</span> : null}
              </td>
              <td className="p-3">
                <StatusTag status={r.status} />
              </td>
              <td className="type-caption p-3">{r.source_batches || "Not yet batched"}</td>
              <td className="type-caption p-3">{r.source_meals}</td>
              <td className="p-3">
                <Link href={`/production/plans/${r.plan_id}`} className="underline underline-offset-4">
                  {r.plan_number}
                </Link>{" "}
                <StatusTag status={r.plan_status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No requirements in this range.</EmptyState>
      )}
    </>
  );
}
