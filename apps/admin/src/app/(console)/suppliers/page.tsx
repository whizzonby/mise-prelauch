import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader, Pager, StatusTag, Tabs } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Options } from "@/components/kitchen-ui";
import { CHANNELS, SUPPLIER_TYPES } from "@/components/supply-ui";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createSupplier } from "../supply-actions";

export const metadata: Metadata = { title: "Suppliers" };

type Search = { q?: string; type?: string; status?: string; farmers?: string; tab?: string; page?: string };
const STATUSES = ["draft", "pending_review", "approved", "suspended", "rejected", "inactive"];

/**
 * The supplier directory: farmers, cooperatives, producers, distributors and
 * packaging suppliers. Staff keep these records on the supplier's behalf;
 * only approved suppliers get live purchase orders.
 */
export default async function SuppliersPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "suppliers.read")) notFound();
  const sp = await searchParams;
  const farmers = sp.farmers === "true" || sp.tab === "farmers";
  const page = Math.max(Number(sp.page) || 1, 1);
  const list = await api.supply.suppliers(token, { q: sp.q, type: sp.type, status: sp.status, farmers, page });
  return (
    <>
      <PageHeader
        title={farmers ? "Farmers" : "Supplier directory"}
        description="Suppliers are recorded by staff, so a farmer who works by phone or WhatsApp needs no account. A supplier is approved against the qualification criteria from the business's procedure; until then it gets no purchase orders."
      />
      <div className="grid grid-cols-[minmax(0,1fr)] gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div>
          <Tabs base="/suppliers" current={farmers ? "farmers" : "all"} tabs={[{ key: "all", label: "All suppliers" }, { key: "farmers", label: "Farmers and producers" }]} />
          <div className="mt-4">
            <FilterBar>
              {farmers ? <input type="hidden" name="farmers" value="true" /> : null}
              <Labelled label="Name, code or contact">
                <TextInput name="q" defaultValue={sp.q} maxLength={100} className="w-56" />
              </Labelled>
              <Labelled label="Type">
                <SelectInput name="type" defaultValue={sp.type ?? ""}>
                  <option value="">Any</option>
                  <Options values={SUPPLIER_TYPES} />
                </SelectInput>
              </Labelled>
              <Labelled label="Status">
                <SelectInput name="status" defaultValue={sp.status ?? ""}>
                  <option value="">Any</option>
                  <Options values={STATUSES} />
                </SelectInput>
              </Labelled>
            </FilterBar>
          </div>
          {list.items.length ? (
            <DataTable head={["Supplier", "Type", "Contact", "Products", "Status"]} minWidth="40rem">
              {list.items.map((s) => (
                <tr key={s.id}>
                  <td className="p-3">
                    <Link href={`/suppliers/${s.id}`} className="font-semibold underline underline-offset-4">
                      {s.name}
                    </Link>
                    <span className="type-caption block text-muted">
                      {s.code}
                      {s.region ? ` · ${s.region}` : ""}
                      {s.has_farm ? " · farm profile" : ""}
                    </span>
                  </td>
                  <td className="p-3">{humanize(s.type)}</td>
                  <td className="p-3">
                    {s.contact_name || "—"}
                    <span className="type-caption block text-muted">
                      {s.phone || s.email || "no contact yet"} · prefers {humanize(s.preferred_channel)}
                    </span>
                  </td>
                  <td className="p-3">{s.products}</td>
                  <td className="p-3">
                    <StatusTag status={s.status} />
                    {s.status === "approved" && !s.qualified ? <span className="type-caption block text-error">Approval lapsed</span> : null}
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <EmptyState>No suppliers match. Nothing is invented: suppliers appear once staff record them.</EmptyState>
          )}
          <Pager page={page} size={50} total={list.total} params={{ q: sp.q, type: sp.type, status: sp.status, farmers: farmers ? "true" : undefined }} />
        </div>
        {can(admin, "suppliers.manage") ? (
          <Panel title="Add a supplier" note="Only the name and type are needed now; informal farmers may have little else. Details, the farm profile and products come next.">
            <KForm action={createSupplier} submit="Add supplier" pending="Adding…" label="Add a supplier">
              <Labelled label="Name">
                <TextInput name="name" required maxLength={120} />
              </Labelled>
              <Labelled label="Type">
                <SelectInput name="type" defaultValue={farmers ? "farmer" : ""} required>
                  <option value="" disabled>
                    Choose
                  </option>
                  <Options values={SUPPLIER_TYPES} />
                </SelectInput>
              </Labelled>
              <Labelled label="Contact person">
                <TextInput name="contact_name" maxLength={120} />
              </Labelled>
              <Labelled label="Phone">
                <TextInput name="phone" type="tel" maxLength={40} />
              </Labelled>
              <Labelled label="Email">
                <TextInput name="email" type="email" maxLength={254} />
              </Labelled>
              <Labelled label="Preferred contact">
                <SelectInput name="preferred_channel" defaultValue="phone">
                  <Options values={CHANNELS} />
                </SelectInput>
              </Labelled>
              <Labelled label="Region" hint="Where they are, for example Chaguanas.">
                <TextInput name="region" maxLength={60} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
