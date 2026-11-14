import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { countAction, recordCounts } from "../../../supply-actions";

export const metadata: Metadata = { title: "Stock count" };

/** Counting one location, one lot per row: big numeric inputs for a phone. */
export default async function CountPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "inventory.read")) notFound();
  const { id } = await params;
  const c = await api.supply.count(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const counting = can(admin, "inventory.count") && c.status === "open";
  const hidden = (action: string) => (
    <>
      <input type="hidden" name="id" value={c.id} />
      <input type="hidden" name="action" value={action} />
    </>
  );
  return (
    <>
      <PageHeader
        back={{ href: "/inventory/counts", label: "All counts" }}
        title={`Count ${c.number}: ${c.location_name}`}
        description={
          <>
            <StatusTag status={c.status === "open" ? "in_progress" : c.status} label={c.status} /> · started by {c.started_by_name}, {formatDateTime(c.created_at)}
            {c.submitted_by_name ? ` · submitted by ${c.submitted_by_name}` : ""}
            {c.posted_by_name ? ` · posted by ${c.posted_by_name}` : ""}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {counting && c.to_count.length ? (
          <Panel title="Still to count" note="Weigh or count what is physically there.">
            <KForm action={recordCounts} submit="Save counts" pending="Saving…" label="Record counts" size="lg">
              <input type="hidden" name="id" value={c.id} />
              <ul className="grid gap-3">
                {c.to_count.map((l) => (
                  <li key={l.id} className="grid grid-cols-[1fr_9rem] items-end gap-3 border-b border-border pb-3">
                    <input type="hidden" name="lot_id" value={l.id} />
                    <span className="type-body font-semibold">{l.code}</span>
                    <Labelled label="Counted">
                      <TextInput name={`counted_${l.id}`} inputMode="decimal" className="text-[1.25rem]" />
                    </Labelled>
                  </li>
                ))}
              </ul>
            </KForm>
          </Panel>
        ) : null}
        <Panel title="Counted" note="Expected is the ledger balance when the line was counted.">
          {c.lines.length ? (
            <DataTable head={["Lot", "Ingredient", "Expected", "Counted", "Variance", "Counted by"]} minWidth="44rem">
              {c.lines.map((l) => (
                <tr key={l.id}>
                  <td className="p-3 font-semibold">{l.lot_code}</td>
                  <td className="p-3">{l.ingredient_name}</td>
                  <td className="p-3">
                    {l.expected} {l.unit}
                  </td>
                  <td className="p-3">
                    {l.counted} {l.unit}
                  </td>
                  <td className="p-3">
                    {Number(l.variance) ? <strong className={Number(l.variance) < 0 ? "text-error" : ""}>{l.variance}</strong> : "0"}
                    {l.moved_since ? <span className="type-caption block text-muted">Moved since counting: kept.</span> : null}
                  </td>
                  <td className="p-3">
                    {l.counted_by_name}
                    <span className="type-caption block text-muted">{formatDateTime(l.counted_at)}</span>
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">Nothing counted yet.</p>
          )}
        </Panel>
        <div className="flex flex-wrap gap-4">
          {counting ? (
            <KForm action={countAction} submit="Submit for approval" pending="…" label="Submit count" inline>
              {hidden("submit")}
            </KForm>
          ) : null}
          {c.status === "submitted" && can(admin, "inventory.adjust") ? (
            <KForm action={countAction} submit="Approve and post variances" pending="Posting…" label="Post count" inline>
              {hidden("post")}
            </KForm>
          ) : null}
          {(c.status === "open" || c.status === "submitted") && can(admin, "inventory.count") ? (
            <KForm action={countAction} submit="Cancel count" pending="…" label="Cancel count" variant="secondary" inline>
              {hidden("cancel")}
            </KForm>
          ) : null}
        </div>
      </div>
    </>
  );
}
