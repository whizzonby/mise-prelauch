import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveServiceDay, setCapacity } from "../../delivery-actions";

export const metadata: Metadata = { title: "Delivery calendar" };

/**
 * Approved delivery dates, windows, closures and capacity. Nothing is
 * pre-filled: no weekday pattern is assumed, and a date without capacity
 * shows "unknown", not unlimited.
 */
export default async function CalendarPage({ searchParams }: { searchParams: Promise<{ from?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "delivery_zones.read")) notFound();
  const sp = await searchParams;
  const today = new Date().toISOString().slice(0, 10);
  const from = sp.from && /^\d{4}-\d{2}-\d{2}$/.test(sp.from) ? sp.from : today;
  const [cal, zones, partners] = await Promise.all([api.delivery.calendar(token, from, 42), api.delivery.zones(token), api.delivery.partners(token)]);
  const dates = [...new Set(cal.items.map((d) => d.date))];
  const caps = await Promise.all(dates.map((d) => api.delivery.capacity(token, d)));
  const manage = can(admin, "delivery_zones.manage");
  return (
    <>
      <PageHeader title="Delivery calendar" description={`Six weeks from ${formatDay(from)}. Times are Trinidad and Tobago time. Customers see only approved windows.`} />
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[1.3fr_1fr]">
        <div className="grid content-start gap-4">
          {dates.length ? (
            dates.map((date, i) => (
              <Panel key={date} title={formatDay(date)}>
                <ul className="type-body-sm grid gap-2">
                  {cal.items
                    .filter((d) => d.date === date)
                    .map((d) => (
                      <li key={d.id}>
                        <StatusTag status={d.status === "open" ? "active" : "suspended"} label={d.status === "open" ? "Delivering" : "Closed"} /> {d.zone_code ?? "All zones"}
                        {d.notes ? ` · ${d.notes}` : ""}
                        {d.windows.length ? (
                          <span className="block text-muted">
                            {d.windows.map((w) => `${w.label} ${w.starts_at}–${w.ends_at}${w.status === "approved" ? "" : " (draft, not shown to customers)"}`).join(" · ")}
                          </span>
                        ) : null}
                      </li>
                    ))}
                </ul>
                <p className="type-caption mt-2 text-muted">
                  Capacity:{" "}
                  {caps[i]!.items.length
                    ? caps[i]!.items.map((c) => `${[c.zone_code ?? "all", c.partner_name].filter(Boolean).join("/")} ${c.assigned}/${c.capacity}`).join(" · ")
                    : "unknown (not configured)"}
                </p>
              </Panel>
            ))
          ) : (
            <Empty>No delivery dates are configured. Add an approved date to start scheduling.</Empty>
          )}
        </div>
        {manage ? (
          <div className="grid content-start gap-6 [&>*]:min-w-0">
            <Panel title="Add or change a date" note="An open date for all zones or one zone, or a closure (holiday). Up to three windows.">
              <KForm action={saveServiceDay} submit="Save date" pending="Saving…" label="Save a service day">
                <div className="grid gap-3 sm:grid-cols-3">
                  <Labelled label="Date">
                    <TextInput type="date" name="date" min={today} />
                  </Labelled>
                  <Labelled label="Zone">
                    <SelectInput name="zone_id" defaultValue="">
                      <option value="">All zones</option>
                      {zones.items.map((z) => (
                        <option key={z.id} value={z.id}>
                          {z.code}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Status">
                    <SelectInput name="status">
                      <option value="open">Delivering</option>
                      <option value="closed">Closed</option>
                    </SelectInput>
                  </Labelled>
                </div>
                {[0, 1, 2].map((i) => (
                  <div key={i} className="grid items-end gap-3 sm:grid-cols-[1fr_7rem_7rem_auto]">
                    <Labelled label={`Window ${i + 1} label`}>
                      <TextInput name="window_label" />
                    </Labelled>
                    <Labelled label="From">
                      <TextInput type="time" name="window_start" />
                    </Labelled>
                    <Labelled label="To">
                      <TextInput type="time" name="window_end" />
                    </Labelled>
                    <label className="flex min-h-11 items-center gap-2">
                      <input type="checkbox" name="window_approved" value={i} className="size-5" /> Approved
                    </label>
                  </div>
                ))}
                <Labelled label="Notes">
                  <TextInput name="notes" />
                </Labelled>
              </KForm>
            </Panel>
            <Panel title="Set capacity" note="Deliveries the scope can take. It cannot go below what is already assigned.">
              <KForm action={setCapacity} submit="Save capacity" pending="Saving…" label="Set capacity">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Labelled label="Date">
                    <TextInput type="date" name="date" min={today} />
                  </Labelled>
                  <Labelled label="Deliveries">
                    <TextInput type="number" name="capacity" min={0} />
                  </Labelled>
                  <Labelled label="Zone">
                    <SelectInput name="zone_id" defaultValue="">
                      <option value="">All zones</option>
                      {zones.items.map((z) => (
                        <option key={z.id} value={z.id}>
                          {z.code}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Partner">
                    <SelectInput name="partner_id" defaultValue="">
                      <option value="">Any partner</option>
                      {partners.items.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                </div>
                <Labelled label="Source of the figure">
                  <TextInput name="notes" placeholder="e.g. partner commitment in agreement" />
                </Labelled>
              </KForm>
            </Panel>
          </div>
        ) : null}
      </div>
    </>
  );
}
