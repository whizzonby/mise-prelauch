import { ApiError } from "@mise/api-client";
import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag, Tabs, Timeline } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Labelled, Panel, TextArea } from "@/components/commerce-ui";
import { DetailList } from "@/components/panel";
import { formatDate, formatDateTime, formatDay, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { addNote } from "../../crm-actions";

export const metadata: Metadata = { title: "Customer" };

/**
 * The Customer 360: one person across every module, read from where each
 * record lives. Allergies, addresses and phone numbers appear only for
 * staff with customers.sensitive, and opening them is audited.
 */
export default async function CustomerPage({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Promise<{ tab?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "customers.read")) notFound();
  const { id } = await params;
  const { tab = "overview" } = await searchParams;
  const c = await api.admin.customer(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const base = `/customers/${id}`;
  const tabs = [
    { key: "overview", label: "Overview" },
    { key: "orders", label: "Orders" },
    { key: "billing", label: "Billing" },
    { key: "deliveries", label: "Deliveries", hidden: !can(admin, "delivery.read") },
    { key: "preferences", label: "Preferences", hidden: !c.can_view_sensitive },
    { key: "activity", label: "Activity" },
    { key: "notes", label: "Notes" },
    { key: "support", label: "Support", hidden: !can(admin, "cases.read") },
  ];
  const name = `${c.first_name} ${c.last_name}`.trim() || c.email;

  return (
    <>
      <PageHeader
        back={{ href: "/customers", label: "All customers" }}
        title={name}
        description={
          <>
            {c.email} · joined {formatDate(c.joined_at)} · <StatusTag status={c.status === "verified" ? "active" : c.status} label={humanize(c.status)} />
          </>
        }
        actions={
          can(admin, "cases.manage") ? (
            <Link href={`/support/new?customer_id=${id}`} className={buttonClasses("secondary")}>
              Open a support case
            </Link>
          ) : null
        }
      />
      <Tabs tabs={tabs} current={tab} base={base} />
      <div className="mt-6">
        {tab === "overview" ? <Overview c={c} canSubs={can(admin, "subscriptions.read")} /> : null}
        {tab === "orders" ? <Orders token={token} id={id} /> : null}
        {tab === "billing" ? <Billing token={token} id={id} canPayments={can(admin, "payments.read")} /> : null}
        {tab === "deliveries" && can(admin, "delivery.read") ? (
          <Deliveries token={token} id={id} canExceptions={can(admin, "delivery_exceptions.read")} canPartners={can(admin, "delivery_partners.read")} />
        ) : null}
        {tab === "preferences" && c.can_view_sensitive ? <Preferences token={token} id={id} /> : null}
        {tab === "activity" ? <Activity token={token} id={id} /> : null}
        {tab === "notes" ? <Notes token={token} id={id} canWrite={can(admin, "leads.manage")} /> : null}
        {tab === "support" ? <Cases cases={c.cases} /> : null}
      </div>
    </>
  );
}

type C = Awaited<ReturnType<typeof api.admin.customer>>;

function Overview({ c, canSubs }: { c: C; canSubs: boolean }) {
  return (
    <div className="grid gap-6 xl:grid-cols-2">
      <Panel title="Account">
        <DetailList
          items={[
            ["Email confirmed", c.email_verified_at ? formatDateTime(c.email_verified_at) : "Not yet"],
            ["Last sign-in", c.last_login_at ? formatDateTime(c.last_login_at) : "Never"],
            ["Prefers contact by", humanize(c.contact_channel)],
            ["Region", c.region_label ?? "Not given"],
            ["Household", c.household_size ? `${c.household_size} people` : "Not given"],
            ["Dietary preferences", c.dietary.length ? c.dietary.map(humanize).join(", ") : "None given"],
            ["Email topics on", c.marketing_consents.length ? c.marketing_consents.map(humanize).join(", ") : "Account emails only"],
          ]}
        />
      </Panel>
      <Panel title="Acquisition">
        <DetailList
          items={[
            ["Source (first touch)", humanize(c.source)],
            ["Campaign", c.utm_campaign ?? "None"],
            ["Waitlist lead", c.lead_id ? <Link key="l" href={`/leads/${c.lead_id}`} className="underline underline-offset-4">Linked {c.lead_linked_at ? formatDate(c.lead_linked_at) : ""}</Link> : "Joined directly"],
            ["Referred by", c.referred_by_lead_id ? <Link key="r" href={`/leads/${c.referred_by_lead_id}`} className="underline underline-offset-4">A waitlist lead</Link> : "No one"],
            ["Orders", String(c.orders)],
            ["Open support cases", String(c.open_cases)],
          ]}
        />
      </Panel>
      <Panel title="Subscriptions" className="xl:col-span-2">
        {c.subscriptions.length ? (
          <DataTable head={["Status", "Plan", "Price", "First week", "Started"]} minWidth="36rem">
            {c.subscriptions.map((s) => (
              <tr key={s.id}>
                <td className="p-3">
                  {canSubs ? (
                    <Link href={`/subscriptions/${s.id}`} className="underline underline-offset-4">
                      <StatusTag status={s.status} />
                    </Link>
                  ) : (
                    <StatusTag status={s.status} />
                  )}
                </td>
                <td className="p-3">{s.plan_name}</td>
                <td className="p-3">{money(s.amount_minor, s.currency)} a week</td>
                <td className="p-3">{formatDay(s.first_week)}</td>
                <td className="p-3">{s.activated_at ? formatDate(s.activated_at) : "Not started"}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <EmptyState>Never subscribed.</EmptyState>
        )}
      </Panel>
    </div>
  );
}

async function Orders({ token, id }: { token: string; id: string }) {
  const { items } = await api.admin.customerOrders(token, id);
  if (!items.length) return <EmptyState>No orders yet.</EmptyState>;
  return (
    <DataTable head={["Order", "Week", "Status", "Plan", "Meals", "Total"]}>
      {items.map((o) => (
        <tr key={o.id}>
          <td className="p-3">
            <Link href={`/orders/${o.id}`} className="font-semibold underline underline-offset-4">
              {o.number}
            </Link>
          </td>
          <td className="p-3">{formatDay(o.week)}</td>
          <td className="p-3">
            <StatusTag status={o.status} />
          </td>
          <td className="p-3">{o.plan_name}</td>
          <td className="p-3">{o.meals_count}</td>
          <td className="p-3">{money(o.total_minor, o.currency)}</td>
        </tr>
      ))}
    </DataTable>
  );
}

async function Billing({ token, id, canPayments }: { token: string; id: string; canPayments: boolean }) {
  const b = await api.admin.customerBilling(token, id);
  return (
    <div className="grid gap-6">
      <Panel title="Weeks billed">
        {b.billing_cycles.length ? (
          <DataTable head={["Week", "Kind", "Status", "Total", "Invoice"]}>
            {b.billing_cycles.map((x) => (
              <tr key={x.id}>
                <td className="p-3">{formatDay(x.period_start)}</td>
                <td className="p-3">{humanize(x.kind)}</td>
                <td className="p-3">
                  <StatusTag status={x.status} />
                </td>
                <td className="p-3">{money(x.total_minor, x.currency)}</td>
                <td className="p-3">{x.invoice_number ?? ""}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <EmptyState>Nothing billed yet.</EmptyState>
        )}
      </Panel>
      <Panel title="Payments, including failed attempts">
        {b.payments.length ? (
          <DataTable head={["When", "Status", "Kind", "Amount", "Refunded", "Note"]}>
            {b.payments.map((p) => (
              <tr key={p.id}>
                <td className="p-3">
                  {canPayments ? (
                    <Link href={`/payments/${p.id}`} className="underline underline-offset-4">
                      {formatDateTime(p.created_at)}
                    </Link>
                  ) : (
                    formatDateTime(p.created_at)
                  )}
                </td>
                <td className="p-3">
                  <StatusTag status={p.status} />
                </td>
                <td className="p-3">{p.kind === "checkout" ? "Checkout" : "Automatic"}</td>
                <td className="p-3">{money(p.amount_minor, p.currency)}</td>
                <td className="p-3">{p.refunded_minor ? money(p.refunded_minor, p.currency) : ""}</td>
                <td className="p-3">{p.failure_message ?? (p.needs_attention ? p.attention_reason : "")}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <EmptyState>No payments.</EmptyState>
        )}
      </Panel>
    </div>
  );
}

async function Preferences({ token, id }: { token: string; id: string }) {
  const p = await api.admin.customerSensitive(token, id);
  return (
    <div className="grid gap-6 xl:grid-cols-2">
      <Panel title="Allergies" note="Health information. Viewing it is recorded in the audit log.">
        {p.preferences.allergies.length ? (
          <ul className="type-body-sm grid gap-1">
            {p.preferences.allergies.map((a) => (
              <li key={a.allergen}>
                <strong>{a.label}</strong>
                {a.detail ? `: ${a.detail}` : ""}
              </li>
            ))}
          </ul>
        ) : (
          <p className="type-body-sm text-muted">{p.preferences.allergies_reviewed_at ? "None, as of " + formatDate(p.preferences.allergies_reviewed_at) : "Never asked."}</p>
        )}
      </Panel>
      <Panel title="Food preferences">
        <DetailList
          items={[
            ["Dietary", p.preferences.dietary.map(humanize).join(", ") || "None"],
            ["Proteins", p.preferences.proteins.map(humanize).join(", ") || "Any"],
            ["Longest cook time", p.preferences.max_cook_minutes ? `${p.preferences.max_cook_minutes} minutes` : "No limit"],
            ["Other restrictions", p.preferences.other_restrictions ?? "None"],
            ["Household", p.household ? `${p.household.size} people` : "Not given"],
          ]}
        />
      </Panel>
      <Panel title="Contact">
        <DetailList items={[["Phone", p.phone ?? "Not given"], ["Prefers", humanize(p.contact_channel)]]} />
      </Panel>
      <Panel title="Addresses">
        {p.addresses.length ? (
          <ul className="type-body-sm grid gap-3">
            {p.addresses.map((a) => (
              <li key={a.id}>
                <strong>{a.label ?? a.recipient_name}</strong>
                {a.is_default ? " (default)" : ""}
                <span className="block text-muted">{[a.line1, a.line2, a.area, a.city, a.region_label].filter(Boolean).join(", ")}</span>
                {a.instructions ? <span className="block text-muted">Instructions: {a.instructions}</span> : null}
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState>No addresses.</EmptyState>
        )}
      </Panel>
    </div>
  );
}

async function Activity({ token, id }: { token: string; id: string }) {
  const { items } = await api.admin.customerActivity(token, id);
  return (
    <Timeline
      items={items.map((e) => ({
        at: e.at,
        title: humanize(e.action.replace(/\./g, " ")),
        detail: [e.actor_name ? `by ${e.actor_name}` : "by the customer or the system", e.reason ? `reason: ${e.reason}` : ""].filter(Boolean).join(" · "),
      }))}
      empty="No recorded activity."
    />
  );
}

async function Notes({ token, id, canWrite }: { token: string; id: string; canWrite: boolean }) {
  const { items } = await api.admin.notes(token, "customers", id);
  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_24rem]">
      <Timeline items={items.map((n) => ({ at: n.created_at, title: n.author_name || "Staff", detail: <p className="whitespace-pre-line">{n.body}</p> }))} empty="No notes yet." />
      {canWrite ? (
        <Panel title="Add a note" note="Internal. The customer never sees notes.">
          <CatalogForm action={addNote} submit="Add note" pending="Adding…" label="Add a note">
            <input type="hidden" name="subject" value="customer" />
            <input type="hidden" name="id" value={id} />
            <Labelled label="Note">
              <TextArea name="body" rows={4} maxLength={2000} required />
            </Labelled>
          </CatalogForm>
        </Panel>
      ) : null}
    </div>
  );
}

function Cases({ cases }: { cases: C["cases"] }) {
  if (!cases.length) return <EmptyState>No support cases.</EmptyState>;
  return (
    <DataTable head={["Case", "Subject", "Status", "Priority", "Opened"]}>
      {cases.map((k) => (
        <tr key={k.id}>
          <td className="p-3">
            <Link href={`/support/${k.id}`} className="font-semibold underline underline-offset-4">
              #{k.number}
            </Link>
          </td>
          <td className="p-3">{k.subject}</td>
          <td className="p-3">
            <StatusTag status={k.status} />
          </td>
          <td className="p-3">{humanize(k.priority)}</td>
          <td className="p-3">{formatDate(k.created_at)}</td>
        </tr>
      ))}
    </DataTable>
  );
}

/** Sprint 7: every delivery of this customer, its exceptions, and where each stands. */
async function Deliveries({ token, id, canExceptions, canPartners }: { token: string; id: string; canExceptions: boolean; canPartners: boolean }) {
  const [list, exceptions] = await Promise.all([
    api.delivery.deliveries(token, { customer_id: id, status: "all" }),
    canExceptions ? api.delivery.exceptions(token, { customer_id: id, status: "all" }) : Promise.resolve(null),
  ]);
  return (
    <div className="grid gap-6">
      <Panel title="Deliveries" note="Status, attempts and proof come from the delivery records. Open one for its full tracking history.">
        {list.items.length ? (
          <DataTable head={["Delivery", "Order", "Date", ...(canPartners ? ["Partner"] : []), "Attempt", "Proof", "Status"]}>
            {list.items.map((d) => (
              <tr key={d.id}>
                <td className="p-3">
                  <Link href={`/delivery/deliveries/${d.id}`} className="font-semibold underline underline-offset-4">
                    {d.number}
                  </Link>
                </td>
                <td className="p-3">{d.order_number}</td>
                <td className="p-3">{d.service_date ? formatDay(d.service_date) : "Not scheduled"}</td>
                {canPartners ? <td className="p-3">{d.partner_name ?? "—"}</td> : null}
                <td className="p-3">{d.attempt}</td>
                <td className="p-3">{d.has_proof ? "Recorded" : "—"}</td>
                <td className="p-3">
                  <StatusTag status={d.status} />
                </td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <EmptyState>No deliveries yet.</EmptyState>
        )}
      </Panel>
      {exceptions ? (
        <Panel title="Delivery exceptions">
          {exceptions.items.length ? (
            <ul className="type-body-sm grid gap-2">
              {exceptions.items.map((e) => (
                <li key={e.id}>
                  <Link href={`/delivery/exceptions/${e.id}`} className="font-semibold underline underline-offset-4">
                    {e.number}
                  </Link>{" "}
                  · {humanize(e.type)} · {e.summary} · <StatusTag status={e.status} />
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>No exceptions.</EmptyState>
          )}
        </Panel>
      ) : null}
    </div>
  );
}
