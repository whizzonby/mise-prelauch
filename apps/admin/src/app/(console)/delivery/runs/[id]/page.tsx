import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { DeliveryStatus, Hidden, OnlineOnly, RequestKey } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { moveStop, reconcileRun, recordCost, recordHandoff, removeStop, runAction } from "../../../delivery-actions";

export const metadata: Metadata = { title: "Dispatch run" };

export default async function RunPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery.read") && !can(admin, "delivery.read_assigned")) notFound();
  const { id } = await params;
  const run = await api.delivery.run(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const staff = can(admin, "delivery.read");
  const [recon, costs] = await Promise.all([
    staff && ["released", "completed", "reconciled"].includes(run.status) ? api.delivery.reconciliation(token, id) : Promise.resolve(null),
    can(admin, "delivery_partners.read") ? api.delivery.costs(token, id) : Promise.resolve(null),
  ]);
  const stops = run.stops ?? [];
  const order = stops.filter((s) => s.status === "active").map((s) => s.id);
  const draft = run.status === "draft";
  const assign = can(admin, "delivery.assign") && draft;
  const dispatch = can(admin, "delivery.dispatch");
  const m = run.manifest;
  const handoverable = run.status === "released" && dispatch && m;
  return (
    <>
      <PageHeader
        back={{ href: staff ? "/delivery/runs" : "/delivery/runs", label: staff ? "Dispatch runs" : "My runs" }}
        title={`Run ${run.number}`}
        description={
          <>
            {formatDay(run.service_date)} · {run.partner_name} ({run.partner_mode.replace(/_/g, " ")}) {run.courier_name ? `· ${run.courier_name}` : ""} ·{" "}
            {run.stop_count} stop(s), {run.package_count} package(s) · <StatusTag status={run.status} />
          </>
        }
        actions={
          m ? (
            <a href={`/delivery/runs/${run.id}/manifest`} className="type-body-sm inline-flex min-h-11 items-center rounded-md border border-foreground px-4 font-semibold print:hidden">
              Download manifest (CSV)
            </a>
          ) : null
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <Panel title="Stops" note={draft ? "Order stops by hand. Changes are locked once the manifest is generated." : undefined}>
          {stops.length ? (
            <DataTable head={["#", "Delivery", "Recipient", "Address", "Window", "Pkgs", "Status", ""]} minWidth="48rem">
              {stops.map((s, i) => (
                <tr key={s.id}>
                  <td className="p-3 font-semibold">{s.sequence}</td>
                  <td className="p-3">
                    <Link href={`/delivery/deliveries/${s.delivery_id}`} className="font-semibold underline underline-offset-4">
                      {s.delivery_number}
                    </Link>
                    {s.capacity_override ? <span className="type-caption block text-error">Over capacity: {s.capacity_override}</span> : null}
                    {s.partner_override ? <span className="type-caption block text-error">{s.partner_override}</span> : null}
                  </td>
                  <td className="p-3">{s.recipient}</td>
                  <td className="p-3">
                    {s.address_line} · {s.region}
                  </td>
                  <td className="p-3">{s.window_label ?? "—"}</td>
                  <td className="p-3">{s.package_count}</td>
                  <td className="p-3">
                    <DeliveryStatus status={s.delivery_status} />
                  </td>
                  <td className="p-3">
                    {assign && s.status === "active" ? (
                      <div className="flex flex-wrap gap-2">
                        {i > 0 ? (
                          <KForm action={moveStop} submit="↑" pending="…" label={`Move ${s.delivery_number} up`} inline variant="secondary">
                            <Hidden values={{ run_id: run.id, stop_id: s.id, direction: "up" }} />
                            {order.map((o) => (
                              <input key={o} type="hidden" name="order" value={o} />
                            ))}
                          </KForm>
                        ) : null}
                        {i < order.length - 1 ? (
                          <KForm action={moveStop} submit="↓" pending="…" label={`Move ${s.delivery_number} down`} inline variant="secondary">
                            <Hidden values={{ run_id: run.id, stop_id: s.id, direction: "down" }} />
                            {order.map((o) => (
                              <input key={o} type="hidden" name="order" value={o} />
                            ))}
                          </KForm>
                        ) : null}
                        <KForm action={removeStop} submit="Remove" pending="…" label={`Remove ${s.delivery_number}`} inline variant="secondary">
                          <Hidden values={{ run_id: run.id, stop_id: s.id, reason: "Removed while planning" }} />
                        </KForm>
                      </div>
                    ) : null}
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">No stops yet. Assign scheduled deliveries from dispatch planning.</p>
          )}
          {dispatch ? (
            <div className="mt-4 flex flex-wrap gap-3">
              {draft && stops.length ? (
                <KForm action={runAction} submit="Generate manifest" pending="Generating…" label="Finalize run" inline>
                  <Hidden values={{ run_id: run.id, action: "finalize" }} />
                </KForm>
              ) : null}
              {run.status === "finalized" ? (
                <>
                  <KForm action={runAction} submit="Release for pickup" pending="Releasing…" label="Release run" inline>
                    <Hidden values={{ run_id: run.id, action: "release" }} />
                  </KForm>
                  <KForm action={runAction} submit="Reopen to change stops" pending="…" label="Reopen run" inline variant="secondary">
                    <Hidden values={{ run_id: run.id, action: "reopen", reason: "Stops need to change" }} />
                  </KForm>
                </>
              ) : null}
            </div>
          ) : null}
        </Panel>

        {handoverable ? (
          <Panel title="Record the handoff" note="Count each package as it is handed over. A rejected or missing package raises a blocking exception. Release alone is not custody.">
            <KForm action={recordHandoff} submit="Record handoff" pending="Saving…" label="Record handoff" size="lg">
              <Hidden values={{ run_id: run.id }} />
              <RequestKey />
              <div className="grid gap-3 sm:grid-cols-2">
                <Labelled label="From">
                  <TextInput name="from_party" defaultValue="Mise kitchen" />
                </Labelled>
                <Labelled label="Received by">
                  <TextInput name="to_party" defaultValue={run.courier_name ?? run.partner_name} />
                </Labelled>
              </div>
              <fieldset className="grid gap-2">
                <legend className="type-body-sm mb-1 font-semibold">Packages</legend>
                {m.content.stops.flatMap((s) =>
                  s.packages.map((p) => (
                    <div key={p.id} className="grid items-center gap-2 border border-border p-3 sm:grid-cols-[1fr_12rem]">
                      <input type="hidden" name="package" value={p.id} />
                      <span className="type-body-sm">
                        <span className="font-mono font-semibold">{p.code}</span> · stop {s.sequence} · {p.contents}
                      </span>
                      <SelectInput name={`outcome_${p.id}`} defaultValue="accepted" aria-label={`Outcome for ${p.code}`}>
                        <option value="accepted">Handed over</option>
                        <option value="rejected">Refused by courier</option>
                        <option value="missing">Missing</option>
                      </SelectInput>
                    </div>
                  )),
                )}
              </fieldset>
              <Labelled label="Evidence reference (optional)" hint="e.g. a signed sheet number. No photos are stored.">
                <TextInput name="evidence_ref" />
              </Labelled>
              <Labelled label="Notes">
                <TextArea name="notes" rows={2} />
              </Labelled>
            </KForm>
            <div className="mt-3">
              <OnlineOnly />
            </div>
          </Panel>
        ) : null}

        {run.handoffs?.length ? (
          <Panel title="Custody record">
            <ul className="type-body-sm grid gap-2">
              {run.handoffs.map((h) => (
                <li key={h.id}>
                  {formatDateTime(h.handed_at)} · {h.from_party} → {h.to_party} · {h.accepted_count} accepted
                  {h.rejected_count ? `, ${h.rejected_count} refused` : ""}
                  {h.missing_count ? `, ${h.missing_count} missing` : ""}
                  {h.disputed ? <span className="font-semibold text-error"> · disputed</span> : null}
                  {h.actor_name ? ` · recorded by ${h.actor_name}` : ""}
                </li>
              ))}
            </ul>
          </Panel>
        ) : null}

        {m ? (
          <Panel title={`Manifest, revision ${m.revision}`} note={`Generated ${formatDateTime(m.generated_at)} · ${m.stop_count} stops · ${m.package_count} packages · ${m.content_hash.slice(0, 12)}`}>
            <div className="print:text-[11pt]">
              <p className="type-body-sm mb-3">{m.content.handling}</p>
              <ol className="grid gap-3">
                {m.content.stops.map((s) => (
                  <li key={s.delivery} className="break-inside-avoid border border-border p-3">
                    <p className="font-semibold">
                      {s.sequence}. {s.recipient} · {s.delivery}
                    </p>
                    <p className="type-body-sm">
                      {s.address}, {s.region}
                      {s.window ? ` · ${s.window}` : ""}
                    </p>
                    {s.phone ? <p className="type-body-sm">{s.phone}</p> : null}
                    {s.instructions ? <p className="type-body-sm border-l-2 border-accent pl-2">{s.instructions}</p> : null}
                    <p className="type-caption mt-1">
                      {s.packages.map((p) => `${p.code} (${p.kits} kit${p.kits === 1 ? "" : "s"})`).join(", ")}
                    </p>
                  </li>
                ))}
              </ol>
            </div>
          </Panel>
        ) : null}

        {recon ? (
          <Panel title="Reconciliation" note="Planned against handed over, delivered, failed and returned. Closing is refused while anything is unexplained.">
            <dl className="type-body-sm grid grid-cols-2 gap-2 sm:grid-cols-4">
              {Object.entries(recon.summary).map(([k, v]) => (
                <div key={k}>
                  <dt className="type-caption text-muted">{k.replace(/_/g, " ")}</dt>
                  <dd className="font-display text-[1.5rem]">{v}</dd>
                </div>
              ))}
            </dl>
            <div className="mt-3">
              <Notice tone="error" title="Discrepancies" items={recon.discrepancies.map((d) => `${d.delivery ?? ""} ${d.detail}`.trim())} />
            </div>
            {recon.status === "reconciled" ? (
              <p className="type-body-sm mt-3 font-semibold text-success">Reconciled {recon.closed_at ? formatDateTime(recon.closed_at) : ""}.</p>
            ) : can(admin, "delivery.reconcile") ? (
              <div className="mt-3">
                <KForm action={reconcileRun} submit="Close and reconcile" pending="Checking…" label="Reconcile run">
                  <Hidden values={{ run_id: run.id }} />
                  <Labelled label="Note (optional)">
                    <TextInput name="explanation" />
                  </Labelled>
                </KForm>
              </div>
            ) : null}
          </Panel>
        ) : null}

        {costs ? (
          <Panel title="Partner costs" note="Recorded figures only. Estimated, contracted and actual are kept apart; no rate is assumed.">
            {costs.items.length ? (
              <ul className="type-body-sm grid gap-1">
                {costs.items.map((c) => (
                  <li key={c.id}>
                    {c.kind} · {money(c.amount_minor, c.currency)} {c.rate_reference ? `· ${c.rate_reference}` : ""} {c.notes ? `· ${c.notes}` : ""}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="type-body-sm text-muted">No costs recorded.</p>
            )}
            {can(admin, "delivery_partners.manage") ? (
              <div className="mt-3">
                <KForm action={recordCost} submit="Record cost" pending="Saving…" label="Record a cost" inline reset>
                  <Hidden values={{ run_id: run.id, partner_id: run.partner_id }} />
                  <Labelled label="Kind">
                    <SelectInput name="kind">
                      {["actual", "estimated", "contracted", "adjustment"].map((k) => (
                        <option key={k}>{k}</option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Amount (TTD)">
                    <TextInput name="amount" inputMode="decimal" />
                  </Labelled>
                  <Labelled label="Rate reference">
                    <TextInput name="rate_reference" />
                  </Labelled>
                </KForm>
              </div>
            ) : null}
          </Panel>
        ) : null}
      </div>
    </>
  );
}
