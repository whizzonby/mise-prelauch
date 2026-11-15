import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader } from "@/components/admin-ui";
import { DataTable, Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { DeliveryStatus, Hidden } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { addStop, createRun, decideReschedule, scheduleDelivery } from "../../delivery-actions";

export const metadata: Metadata = { title: "Dispatch planning" };

/**
 * Plan one service date: schedule released deliveries onto it, see capacity
 * (unknown is shown as unknown), create runs for approved partners and
 * assign deliveries to them. Nothing here changes an order or production.
 */
export default async function PlanningPage({ searchParams }: { searchParams: Promise<{ date?: string; zone?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery.read")) notFound();
  const sp = await searchParams;
  const today = new Date().toISOString().slice(0, 10);
  const [calendar, zones, partners, couriers, unassigned, requests] = await Promise.all([
    api.delivery.calendar(token, today, 42),
    api.delivery.zones(token),
    api.delivery.partners(token),
    api.delivery.couriers(token),
    api.delivery.deliveries(token, { status: "unassigned", zone_id: sp.zone }),
    api.delivery.rescheduleRequests(token),
  ]);
  const openDays = [...new Set(calendar.items.filter((d) => d.status === "open").map((d) => d.date))];
  const date = sp.date && /^\d{4}-\d{2}-\d{2}$/.test(sp.date) ? sp.date : openDays[0];
  const [runs, capacity, onDate] = date
    ? await Promise.all([api.delivery.runs(token, { date }), api.delivery.capacity(token, date), api.delivery.deliveries(token, { date, status: "all" })])
    : [null, null, null];
  const unscheduled = unassigned.items.filter((d) => !d.schedule_confirmed_at);
  const waiting = unassigned.items.filter((d) => d.schedule_confirmed_at && d.service_date === date);
  const windows = calendar.items.filter((d) => d.date === date).flatMap((d) => d.windows.filter((w) => w.status === "approved").map((w) => ({ ...w, zone: d.zone_code })));
  const draftRuns = (runs?.items ?? []).filter((r) => r.status === "draft");
  const assignable = partners.items.filter((p) => p.assignable);
  const plan = can(admin, "delivery.plan");
  const assign = can(admin, "delivery.assign");
  return (
    <>
      <PageHeader
        title="Dispatch planning"
        description="Schedule released deliveries onto an approved date, then assign them to runs. Stop order is set by hand; there is no route optimiser."
      />
      {!openDays.length ? (
        <Notice tone="warning" title="No approved delivery dates" items={["Add service days in the delivery calendar before anything can be scheduled."]} />
      ) : null}
      <FilterBar>
        <Labelled label="Service date">
          <SelectInput name="date" defaultValue={date}>
            {openDays.map((d) => (
              <option key={d} value={d}>
                {formatDay(d)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Zone">
          <SelectInput name="zone" defaultValue={sp.zone ?? ""}>
            <option value="">All zones</option>
            {zones.items.map((z) => (
              <option key={z.id} value={z.id}>
                {z.code} · {z.name}
              </option>
            ))}
          </SelectInput>
        </Labelled>
      </FilterBar>

      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        <Panel title={`Not scheduled (${unscheduled.length})`} note="Released by fulfillment; needs a date (and a zone if no coverage rule applied).">
          {unscheduled.length ? (
            <ul className="grid gap-4">
              {unscheduled.map((d) => (
                <li key={d.id} className="border border-border p-3">
                  <p className="type-body-sm">
                    <Link href={`/delivery/deliveries/${d.id}`} className="font-semibold underline underline-offset-4">
                      {d.number}
                    </Link>{" "}
                    · {d.order_number} · {d.address.region_label} · {d.zone_code ?? <span className="text-error">no zone</span>}
                    {d.blocking_exceptions ? <span className="text-error"> · blocked</span> : null}
                  </p>
                  {plan && date ? (
                    <div className="mt-2">
                      <KForm action={scheduleDelivery} submit="Schedule" pending="Saving…" label={`Schedule ${d.number}`} inline>
                        <Hidden values={{ id: d.id, service_date: date }} />
                        {windows.length ? (
                          <Labelled label="Window">
                            <SelectInput name="window_id" defaultValue="">
                              <option value="">No window promised</option>
                              {windows.map((w) => (
                                <option key={w.id} value={w.id}>
                                  {w.label} {w.starts_at}–{w.ends_at}
                                  {w.zone ? ` (${w.zone})` : ""}
                                </option>
                              ))}
                            </SelectInput>
                          </Labelled>
                        ) : null}
                        {!d.zone_id ? (
                          <>
                            <Labelled label="Zone (by hand)">
                              <SelectInput name="zone_id" defaultValue="">
                                <option value="">Choose…</option>
                                {zones.items.filter((z) => z.status === "active").map((z) => (
                                  <option key={z.id} value={z.id}>
                                    {z.code}
                                  </option>
                                ))}
                              </SelectInput>
                            </Labelled>
                            <Labelled label="Why this zone">
                              <TextInput name="reason" />
                            </Labelled>
                          </>
                        ) : null}
                      </KForm>
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <Empty>Nothing waiting for a date.</Empty>
          )}
        </Panel>

        <Panel title={date ? `Capacity on ${formatDay(date)}` : "Capacity"} note="Where no capacity is configured it is unknown, and assigning needs an explicit override.">
          {capacity?.items.length ? (
            <DataTable head={["Scope", "Capacity", "Assigned", "Left"]} minWidth="24rem">
              {capacity.items.map((c) => (
                <tr key={c.id}>
                  <td className="p-3">{[c.zone_code ?? "All zones", c.partner_name, c.window_label, c.courier_name].filter(Boolean).join(" · ")}</td>
                  <td className="p-3">{c.capacity}</td>
                  <td className="p-3">{c.assigned}</td>
                  <td className={`p-3 font-semibold ${c.remaining <= 0 ? "text-error" : ""}`}>{c.remaining}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <p className="type-body-sm text-muted">Unknown: no capacity is configured for this date.</p>
          )}
        </Panel>
      </div>

      {date ? (
        <div className="mt-6 grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          <Panel title={`Scheduled, waiting for a run (${waiting.length})`}>
            {waiting.length ? (
              <ul className="grid gap-4">
                {waiting.map((d) => (
                  <li key={d.id} className="border border-border p-3">
                    <p className="type-body-sm">
                      <Link href={`/delivery/deliveries/${d.id}`} className="font-semibold underline underline-offset-4">
                        {d.number}
                      </Link>{" "}
                      · {d.address.region_label} · {d.zone_code} {d.window_label ? `· ${d.window_label}` : ""} · {d.package_count} package(s)
                    </p>
                    {assign && draftRuns.length ? (
                      <div className="mt-2">
                        <KForm action={addStop} submit="Assign" pending="Assigning…" label={`Assign ${d.number}`} inline>
                          <Hidden values={{ delivery_id: d.id }} />
                          <Labelled label="Run">
                            <SelectInput name="run_id">
                              {draftRuns.map((r) => (
                                <option key={r.id} value={r.id}>
                                  {r.number} · {r.partner_name}
                                  {r.courier_name ? ` · ${r.courier_name}` : ""}
                                </option>
                              ))}
                            </SelectInput>
                          </Labelled>
                          {can(admin, "delivery.dispatch") ? (
                            <Labelled label="Capacity override (only if needed)">
                              <TextInput name="capacity_override" placeholder="Reason" />
                            </Labelled>
                          ) : null}
                        </KForm>
                      </div>
                    ) : null}
                  </li>
                ))}
              </ul>
            ) : (
              <Empty>Nothing scheduled for this date is waiting.</Empty>
            )}
          </Panel>

          <Panel title={`Runs on ${formatDay(date)}`}>
            {runs?.items.length ? (
              <ul className="type-body-sm grid gap-2">
                {runs.items.map((r) => (
                  <li key={r.id}>
                    <Link href={`/delivery/runs/${r.id}`} className="font-semibold underline underline-offset-4">
                      {r.number}
                    </Link>{" "}
                    · {r.partner_name} {r.courier_name ? `· ${r.courier_name}` : ""} · {r.stop_count} stop(s) · {r.status}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="type-body-sm text-muted">No runs yet.</p>
            )}
            {assign ? (
              assignable.length ? (
                <div className="mt-4 border-t border-border pt-4">
                  <KForm action={createRun} submit="Create run" pending="Creating…" label="Create a run">
                    <Hidden values={{ service_date: date }} />
                    <div className="grid gap-3 sm:grid-cols-2">
                      <Labelled label="Partner (approved only)">
                        <SelectInput name="partner_id">
                          {assignable.map((p) => (
                            <option key={p.id} value={p.id}>
                              {p.name}
                              {p.demo ? " (demo)" : ""}
                            </option>
                          ))}
                        </SelectInput>
                      </Labelled>
                      <Labelled label="Courier (optional)">
                        <SelectInput name="courier_id" defaultValue="">
                          <option value="">None named</option>
                          {couriers.items.filter((c) => c.status === "active").map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.name} · {c.partner_name}
                            </option>
                          ))}
                        </SelectInput>
                      </Labelled>
                    </div>
                  </KForm>
                </div>
              ) : (
                <p className="type-body-sm mt-4 text-error">No approved delivery partner: runs cannot be created.</p>
              )
            ) : null}
          </Panel>
        </div>
      ) : null}

      {requests.items.length && can(admin, "delivery.reschedule") ? (
        <div className="mt-6">
          <Panel title="Reschedule requests" note="Approving checks the calendar and known capacity. Nothing is promised until you decide.">
            <ul className="grid gap-4">
              {requests.items.map((q) => (
                <li key={q.id} className="border border-border p-3">
                  <p className="type-body-sm">
                    <Link href={`/delivery/deliveries/${q.delivery_id}`} className="font-semibold underline underline-offset-4">
                      {q.delivery_number}
                    </Link>{" "}
                    · from {q.requested_by} · currently {q.service_date ? formatDay(q.service_date) : "not scheduled"}
                    {q.preferred_date ? ` · asks for ${formatDay(q.preferred_date)}` : ""} · “{q.reason}”
                  </p>
                  <div className="mt-2">
                    <KForm action={decideReschedule} submit="Decide" pending="Saving…" label={`Decide request for ${q.delivery_number}`} inline>
                      <Hidden values={{ request_id: q.id }} />
                      <Labelled label="Decision">
                        <SelectInput name="decision">
                          <option value="approve">Approve</option>
                          <option value="decline">Decline</option>
                        </SelectInput>
                      </Labelled>
                      <Labelled label="New date">
                        <SelectInput name="new_date" defaultValue={q.preferred_date ?? ""}>
                          <option value="">—</option>
                          {openDays.map((d) => (
                            <option key={d} value={d}>
                              {formatDay(d)}
                            </option>
                          ))}
                        </SelectInput>
                      </Labelled>
                      <Labelled label="Note">
                        <TextInput name="note" />
                      </Labelled>
                    </KForm>
                  </div>
                </li>
              ))}
            </ul>
          </Panel>
        </div>
      ) : null}

      {onDate?.items.length ? (
        <div className="mt-6">
          <Panel title={`Everything on ${formatDay(date!)}`}>
            <DataTable head={["Delivery", "Order", "Zone", "Run", "Stop", "Status"]}>
              {onDate.items.map((d) => (
                <tr key={d.id}>
                  <td className="p-3">
                    <Link href={`/delivery/deliveries/${d.id}`} className="font-semibold underline underline-offset-4">
                      {d.number}
                    </Link>
                  </td>
                  <td className="p-3">{d.order_number}</td>
                  <td className="p-3">{d.zone_code ?? "—"}</td>
                  <td className="p-3">{d.run_number ?? "—"}</td>
                  <td className="p-3">{d.stop_sequence ?? "—"}</td>
                  <td className="p-3">
                    <DeliveryStatus status={d.status} />
                  </td>
                </tr>
              ))}
            </DataTable>
          </Panel>
        </div>
      ) : null}
    </>
  );
}
