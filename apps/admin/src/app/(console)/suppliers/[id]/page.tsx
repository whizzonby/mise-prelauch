import { ApiError, type Supplier } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag, Tabs, Timeline } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Field, Notice, Options } from "@/components/kitchen-ui";
import { CHANNELS, CONTACT_CHANNELS, SUPPLIER_TYPES } from "@/components/supply-ui";
import { formatDate, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { addDocument, createProduct, logCommunication, reviewSupplier, saveFarm, supplierAction, updateSupplier, verifyDocument } from "../../supply-actions";

export const metadata: Metadata = { title: "Supplier" };

const FARM_TYPES = ["farmer", "cooperative", "producer"];

export default async function SupplierPage({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Promise<{ tab?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "suppliers.read")) notFound();
  const { id } = await params;
  const { tab = "overview" } = await searchParams;
  const s = await api.supply.supplier(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const manage = can(admin, "suppliers.manage");
  const farm = FARM_TYPES.includes(s.type);
  return (
    <>
      <PageHeader
        back={{ href: "/suppliers", label: "All suppliers" }}
        title={s.name}
        description={
          <>
            {s.code} · {humanize(s.type)} · <StatusTag status={s.status} />
            {s.status === "approved" && !s.qualified ? <> · approval lapsed on {formatDate(s.review_expires_on ?? "")}</> : null}
            {s.review_expires_on && s.qualified ? <> · approval valid until {formatDate(s.review_expires_on)}</> : null}
          </>
        }
      />
      <Tabs
        base={`/suppliers/${id}`}
        current={tab}
        tabs={[
          { key: "overview", label: "Overview" },
          { key: "farm", label: "Farm", hidden: !farm },
          { key: "products", label: `Products (${s.products})` },
          { key: "documents", label: `Documents (${s.documents?.length ?? 0})` },
          { key: "contact", label: "Contact log" },
          { key: "history", label: "Approval history" },
        ]}
      />
      <div className="mt-6 grid gap-6 [&>*]:min-w-0">
        {tab === "overview" ? <Overview s={s} manage={manage} approve={can(admin, "suppliers.approve")} /> : null}
        {tab === "farm" && farm ? <Farm s={s} manage={manage} /> : null}
        {tab === "products" ? <Products s={s} token={token} manage={can(admin, "supplier_products.manage")} prices={can(admin, "supplier_products.read")} /> : null}
        {tab === "documents" ? <Documents s={s} manage={manage} verify={can(admin, "suppliers.approve")} /> : null}
        {tab === "contact" ? <Contact s={s} manage={manage} /> : null}
        {tab === "history" ? <History s={s} /> : null}
      </div>
    </>
  );
}

function Overview({ s, manage, approve }: { s: Supplier; manage: boolean; approve: boolean }) {
  const hidden = (action: string) => (
    <>
      <input type="hidden" name="id" value={s.id} />
      <input type="hidden" name="action" value={action} />
    </>
  );
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[minmax(0,1fr)_26rem]">
      <Panel title="Details" note="Only what the supplier told us. Payment terms are as agreed; empty means not agreed yet.">
        {manage ? (
          <KForm action={updateSupplier} submit="Save details" pending="Saving…" label="Supplier details">
            <input type="hidden" name="id" value={s.id} />
            <Field>
              <Labelled label="Registered or trading name">
                <TextInput name="name" defaultValue={s.name} required maxLength={120} />
              </Labelled>
              <Labelled label="Also known as">
                <TextInput name="trading_name" defaultValue={s.trading_name} maxLength={120} />
              </Labelled>
              <Labelled label="Type" hint="Changing the type of an approved supplier sends it for a new review.">
                <SelectInput name="type" defaultValue={s.type}>
                  <Options values={SUPPLIER_TYPES} />
                </SelectInput>
              </Labelled>
              <Labelled label="Contact person">
                <TextInput name="contact_name" defaultValue={s.contact_name} maxLength={120} />
              </Labelled>
              <Labelled label="Phone">
                <TextInput name="phone" type="tel" defaultValue={s.phone} maxLength={40} />
              </Labelled>
              <Labelled label="Email">
                <TextInput name="email" type="email" defaultValue={s.email} maxLength={254} />
              </Labelled>
              <Labelled label="Preferred contact">
                <SelectInput name="preferred_channel" defaultValue={s.preferred_channel}>
                  <Options values={CHANNELS} />
                </SelectInput>
              </Labelled>
              <Labelled label="Region">
                <TextInput name="region" defaultValue={s.region} maxLength={60} />
              </Labelled>
            </Field>
            <Labelled label="Business location">
              <TextInput name="location" defaultValue={s.location} maxLength={300} />
            </Labelled>
            <Labelled label="Operating regions" hint="Separated by commas.">
              <TextInput name="operating_regions" defaultValue={s.operating_regions.join(", ")} />
            </Labelled>
            <Labelled label="Payment terms (as agreed)">
              <TextInput name="payment_terms" defaultValue={s.payment_terms} maxLength={300} />
            </Labelled>
            <Labelled label="Delivery arrangements">
              <TextArea name="delivery_arrangements" rows={2} defaultValue={s.delivery_arrangements} maxLength={500} />
            </Labelled>
            <Labelled label="Internal notes">
              <TextArea name="notes" rows={3} defaultValue={s.notes} maxLength={2000} />
            </Labelled>
          </KForm>
        ) : (
          <dl className="type-body-sm grid gap-2">
            <div>Contact: {s.contact_name || "—"} · {s.phone || "no phone"} · {s.email || "no email"} · prefers {humanize(s.preferred_channel)}</div>
            <div>Region: {s.region || "—"} · operates in {s.operating_regions.join(", ") || "—"}</div>
            <div>Payment terms: {s.payment_terms || "not agreed"}</div>
            <div>Delivery: {s.delivery_arrangements || "—"}</div>
          </dl>
        )}
      </Panel>
      <div className="grid content-start gap-6 [&>*]:min-w-0">
        <Notice tone="warning" title="Before this supplier can be approved" items={s.missing ?? []} />
        {manage && (s.status === "draft" || s.status === "rejected") ? (
          <Panel title="Send for review" note="Someone other than you approves it, against the criteria for this type of supplier.">
            <KForm action={supplierAction} submit="Submit for review" pending="Sending…" label="Submit for review">
              {hidden("submit")}
              <Labelled label="Note to the reviewer (optional)">
                <TextArea name="note" rows={2} maxLength={500} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
        {approve && s.status === "pending_review" ? (
          <Panel title="Qualification review" note="Record each criterion from the approved procedure. Approval needs every required one met; you cannot review a supplier you submitted.">
            <KForm action={reviewSupplier} submit="Record review" pending="Saving…" label="Qualification review">
              <input type="hidden" name="id" value={s.id} />
              {(s.criteria ?? []).map((c) => (
                <fieldset key={c.id} className="grid gap-2 border-b border-border pb-3">
                  <input type="hidden" name="criterion_id" value={c.id} />
                  <legend className="type-body-sm font-semibold">
                    {c.name} {c.required ? <StatusTag status="pending" label="Required" /> : null}
                  </legend>
                  {c.description ? <p className="type-caption text-muted">{c.description}</p> : null}
                  <div className="flex gap-4">
                    <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                      <input type="radio" name={`met_${c.id}`} value="yes" className="size-5" /> Met
                    </label>
                    <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                      <input type="radio" name={`met_${c.id}`} value="no" defaultChecked className="size-5" /> Not met
                    </label>
                  </div>
                  <TextInput name={`note_${c.id}`} placeholder="Evidence or note" aria-label={`Note: ${c.name}`} maxLength={500} />
                </fieldset>
              ))}
              <Labelled label="Decision">
                <SelectInput name="decision" defaultValue="approved">
                  <option value="approved">Approve</option>
                  <option value="changes_requested">Send back as a draft</option>
                  <option value="rejected">Reject</option>
                </SelectInput>
              </Labelled>
              <Labelled label="Notes" hint="Required to send back or reject.">
                <TextArea name="notes" rows={2} maxLength={2000} />
              </Labelled>
              <Labelled label="Approval valid until (optional)" hint="When the procedure requires periodic review.">
                <TextInput type="date" name="expires_on" />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
        {approve && s.status === "approved" ? (
          <Panel title="Suspend" note="Stops new orders. Purchasing is alerted to open orders.">
            <KForm action={supplierAction} submit="Suspend supplier" pending="Saving…" label="Suspend supplier" variant="secondary">
              {hidden("suspend")}
              <Labelled label="Reason">
                <TextInput name="reason" required maxLength={500} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
        {approve && (s.status === "suspended" || s.status === "inactive") ? (
          <Panel title="Reactivate" note={s.status === "suspended" ? "Sends the supplier for a new review." : "Reopens the record as a draft."}>
            <KForm action={supplierAction} submit="Reactivate" pending="Saving…" label="Reactivate supplier" variant="secondary">
              {hidden("reactivate")}
              <Labelled label="Reason">
                <TextInput name="reason" required maxLength={500} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
        {approve && s.status !== "inactive" ? (
          <details>
            <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Deactivate this supplier</summary>
            <div className="mt-3">
              <KForm action={supplierAction} submit="Deactivate" pending="Saving…" label="Deactivate supplier" variant="secondary">
                {hidden("deactivate")}
                <Labelled label="Reason">
                  <TextInput name="reason" required maxLength={500} />
                </Labelled>
              </KForm>
            </div>
          </details>
        ) : null}
      </div>
    </div>
  );
}

function Farm({ s, manage }: { s: Supplier; manage: boolean }) {
  const f = s.farm;
  return (
    <Panel
      title="Farm profile"
      note={
        f
          ? `Recorded by ${f.recorded_by_name || "staff"} on the farmer's behalf, ${formatDateTime(f.updated_at)}${f.information_source ? ` (${f.information_source})` : ""}. Estimates here are not guaranteed supply.`
          : "Nothing recorded yet. Every field is optional."
      }
    >
      {manage ? (
        <KForm action={saveFarm} submit="Save farm profile" pending="Saving…" label="Farm profile">
          <input type="hidden" name="id" value={s.id} />
          <Field>
            <Labelled label="Farm name">
              <TextInput name="farm_name" defaultValue={f?.farm_name} maxLength={120} />
            </Labelled>
            <Labelled label="Farm location or region">
              <TextInput name="farm_location" defaultValue={f?.farm_location} maxLength={300} />
            </Labelled>
          </Field>
          <Labelled label="Crops produced" hint="Separated by commas.">
            <TextInput name="crops" defaultValue={f?.crops.join(", ")} />
          </Labelled>
          <Field>
            <Labelled label="Growing seasons">
              <TextArea name="growing_seasons" rows={2} defaultValue={f?.growing_seasons} maxLength={500} />
            </Labelled>
            <Labelled label="Harvest windows">
              <TextArea name="harvest_windows" rows={2} defaultValue={f?.harvest_windows} maxLength={500} />
            </Labelled>
            <Labelled label="Estimated supply capacity" hint="As the farmer estimates it.">
              <TextArea name="estimated_capacity" rows={2} defaultValue={f?.estimated_capacity} maxLength={500} />
            </Labelled>
            <Labelled label="Collection arrangements">
              <TextArea name="collection_arrangements" rows={2} defaultValue={f?.collection_arrangements} maxLength={500} />
            </Labelled>
          </Field>
          <Labelled label="Where this information came from" hint="For example, a phone call on 12 November.">
            <TextInput name="information_source" defaultValue={f?.information_source} maxLength={300} />
          </Labelled>
        </KForm>
      ) : f ? (
        <dl className="type-body-sm grid gap-2">
          <div>
            {f.farm_name || "—"} · {f.farm_location || "—"}
          </div>
          <div>Crops: {f.crops.join(", ") || "—"}</div>
          <div>Harvest: {f.harvest_windows || "—"}</div>
          <div>Capacity (estimate): {f.estimated_capacity || "—"}</div>
        </dl>
      ) : (
        <EmptyState>No farm profile.</EmptyState>
      )}
      <p className="type-caption mt-3 text-muted">Certifications are documents, and count only once someone has verified them. Nothing here marks a farm organic or certified.</p>
    </Panel>
  );
}

async function Products({ s, token, manage, prices }: { s: Supplier; token: string; manage: boolean; prices: boolean }) {
  const [products, ingredients, units] = await Promise.all([
    prices ? api.supply.products(token, { supplier_id: s.id }) : Promise.resolve({ items: [] }),
    manage ? api.kitchen.ingredients(token, { active: "true", page_size: 200 }) : Promise.resolve(null),
    manage ? api.kitchen.units(token) : Promise.resolve(null),
  ]);
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[minmax(0,1fr)_24rem]">
      <Panel title="Products" note="Each product maps to one catalogue ingredient (or is a packaging item). A mapping counts only once someone confirms it.">
        {products.items.length ? (
          <DataTable head={["Product", "Ingredient", "Pack", "Price per pack", "Mapping"]} minWidth="40rem">
            {products.items.map((p) => (
              <tr key={p.id}>
                <td className="p-3">
                  <Link href={`/supplier-products/${p.id}`} className="font-semibold underline underline-offset-4">
                    {p.name}
                  </Link>
                  <span className="type-caption block text-muted">{[p.sku, p.grade].filter(Boolean).join(" · ") || "—"}</span>
                </td>
                <td className="p-3">{p.ingredient_name ?? "Packaging"}</td>
                <td className="p-3">
                  {p.pack_quantity} {p.pack_unit}
                  {p.pack_description ? <span className="type-caption block text-muted">{p.pack_description}</span> : null}
                </td>
                <td className="p-3">{p.price ? money(p.price.amount_minor, p.price.currency) : <span className="text-muted">No price</span>}</td>
                <td className="p-3">
                  <StatusTag status={p.mapping_status} />
                </td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <EmptyState>No products recorded.</EmptyState>
        )}
      </Panel>
      {manage && ingredients && units ? (
        <Panel title="Add a product" note="The pack must convert to the ingredient's base unit. Prices are added on the product page.">
          <KForm action={createProduct} submit="Add product" pending="Adding…" label="Add a product">
            <input type="hidden" name="supplier_id" value={s.id} />
            <Labelled label="Product name" hint="As the supplier calls it.">
              <TextInput name="name" required maxLength={160} />
            </Labelled>
            <Labelled label="Kind">
              <SelectInput name="kind" defaultValue="ingredient">
                <option value="ingredient">Ingredient</option>
                <option value="packaging">Packaging item</option>
              </SelectInput>
            </Labelled>
            <Labelled label="Catalogue ingredient">
              <SelectInput name="ingredient_id" defaultValue="">
                <option value="">—</option>
                {ingredients.items.map((i) => (
                  <option key={i.id} value={i.id}>
                    {i.name} ({i.base_unit})
                  </option>
                ))}
              </SelectInput>
            </Labelled>
            <div className="grid grid-cols-2 gap-3">
              <Labelled label="Pack size">
                <TextInput name="pack_quantity" inputMode="decimal" required />
              </Labelled>
              <Labelled label="Unit">
                <SelectInput name="pack_unit" defaultValue="kg">
                  {units.items.map((u) => (
                    <option key={u.code}>{u.code}</option>
                  ))}
                </SelectInput>
              </Labelled>
            </div>
            <Labelled label="Pack description" hint="For example, 10 kg crate.">
              <TextInput name="pack_description" maxLength={120} />
            </Labelled>
            <div className="grid grid-cols-2 gap-3">
              <Labelled label="SKU">
                <TextInput name="sku" maxLength={60} />
              </Labelled>
              <Labelled label="Lead time (days)">
                <TextInput type="number" name="lead_time_days" min={0} max={365} />
              </Labelled>
            </div>
          </KForm>
        </Panel>
      ) : null}
    </div>
  );
}

function Documents({ s, manage, verify }: { s: Supplier; manage: boolean; verify: boolean }) {
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[minmax(0,1fr)_24rem]">
      <Panel title="Documents" note="Details only: files are not stored. A certification counts only once someone verified it against its source.">
        {s.documents?.length ? (
          <ul className="grid gap-3">
            {s.documents.map((d) => (
              <li key={d.id} className="border-b border-border pb-3">
                <p className="type-body font-semibold">
                  {d.title} <StatusTag status={d.verified_by_name ? "passed" : "pending"} label={d.verified_by_name ? `Verified by ${d.verified_by_name}` : "Not verified"} />
                  {d.expired ? <StatusTag status="expired" label="Expired" /> : null}
                </p>
                <p className="type-caption text-muted">
                  {humanize(d.kind)}
                  {d.issued_by ? ` · issued by ${d.issued_by}` : ""}
                  {d.reference ? ` · ${d.reference}` : ""}
                  {d.expires_on ? ` · expires ${formatDate(d.expires_on)}` : ""}
                </p>
                {verify && !d.verified_by_name ? (
                  <div className="mt-2">
                    <KForm action={verifyDocument} submit="Mark verified" pending="…" label={`Verify ${d.title}`} variant="secondary" inline>
                      <input type="hidden" name="id" value={s.id} />
                      <input type="hidden" name="document_id" value={d.id} />
                    </KForm>
                  </div>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState>No documents recorded.</EmptyState>
        )}
      </Panel>
      {manage ? (
        <Panel title="Record a document">
          <KForm action={addDocument} submit="Record document" pending="Saving…" label="Record a document" reset>
            <input type="hidden" name="id" value={s.id} />
            <Labelled label="Kind">
              <SelectInput name="kind" defaultValue="certification">
                <Options values={["certification", "licence", "inspection_report", "agreement", "other"]} />
              </SelectInput>
            </Labelled>
            <Labelled label="Title">
              <TextInput name="title" required maxLength={200} />
            </Labelled>
            <Labelled label="Issued by">
              <TextInput name="issued_by" maxLength={200} />
            </Labelled>
            <Labelled label="Reference or where it is kept">
              <TextInput name="reference" maxLength={300} />
            </Labelled>
            <Labelled label="Expires on">
              <TextInput type="date" name="expires_on" />
            </Labelled>
          </KForm>
        </Panel>
      ) : null}
    </div>
  );
}

function Contact({ s, manage }: { s: Supplier; manage: boolean }) {
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-[minmax(0,1fr)_24rem]">
      <Panel title="Contact log" note="What staff discussed with the supplier. Mise sends no messages itself.">
        <Timeline
          empty="Nothing recorded yet."
          items={(s.communications ?? []).map((c) => ({
            at: c.created_at,
            title: `${humanize(c.channel)} · ${c.direction === "outbound" ? "we contacted them" : "they contacted us"} · ${c.recorded_by_name}`,
            detail: <p className="whitespace-pre-line">{c.summary}</p>,
          }))}
        />
      </Panel>
      {manage ? (
        <Panel title="Record a contact">
          <KForm action={logCommunication} submit="Record" pending="Saving…" label="Record a contact" reset>
            <input type="hidden" name="id" value={s.id} />
            <div className="grid grid-cols-2 gap-3">
              <Labelled label="Channel">
                <SelectInput name="channel" defaultValue={s.preferred_channel}>
                  <Options values={CONTACT_CHANNELS} />
                </SelectInput>
              </Labelled>
              <Labelled label="Direction">
                <SelectInput name="direction" defaultValue="outbound">
                  <option value="outbound">We contacted them</option>
                  <option value="inbound">They contacted us</option>
                </SelectInput>
              </Labelled>
            </div>
            <Labelled label="What was said">
              <TextArea name="summary" rows={3} required maxLength={2000} />
            </Labelled>
          </KForm>
        </Panel>
      ) : null}
    </div>
  );
}

function History({ s }: { s: Supplier }) {
  return (
    <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
      <Panel title="Status changes">
        <Timeline
          items={(s.history ?? []).map((h) => ({
            at: h.created_at,
            title: `${h.from ? humanize(h.from) + " → " : ""}${humanize(h.to)}${h.actor_name ? ` · ${h.actor_name}` : ""}`,
            detail: h.note ?? undefined,
          }))}
        />
      </Panel>
      <Panel title="Reviews">
        <Timeline
          empty="No reviews yet."
          items={(s.reviews ?? []).map((r) => ({
            at: r.created_at,
            title: `${humanize(r.decision)} · ${r.reviewer_name}${r.expires_on ? ` · valid until ${formatDate(r.expires_on)}` : ""}`,
            detail: (
              <ul className="grid gap-1">
                {r.results.map((x) => (
                  <li key={x.criterion_id}>
                    {x.met ? "Met" : "Not met"}: {x.name}
                    {x.note ? ` (${x.note})` : ""}
                  </li>
                ))}
                {r.notes ? <li className="italic">{r.notes}</li> : null}
              </ul>
            ),
          }))}
        />
      </Panel>
    </div>
  );
}
