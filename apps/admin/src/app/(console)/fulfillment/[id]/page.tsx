import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, TextArea, TextInput } from "@/components/commerce-ui";
import { FULFILLMENT_LABELS, FulfillmentStatus, Hidden, OnlineOnly, RequestKey } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { flagBatch, releaseFulfillment, verifyItem } from "../../delivery-actions";

export const metadata: Metadata = { title: "Verify order" };

/**
 * The packing-table screen: one card per kit line, large controls, works on
 * a phone. A quantity alone never verifies: the batch code on the kit label
 * must match, and the recipe card, packaging and delivery label are checked.
 */
export default async function FulfillmentPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "fulfillment.read")) notFound();
  const { id } = await params;
  const f = await api.delivery.fulfillment(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const [labels, trace, exceptions] = await Promise.all([
    api.delivery.labels(token, id),
    api.delivery.fulfillmentTrace(token, id),
    can(admin, "delivery_exceptions.read") ? api.delivery.exceptions(token, { status: "open" }) : Promise.resolve(null),
  ]);
  const mine = (exceptions?.items ?? []).filter((e) => e.obligation_id === f.id);
  const verifying = can(admin, "fulfillment.verify") && ["ready_for_verification", "verification_in_progress"].includes(f.status);
  return (
    <>
      <PageHeader
        back={{ href: "/fulfillment", label: "Fulfillment queue" }}
        title={`Order ${f.order_number}`}
        description={
          <>
            {f.number} · {f.customer_name} · week of {formatDay(f.week)} · {f.kits_required} kits · {f.region} · <FulfillmentStatus status={f.status} />
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[1.4fr_1fr]">
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          {f.status === "exception" ? (
            <Notice
              tone="error"
              title={`Blocked${f.resume_status ? `; returns to “${FULFILLMENT_LABELS[f.resume_status] ?? f.resume_status}” when resolved` : ""}`}
              items={mine.length ? mine.map((e) => `${e.number}: ${e.summary}`) : ["See the history below."]}
            />
          ) : null}
          {mine.length ? (
            <p className="type-body-sm">
              {mine.map((e) => (
                <Link key={e.id} href={`/delivery/exceptions/${e.id}`} className="mr-3 font-semibold underline underline-offset-4">
                  Open {e.number}
                </Link>
              ))}
            </p>
          ) : null}
          {f.packages?.map((p) => (
            <Panel key={p.id} title={`Package ${p.code}`} note={`Verification: ${p.verification_status}. Handoff: ${p.handoff_status.replace(/_/g, " ")}.`}>
              <div className="grid gap-4">
                {p.items.map((it) => (
                  <article key={it.id} className="border border-border p-4">
                    <header className="flex flex-wrap items-baseline justify-between gap-2">
                      <h3 className="text-[1.25rem] font-semibold">
                        {it.quantity} × {it.meal_name}
                      </h3>
                      <span className="type-body-sm">
                        Batch <span className="font-mono font-semibold">{it.batch_code}</span> · recipe v{it.recipe_version}
                      </span>
                    </header>
                    {it.status !== "pending" ? (
                      <p className={`type-body-sm mt-2 font-semibold ${it.status === "verified" ? "text-success" : "text-error"}`}>
                        {it.status === "verified" ? `Verified: ${it.verified_quantity} counted.` : "Discrepancy recorded."}
                      </p>
                    ) : verifying ? (
                      <div className="mt-3">
                        <KForm action={verifyItem} submit="Record check" pending="Saving…" label={`Check ${it.meal_name}`} size="lg">
                          <Hidden values={{ id: f.id, item_id: it.id }} />
                          <RequestKey />
                          <div className="grid gap-4 sm:grid-cols-2">
                            <Labelled label="Kits counted in the package">
                              <TextInput name="counted_quantity" type="number" inputMode="numeric" min={0} required className="text-[1.25rem]" />
                            </Labelled>
                            <Labelled label="Batch code on the kit label" hint="Scan or type it.">
                              <TextInput name="scanned_batch_code" autoCapitalize="characters" autoComplete="off" required className="font-mono text-[1.25rem]" />
                            </Labelled>
                          </div>
                          <fieldset className="grid gap-2">
                            <legend className="type-body-sm mb-1 font-semibold">I checked</legend>
                            {[
                              ["recipe_confirmed", "The recipe card matches this meal"],
                              ["packaging_ok", "Packaging is intact and sealed"],
                              ["label_ok", "The delivery label is on the package"],
                            ].map(([name, label]) => (
                              <label key={name} className="flex min-h-11 items-center gap-3">
                                <input type="checkbox" name={name} className="size-6" />
                                <span>{label}</span>
                              </label>
                            ))}
                          </fieldset>
                          <Labelled label="Anything wrong? (optional)" hint="Writing here records a discrepancy.">
                            <TextArea name="discrepancy" rows={2} />
                          </Labelled>
                        </KForm>
                      </div>
                    ) : null}
                  </article>
                ))}
              </div>
            </Panel>
          ))}
          {verifying ? <OnlineOnly /> : null}
          {can(admin, "fulfillment.release") && f.status === "verified" ? (
            <Panel title="Release to dispatch" note="Holds, batch problems, payment and blocking exceptions are checked again now.">
              <KForm action={releaseFulfillment} submit="Release to dispatch" pending="Releasing…" label="Release to dispatch">
                <Hidden values={{ id: f.id, version: f.version }} />
              </KForm>
            </Panel>
          ) : null}
          {f.delivery_id ? (
            <p className="type-body-sm">
              Delivery{" "}
              <Link href={`/delivery/deliveries/${f.delivery_id}`} className="font-semibold underline underline-offset-4">
                {f.delivery_number}
              </Link>
            </p>
          ) : null}
        </div>
        <div className="grid content-start gap-6 [&>*]:min-w-0">
          <Panel title="Package labels" note={labels.items[0]?.notice}>
            {labels.items.map((l) => (
              <div key={l.package_code} className="border-2 border-dashed border-border-strong p-4 font-mono text-[0.9375rem]">
                <p className="font-display text-[1.5rem] font-normal not-italic">Mise</p>
                <p className="mt-1 text-[1.25rem] font-bold">{l.package_code}</p>
                <p>Order {l.order_number}</p>
                <p>For {l.recipient}</p>
                <p>
                  {l.region}
                  {l.zone_code ? ` · zone ${l.zone_code}` : ""}
                  {l.delivery_date ? ` · ${formatDay(l.delivery_date)}` : " · date not set"}
                </p>
                <p className="mt-2">{l.kits} kit(s)</p>
                <ul className="type-caption list-disc pl-5">
                  {l.contents.map((c) => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
                <p className="type-caption mt-2">{l.handling}</p>
                {!l.certified ? <p className="type-caption mt-2 font-bold text-error">NOT CERTIFIED FOR LIVE USE</p> : null}
              </div>
            ))}
          </Panel>
          <Panel title="Traceability" note="Batch level: this order's kits came from these batches; the lots are those issued to each batch, not to this kit.">
            <ul className="type-body-sm grid gap-3">
              {trace.items.map((t) => (
                <li key={t.batch_id + t.package_code}>
                  <span className="font-semibold">{t.meal_name}</span> · batch{" "}
                  <Link href={`/production/batches/${t.batch_id}`} className="font-mono underline underline-offset-4">
                    {t.batch_code}
                  </Link>
                  {t.lots.length ? (
                    <ul className="type-caption mt-1 list-disc pl-5 text-muted">
                      {t.lots.map((l) => (
                        <li key={l.lot_id}>
                          {l.ingredient}: lot {l.lot_code} from {l.supplier_name}
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="type-caption text-muted">No ingredient lots were issued to this batch through inventory.</p>
                  )}
                </li>
              ))}
            </ul>
          </Panel>
          {can(admin, "fulfillment.verify") ? (
            <Panel title="Report a batch problem" note="Blocks every order with kits from the batch until resolved. Production's records are not changed.">
              <KForm action={flagBatch} submit="Flag batch" pending="Flagging…" label="Flag a batch" variant="secondary">
                <Labelled label="Batch">
                  <select name="batch_id" className="min-h-11 border border-border-strong bg-surface px-3">
                    {trace.items.map((t) => (
                      <option key={t.batch_id} value={t.batch_id}>
                        {t.batch_code} · {t.meal_name}
                      </option>
                    ))}
                  </select>
                </Labelled>
                <Labelled label="What is wrong">
                  <TextArea name="reason" rows={2} />
                </Labelled>
              </KForm>
            </Panel>
          ) : null}
          <Panel title="History">
            <Timeline
              items={(f.history ?? []).map((h) => ({
                at: h.at,
                title: FULFILLMENT_LABELS[h.to] ?? h.to,
                detail: [h.reason, h.actor_name].filter(Boolean).join(" · "),
              }))}
            />
            {(f.verifications ?? []).length ? (
              <ul className="type-caption mt-4 grid gap-1">
                {(f.verifications ?? []).map((v, i) => (
                  <li key={i}>
                    {formatDateTime(v.at)} · {v.result} · counted {v.counted_quantity}, scanned {v.scanned_batch_code}
                    {v.actor_name ? ` · ${v.actor_name}` : ""}
                    {v.discrepancy ? ` · ${v.discrepancy}` : ""}
                  </li>
                ))}
              </ul>
            ) : null}
          </Panel>
        </div>
      </div>
    </>
  );
}
