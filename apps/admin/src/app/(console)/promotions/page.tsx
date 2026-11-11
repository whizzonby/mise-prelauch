import { Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { commerceTone, formatDate, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createPromotion, setPromotionStatus } from "../commerce-actions";

export const metadata: Metadata = { title: "Promotions" };

/**
 * Promotion codes. Created as drafts; they apply only once someone with
 * this permission activates them. The business plan's early-bird offer is
 * not set up until it is approved.
 */
export default async function PromotionsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "promotions.manage")) notFound();
  const [{ items }, plans] = await Promise.all([
    api.admin.promotions(token),
    can(admin, "subscriptions.read") ? api.admin.plans(token) : Promise.resolve({ items: [] }),
  ]);
  return (
    <>
      <Heading as="h1" size="h2">
        Promotions
      </Heading>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        Discounts are calculated by the API from the code; the customer app never sends an amount.
      </Text>
      <div className="mt-6">
        {items.length ? (
          <DataTable head={["Code", "Offer", "Status", "Used", "Valid", ""]}>
            {items.map((p) => (
              <tr key={p.id}>
                <td className="p-3 font-mono">{p.code}</td>
                <td className="p-3">
                  {p.name}
                  <span className="type-caption block text-muted">{p.description}</span>
                </td>
                <td className="p-3">
                  <Tag tone={commerceTone(p.status)}>{humanize(p.status)}</Tag>
                </td>
                <td className="p-3">
                  {p.redemptions}
                  {p.max_redemptions ? ` of ${p.max_redemptions}` : ""}
                </td>
                <td className="p-3">
                  {p.valid_from ? formatDate(p.valid_from) : "Now"} – {p.valid_until ? formatDate(p.valid_until) : "no end"}
                </td>
                <td className="p-3">
                  {p.status !== "archived" ? (
                    <CatalogForm
                      action={setPromotionStatus}
                      submit={p.status === "draft" ? "Activate" : "Archive"}
                      pending="…"
                      label={`Change ${p.code}`}
                    >
                      <input type="hidden" name="id" value={p.id} />
                      <input type="hidden" name="action" value={p.status === "draft" ? "activate" : "archive"} />
                    </CatalogForm>
                  ) : null}
                </td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No promotions. None are created automatically.</Empty>
        )}
      </div>
      <Panel title="New promotion" className="mt-10 max-w-[52rem]">
        <CatalogForm action={createPromotion} submit="Create draft" pending="Creating…" label="New promotion">
          <div className="grid gap-4 sm:grid-cols-2">
            <Labelled label="Code" hint="Capital letters, numbers and hyphens.">
              <TextInput name="code" required maxLength={30} className="uppercase" />
            </Labelled>
            <Labelled label="Name">
              <TextInput name="name" required maxLength={80} />
            </Labelled>
          </div>
          <div className="grid gap-4 sm:grid-cols-4">
            <Labelled label="Kind">
              <SelectInput name="kind" defaultValue="percent">
                <option value="percent">Percentage</option>
                <option value="fixed">Fixed amount (TTD)</option>
              </SelectInput>
            </Labelled>
            <Labelled label="Value" hint="15 for 15%, or 50.00">
              <TextInput name="value" inputMode="decimal" required />
            </Labelled>
            <Labelled label="Weeks it applies to">
              <TextInput name="cycles" type="number" min={1} max={52} defaultValue={1} />
            </Labelled>
            <Labelled label="Use limit" hint="Empty: no limit">
              <TextInput name="max_redemptions" type="number" min={1} />
            </Labelled>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Labelled label="Valid from">
              <TextInput name="valid_from" type="datetime-local" />
            </Labelled>
            <Labelled label="Valid until">
              <TextInput name="valid_until" type="datetime-local" />
            </Labelled>
          </div>
          {plans.items.length ? (
            <fieldset>
              <legend className="type-label">Plans (none ticked: every plan)</legend>
              <div className="mt-2 flex flex-wrap gap-4">
                {plans.items.map((p) => (
                  <label key={p.id} className="type-body-sm flex items-center gap-2">
                    <input type="checkbox" name="plan_ids" value={p.id} className="size-4" />
                    {p.name}
                  </label>
                ))}
              </div>
            </fieldset>
          ) : null}
        </CatalogForm>
      </Panel>
    </>
  );
}
