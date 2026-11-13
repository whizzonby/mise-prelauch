import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, TextArea } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { BatchCard } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { resolveHold } from "../../kitchen-actions";

export const metadata: Metadata = { title: "Quality control" };

/** Holds first, then batches waiting for inspection, then hold history. */
export default async function QualityPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "quality.read")) notFound();
  const [holds, waiting, templates] = await Promise.all([
    api.kitchen.holds(token),
    can(admin, "production.read") ? api.kitchen.batches(token, { status: "awaiting_quality_check" }) : Promise.resolve({ items: [] }),
    api.kitchen.qualityTemplates(token),
  ]);
  const open = holds.items.filter((h) => h.status === "open");
  const release = can(admin, "quality.release_hold");
  return (
    <>
      <PageHeader
        title="Quality control"
        description="Checks follow the checklist configured from Mise's approved procedures; no thresholds are invented here and nothing here is a regulatory certification. Only staff with hold-release permission can lift a hold."
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {!templates.items.some((t) => t.active) ? (
          <p className="type-body-sm border-l-4 border-accent bg-surface p-4">
            No quality checklist is configured yet. {can(admin, "production.configure") ? <Link href="/production/settings" className="underline underline-offset-4">Add checks from the approved procedure.</Link> : "Ask a production manager to add one."}
          </p>
        ) : null}
        <Panel title={`Open holds (${open.length})`}>
          {open.length ? (
            <ul className="grid gap-4">
              {open.map((h) => (
                <li key={h.id} className="border-l-4 border-error bg-surface p-4">
                  <p className="type-body font-semibold">
                    <Link href={`/production/batches/${h.batch_id}`} className="underline underline-offset-4">
                      {h.batch_code} {h.meal_name}
                    </Link>{" "}
                    · {formatDay(h.production_date)}
                  </p>
                  <p className="type-body-sm mt-1">{h.reason}</p>
                  <p className="type-caption text-muted">
                    Held {formatDateTime(h.created_at)}
                    {h.created_by_name ? ` by ${h.created_by_name}` : ""}
                  </p>
                  {release ? (
                    <div className="mt-3">
                      <KForm action={resolveHold} submit="Release hold" pending="Saving…" label={`Resolve hold on ${h.batch_code}`} secondary={{ name: "decision", value: "discard", label: "Discard batch" }}>
                        <input type="hidden" name="id" value={h.id} />
                        <input type="hidden" name="batch_id" value={h.batch_id} />
                        <Labelled label="Resolution" hint="Releasing sends the batch back for a fresh inspection; it does not pass it.">
                          <TextArea name="notes" rows={2} required maxLength={1000} />
                        </Labelled>
                      </KForm>
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>No batch is on hold.</EmptyState>
          )}
        </Panel>
        <Panel title="Waiting for checks and packing">
          {waiting.items.length ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {waiting.items.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          ) : (
            <EmptyState>Nothing waiting.</EmptyState>
          )}
        </Panel>
        <Panel title="Hold history">
          {holds.items.filter((h) => h.status !== "open").length ? (
            <DataTable head={["Batch", "Reason", "Outcome", "Resolved"]} minWidth="40rem">
              {holds.items
                .filter((h) => h.status !== "open")
                .map((h) => (
                  <tr key={h.id}>
                    <td className="p-3">
                      {h.batch_code} {h.meal_name}
                    </td>
                    <td className="p-3">{h.reason}</td>
                    <td className="p-3">
                      <StatusTag status={h.status === "released" ? "released" : "cancelled"} label={h.status === "released" ? "Released" : "Discarded"} />
                      {h.resolution_notes ? <span className="type-caption block">{h.resolution_notes}</span> : null}
                    </td>
                    <td className="p-3">
                      {h.resolved_at ? formatDateTime(h.resolved_at) : "—"} {h.resolved_by_name}
                    </td>
                  </tr>
                ))}
            </DataTable>
          ) : (
            <EmptyState>No resolved holds.</EmptyState>
          )}
        </Panel>
      </div>
    </>
  );
}
