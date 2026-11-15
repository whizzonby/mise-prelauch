import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { EXCEPTION_TYPES, Hidden, SEVERITIES } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Options } from "@/components/kitchen-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { raiseException } from "../../delivery-actions";

export const metadata: Metadata = { title: "Tracking & exceptions" };

export default async function ExceptionsPage({ searchParams }: { searchParams: Promise<{ status?: string; stage?: string; delivery_id?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_exceptions.read")) notFound();
  const sp = await searchParams;
  const list = await api.delivery.exceptions(token, { status: sp.status, stage: sp.stage, delivery_id: sp.delivery_id });
  return (
    <>
      <PageHeader title="Tracking & exceptions" description="Every fulfillment and delivery problem in one queue, most severe first. A blocking exception stops dispatch of what it names." />
      <FilterBar>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? "open"}>
            <option value="open">Open</option>
            <option value="resolved">Resolved</option>
            <option value="all">All</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Stage">
          <SelectInput name="stage" defaultValue={sp.stage ?? ""}>
            <option value="">Both</option>
            <option value="fulfillment">Fulfillment</option>
            <option value="delivery">Delivery</option>
          </SelectInput>
        </Labelled>
        {sp.delivery_id ? <input type="hidden" name="delivery_id" value={sp.delivery_id} /> : null}
      </FilterBar>
      {list.items.length ? (
        <DataTable head={["Exception", "Severity", "Type", "Summary", "About", "Owner", "Opened", "Status"]} minWidth="60rem">
          {list.items.map((e) => (
            <tr key={e.id}>
              <td className="p-3">
                <Link href={`/delivery/exceptions/${e.id}`} className="font-semibold underline underline-offset-4">
                  {e.number}
                </Link>
                {e.blocking ? <span className="type-caption block font-semibold text-error">Blocking</span> : null}
              </td>
              <td className="p-3">
                <StatusTag status={e.severity} />
              </td>
              <td className="p-3">{humanize(e.type)}</td>
              <td className="p-3">{e.summary}</td>
              <td className="p-3">{e.delivery_number ?? e.order_number ?? "—"}</td>
              <td className="p-3">{e.owner_name ?? "Unassigned"}</td>
              <td className="p-3">{formatDateTime(e.created_at)}</td>
              <td className="p-3">
                <StatusTag status={e.status} />
              </td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <Empty>No exceptions match.</Empty>
      )}
      {can(admin, "delivery_exceptions.manage") ? (
        <div className="mt-6">
          <Panel title="Raise an exception" note="Link it to a delivery (its number from the delivery page) so the history stays together.">
            <KForm action={raiseException} submit="Raise exception" pending="Saving…" label="Raise an exception" reset>
              <div className="grid gap-3 sm:grid-cols-3">
                <Labelled label="Type">
                  <SelectInput name="type">
                    <Options values={EXCEPTION_TYPES} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Severity">
                  <SelectInput name="severity" defaultValue="medium">
                    <Options values={SEVERITIES} />
                  </SelectInput>
                </Labelled>
                <Labelled label="Delivery id">
                  <TextInput name="delivery_id" defaultValue={sp.delivery_id} />
                </Labelled>
              </div>
              <Hidden values={{ stage: "delivery" }} />
              <Labelled label="Summary">
                <TextInput name="summary" />
              </Labelled>
              <Labelled label="Details">
                <TextArea name="notes" rows={2} />
              </Labelled>
              <label className="flex min-h-11 items-center gap-3">
                <input type="checkbox" name="blocking" className="size-5" /> Block dispatch until resolved
              </label>
            </KForm>
          </Panel>
        </div>
      ) : null}
    </>
  );
}
