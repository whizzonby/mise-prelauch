import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { startCount } from "../../supply-actions";

export const metadata: Metadata = { title: "Stock counts" };

export default async function CountsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const [counts, locations] = await Promise.all([api.supply.counts(token), can(admin, "inventory.count") ? api.supply.locations(token) : Promise.resolve(null)]);
  return (
    <>
      <PageHeader
        back={{ href: "/inventory", label: "Inventory" }}
        title="Stock counts"
        description="Count a location lot by lot. Each line remembers the ledger balance when it was counted, so stock that moves during the count is kept. Someone other than the counter approves and posts the variances."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_20rem]">
        <Panel title="Counts">
          {counts.items.length ? (
            <DataTable head={["Count", "Location", "Lines", "Started", "Status"]} minWidth="34rem">
              {counts.items.map((c) => (
                <tr key={c.id}>
                  <td className="p-3">
                    <Link href={`/inventory/counts/${c.id}`} className="font-semibold underline underline-offset-4">
                      {c.number}
                    </Link>
                  </td>
                  <td className="p-3">{c.location_name}</td>
                  <td className="p-3">{c.lines.length}</td>
                  <td className="p-3">
                    {formatDateTime(c.created_at)}
                    <span className="type-caption block text-muted">{c.started_by_name}</span>
                  </td>
                  <td className="p-3">
                    <StatusTag status={c.status === "open" ? "in_progress" : c.status === "submitted" ? "pending_approval" : c.status} label={c.status} />
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No counts yet.</EmptyState>
          )}
        </Panel>
        {locations ? (
          <Panel title="Start a count">
            <KForm action={startCount} submit="Start count" pending="Starting…" label="Start a count">
              <Labelled label="Location">
                <SelectInput name="location_id" required defaultValue="">
                  <option value="" disabled>
                    Choose
                  </option>
                  {locations.items
                    .filter((l) => l.active)
                    .map((l) => (
                      <option key={l.id} value={l.id}>
                        {l.name} ({l.lots} lots)
                      </option>
                    ))}
                </SelectInput>
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
