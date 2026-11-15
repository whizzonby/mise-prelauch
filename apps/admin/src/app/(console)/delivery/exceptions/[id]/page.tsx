import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea } from "@/components/commerce-ui";
import { Hidden, NEXT_ACTIONS } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Options } from "@/components/kitchen-ui";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { updateException } from "../../../delivery-actions";

export const metadata: Metadata = { title: "Exception" };

export default async function ExceptionPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_exceptions.read")) notFound();
  const { id } = await params;
  const e = await api.delivery.exception(token, id).catch((err) => {
    if (err instanceof ApiError && err.status === 404) notFound();
    throw err;
  });
  const open = e.status === "open" || e.status === "in_progress";
  return (
    <>
      <PageHeader
        back={{ href: "/delivery/exceptions", label: "Tracking & exceptions" }}
        title={`${e.number}: ${e.summary}`}
        description={
          <>
            {humanize(e.stage)} · {humanize(e.type)} · <StatusTag status={e.severity} /> · <StatusTag status={e.status} />
            {e.blocking ? <span className="font-semibold text-error"> · blocking</span> : null} · next action: {humanize(e.next_action)}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          <Panel title="About">
            {e.notes ? <p className="type-body-sm whitespace-pre-line">{e.notes}</p> : null}
            <ul className="type-body-sm mt-3 grid gap-1">
              {e.delivery_id ? (
                <li>
                  Delivery{" "}
                  <Link href={`/delivery/deliveries/${e.delivery_id}`} className="font-semibold underline underline-offset-4">
                    {e.delivery_number}
                  </Link>
                </li>
              ) : null}
              {e.obligation_id ? (
                <li>
                  Fulfillment for order{" "}
                  <Link href={`/fulfillment/${e.obligation_id}`} className="font-semibold underline underline-offset-4">
                    {e.order_number}
                  </Link>
                </li>
              ) : null}
              {e.batch_id ? (
                <li>
                  <Link href={`/production/batches/${e.batch_id}`} className="underline underline-offset-4">
                    Production batch
                  </Link>
                </li>
              ) : null}
              {e.run_id ? (
                <li>
                  <Link href={`/delivery/runs/${e.run_id}`} className="underline underline-offset-4">
                    Dispatch run
                  </Link>
                </li>
              ) : null}
              <li>Owner: {e.owner_name ?? "unassigned"}</li>
              {e.resolution ? <li>Resolution: {e.resolution}</li> : null}
            </ul>
          </Panel>
          {open && can(admin, "delivery_exceptions.manage") ? (
            <Panel title="Work the exception" note="Resolving needs a note on what was done. Refunds go through payments; nothing is refunded here.">
              <KForm action={updateException} submit="Save" pending="Saving…" label="Update exception">
                <Hidden values={{ id: e.id }} />
                <div className="grid gap-3 sm:grid-cols-3">
                  <Labelled label="Status">
                    <SelectInput name="status" defaultValue={e.status}>
                      <option value="open">Open</option>
                      <option value="in_progress">In progress</option>
                      <option value="resolved">Resolved</option>
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Next action">
                    <SelectInput name="next_action" defaultValue={e.next_action}>
                      <Options values={NEXT_ACTIONS} />
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Owner">
                    <SelectInput name="owner" defaultValue="">
                      <option value="">Keep</option>
                      <option value="me">Me</option>
                    </SelectInput>
                  </Labelled>
                </div>
                <Labelled label="Note">
                  <TextArea name="note" rows={2} />
                </Labelled>
                <Labelled label="Resolution (when resolving)">
                  <TextArea name="resolution" rows={2} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
        </div>
        <Panel title="History">
          <Timeline items={(e.events ?? []).map((x) => ({ at: x.created_at, title: humanize(x.type), detail: [x.body, x.actor_name].filter(Boolean).join(" · ") }))} />
        </Panel>
      </div>
    </>
  );
}
