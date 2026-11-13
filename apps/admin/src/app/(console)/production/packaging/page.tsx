import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { BatchCard } from "@/components/kitchen-ui";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Packaging" };

/** Batches to pack. Packaging stock is managed with inventory in Sprint 6. */
export default async function PackagingPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "packaging.manage")) notFound();
  const [inProgress, waiting] = await Promise.all([
    api.kitchen.batches(token, { status: "in_progress" }),
    api.kitchen.batches(token, { status: "awaiting_quality_check" }),
  ]);
  const toPack = waiting.items.filter((b) => b.packaging_status !== "completed");
  const packed = waiting.items.filter((b) => b.packaging_status === "completed");
  return (
    <>
      <PageHeader
        title="Packaging"
        description="Open a batch to record kits packed and confirm labels against the approved recipe. Packed kits must equal accepted kits. Packaging stock is not tracked here; Sprint 6 inventory will."
      />
      <div className="grid gap-6">
        <Panel title={`To pack (${toPack.length})`} note="Output recorded, not yet packed.">
          {toPack.length ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {toPack.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          ) : (
            <EmptyState>Nothing to pack.</EmptyState>
          )}
        </Panel>
        <Panel title={`Packed, waiting for completion (${packed.length})`}>
          {packed.length ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {packed.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          ) : (
            <EmptyState>None.</EmptyState>
          )}
        </Panel>
        <Panel title={`Still in production (${inProgress.items.length})`}>
          {inProgress.items.length ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {inProgress.items.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          ) : (
            <EmptyState>None.</EmptyState>
          )}
        </Panel>
      </div>
    </>
  );
}
