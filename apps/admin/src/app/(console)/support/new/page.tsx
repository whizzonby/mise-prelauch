import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createCase } from "../../crm-actions";

export const metadata: Metadata = { title: "New case" };

const CATEGORIES = ["subscription", "billing", "meal_selection", "delivery", "account_access", "product_feedback", "general"];

export default async function NewCasePage({ searchParams }: { searchParams: Promise<{ customer_id?: string; lead_id?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "cases.manage")) notFound();
  const { customer_id = "", lead_id = "" } = await searchParams;
  const customer = customer_id && can(admin, "customers.read") ? await api.admin.customer(token, customer_id).catch(() => null) : null;
  return (
    <>
      <PageHeader title="New support case" back={{ href: "/support", label: "All cases" }} />
      <Panel className="max-w-[44rem]">
        <CatalogForm action={createCase} submit="Open case" pending="Opening…" label="New case">
          <input type="hidden" name="customer_id" value={customer_id} />
          <input type="hidden" name="lead_id" value={lead_id} />
          {customer ? (
            <p className="type-body-sm">
              For <strong>{`${customer.first_name} ${customer.last_name}`.trim()}</strong> ({customer.email})
            </p>
          ) : (
            <p className="type-body-sm text-muted">Not linked to a customer. Open cases from a customer&apos;s profile to link them.</p>
          )}
          <Labelled label="Subject">
            <TextInput name="subject" required maxLength={160} />
          </Labelled>
          <div className="grid gap-4 sm:grid-cols-2">
            <Labelled label="Category">
              <SelectInput name="category" defaultValue="general">
                {CATEGORIES.map((c) => (
                  <option key={c} value={c}>
                    {humanize(c)}
                  </option>
                ))}
              </SelectInput>
            </Labelled>
            <Labelled label="Priority">
              <SelectInput name="priority" defaultValue="normal">
                {["low", "normal", "high", "urgent"].map((p) => (
                  <option key={p} value={p}>
                    {humanize(p)}
                  </option>
                ))}
              </SelectInput>
            </Labelled>
          </div>
          <Labelled label="First note" hint="What the customer said, and what you have done so far.">
            <TextArea name="note" rows={4} maxLength={2000} />
          </Labelled>
          <input type="hidden" name="assigned_to" value={admin.id} />
          <p className="type-caption text-muted">The case is assigned to you; you can reassign it.</p>
        </CatalogForm>
      </Panel>
    </>
  );
}
