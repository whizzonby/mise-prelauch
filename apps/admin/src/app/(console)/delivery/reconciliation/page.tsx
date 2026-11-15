import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty } from "@/components/commerce-ui";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Reconciliation" };

/** Runs that went out, and whether each is reconciled. Each run's own page shows its discrepancies. */
export default async function ReconciliationPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery.read")) notFound();
  const [released, completed, reconciled] = await Promise.all([
    api.delivery.runs(token, { status: "released" }),
    api.delivery.runs(token, { status: "completed" }),
    api.delivery.runs(token, { status: "reconciled" }),
  ]);
  const runs = [...released.items, ...completed.items, ...reconciled.items];
  const recs = await Promise.all(runs.map((r) => (r.status === "reconciled" ? Promise.resolve(null) : api.delivery.reconciliation(token, r.id))));
  return (
    <>
      <PageHeader
        title="Reconciliation"
        description="Planned deliveries against handed-over, delivered, failed and returned packages. A run closes only when nothing is unexplained."
      />
      {runs.length ? (
        <DataTable head={["Run", "Date", "Partner", "Stops", "Unexplained", "Status"]}>
          {runs.map((r, i) => (
            <tr key={r.id}>
              <td className="p-3">
                <Link href={`/delivery/runs/${r.id}`} className="font-semibold underline underline-offset-4">
                  {r.number}
                </Link>
              </td>
              <td className="p-3">{formatDay(r.service_date)}</td>
              <td className="p-3">{r.partner_name}</td>
              <td className="p-3">{r.stop_count}</td>
              <td className={`p-3 ${recs[i]?.discrepancies.length ? "font-semibold text-error" : ""}`}>{recs[i] ? recs[i]!.discrepancies.length : "—"}</td>
              <td className="p-3">
                <StatusTag status={r.status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No runs have gone out yet.</Empty>
      )}
    </>
  );
}
