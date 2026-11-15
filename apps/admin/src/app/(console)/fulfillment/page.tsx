import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { FULFILLMENT_LABELS, FulfillmentStatus } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { syncFulfillment } from "../delivery-actions";

export const metadata: Metadata = { title: "Fulfillment queue" };

export default async function FulfillmentQueuePage({ searchParams }: { searchParams: Promise<{ status?: string; week?: string; q?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "fulfillment.read")) notFound();
  const sp = await searchParams;
  const [list, consumer] = await Promise.all([
    api.delivery.fulfillments(token, { status: sp.status, week: sp.week, q: sp.q }),
    api.delivery.consumer(token),
  ]);
  return (
    <>
      <PageHeader
        title="Fulfillment queue"
        description="Orders whose kits production has completed, packed and released. Check each kit against the order before it goes to dispatch."
        actions={
          can(admin, "fulfillment.release") ? (
            <KForm action={syncFulfillment} submit="Check for ready orders" pending="Checking…" label="Check for ready orders" variant="secondary" inline />
          ) : null
        }
      />
      {consumer.failed.length ? (
        <Notice tone="error" title="Readiness events that failed" items={consumer.failed.map((f) => `Job ${f.job_id} after ${f.attempts} attempts: ${f.error}`)} />
      ) : null}
      <FilterBar>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? "active"}>
            <option value="active">Open (not fulfilled)</option>
            {Object.entries(FULFILLMENT_LABELS).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
            <option value="all">All</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Week (Monday)">
          <TextInput type="date" name="week" defaultValue={sp.week} />
        </Labelled>
        <Labelled label="Search">
          <TextInput name="q" defaultValue={sp.q} placeholder="Order or customer" />
        </Labelled>
      </FilterBar>
      {list.items.length ? (
        <DataTable head={["Fulfillment", "Order", "Customer", "Week", "Kits", "Region", "Status"]}>
          {list.items.map((f) => (
            <tr key={f.id} className={f.status === "exception" ? "bg-error/5" : undefined}>
              <td className="p-3">
                <Link href={`/fulfillment/${f.id}`} className="font-semibold underline underline-offset-4">
                  {f.number}
                </Link>
              </td>
              <td className="p-3">{f.order_number}</td>
              <td className="p-3">{f.customer_name}</td>
              <td className="p-3">{formatDay(f.week)}</td>
              <td className="p-3">{f.kits_required}</td>
              <td className="p-3">{f.region}</td>
              <td className="p-3">
                <FulfillmentStatus status={f.status} />
                {f.open_blocking_exceptions ? <span className="type-caption ml-2 text-error">{f.open_blocking_exceptions} blocking</span> : null}
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>Nothing here. Orders appear when production completes every batch of their meals.</Empty>
      )}
    </>
  );
}
