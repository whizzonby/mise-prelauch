import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader, StatusTag } from "@/components/admin-ui";
import { Empty, Labelled, Panel, SelectInput, TextArea } from "@/components/commerce-ui";
import { Hidden } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { reviewReturn } from "../../delivery-actions";

export const metadata: Metadata = { title: "Returns" };

/** Packages that came back. Quarantined until reviewed; never returned to food inventory. */
export default async function ReturnsPage({ searchParams }: { searchParams: Promise<{ status?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_exceptions.read")) notFound();
  const sp = await searchParams;
  const list = await api.delivery.returns(token, sp.status);
  return (
    <>
      <PageHeader
        title="Returns"
        description="Meal kits that came back after a failed attempt. No approved food-handling procedure for reuse exists, so they stay quarantined until someone records what was done with them."
      />
      <FilterBar>
        <Labelled label="Show">
          <SelectInput name="status" defaultValue={sp.status ?? "awaiting_review"}>
            <option value="awaiting_review">Awaiting review</option>
            <option value="all">All</option>
          </SelectInput>
        </Labelled>
      </FilterBar>
      {list.items.length ? (
        <div className="grid gap-4">
          {list.items.map((r) => (
            <Panel key={r.id} title={`${r.delivery_number} · order ${r.order_number}`} note={`Returned ${formatDateTime(r.returned_at)} · attempt ${r.attempt}`}>
              <p className="type-body-sm">
                {r.reason} · condition {humanize(r.condition)} · <StatusTag status={r.disposition} /> · packages {r.packages.join(", ")}
                {r.received_by ? ` · received by ${r.received_by}` : ""}
              </p>
              {r.disposition_note ? <p className="type-body-sm mt-1 text-muted">{r.disposition_note}</p> : null}
              <p className="type-body-sm mt-1">
                <Link href={`/delivery/deliveries/${r.delivery_id}`} className="underline underline-offset-4">
                  Open delivery
                </Link>
              </p>
              {r.status === "awaiting_review" && can(admin, "delivery_exceptions.manage") ? (
                <div className="mt-3">
                  <KForm action={reviewReturn} submit="Record review" pending="Saving…" label={`Review return ${r.delivery_number}`}>
                    <Hidden values={{ id: r.id }} />
                    <Labelled label="What was done">
                      <SelectInput name="disposition">
                        <option value="discarded">Discarded</option>
                        <option value="other_approved">Another approved procedure</option>
                      </SelectInput>
                    </Labelled>
                    <Labelled label="Note (which procedure, who)">
                      <TextArea name="note" rows={2} />
                    </Labelled>
                  </KForm>
                </div>
              ) : null}
            </Panel>
          ))}
        </div>
      ) : (
        <Empty>No returns waiting for review.</Empty>
      )}
    </>
  );
}
