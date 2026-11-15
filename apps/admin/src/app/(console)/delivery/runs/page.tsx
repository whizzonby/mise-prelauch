import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Dispatch runs" };

export default async function RunsPage({ searchParams }: { searchParams: Promise<{ date?: string; status?: string }> }) {
  const { token, admin } = await requireAdmin();
  const all = can(admin, "delivery.read");
  if (!all && !can(admin, "delivery.read_assigned")) notFound();
  const sp = await searchParams;
  const runs = await api.delivery.runs(token, { date: sp.date || undefined, status: sp.status || undefined }).catch((e) => {
    // A courier login not yet linked to a courier record sees no runs.
    if (e instanceof ApiError && e.status === 403 && !all) return null;
    throw e;
  });
  if (!runs) {
    return (
      <>
        <PageHeader title="My runs" />
        <Empty>Your login is not linked to a courier record yet. Ask a dispatch manager to link it; then your assigned runs appear here.</Empty>
      </>
    );
  }
  return (
    <>
      <PageHeader
        title={all ? "Dispatch runs" : "My runs"}
        description={all ? "Each run is one partner (and optionally one courier) on one date, with its stops in order." : "The runs assigned to you. Other deliveries are not shown."}
      />
      <FilterBar>
        <Labelled label="Date">
          <TextInput type="date" name="date" defaultValue={sp.date} />
        </Labelled>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">Any</option>
            {["draft", "finalized", "released", "completed", "reconciled"].map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </SelectInput>
        </Labelled>
      </FilterBar>
      {runs.items.length ? (
        <DataTable head={["Run", "Date", "Partner", "Courier", "Stops", "Packages", "Status"]}>
          {runs.items.map((r) => (
            <tr key={r.id}>
              <td className="p-3">
                <Link href={`/delivery/runs/${r.id}`} className="font-semibold underline underline-offset-4">
                  {r.number}
                </Link>
              </td>
              <td className="p-3">{formatDay(r.service_date)}</td>
              <td className="p-3">{r.partner_name}</td>
              <td className="p-3">{r.courier_name ?? "—"}</td>
              <td className="p-3">{r.stop_count}</td>
              <td className="p-3">{r.package_count}</td>
              <td className="p-3">
                <StatusTag status={r.status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No runs match.</Empty>
      )}
    </>
  );
}
