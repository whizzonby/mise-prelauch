import { ApiError } from "@mise/api-client";
import { Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { PlanFields } from "@/components/plan-fields";
import { commerceTone, formatDateTime, humanize, money } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { addPrice, setPlanStatus, updatePlan } from "../../commerce-actions";

export const metadata: Metadata = { title: "Plan" };

export default async function PlanPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "subscriptions.read")) notFound();
  const { id } = await params;
  const plan = await api.admin.plan(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const manage = can(admin, "plans.manage");
  return (
    <>
      <Link href="/plans" className="type-body-sm underline underline-offset-4">
        All plans
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Heading as="h1" size="h2">
          {plan.name}
        </Heading>
        <Tag tone={commerceTone(plan.status)}>{humanize(plan.status)}</Tag>
        {plan.is_demo ? <Tag tone="warning">Demo</Tag> : null}
      </div>
      <p className="type-body-sm mt-2 text-muted">{plan.purchasable ? "On sale now." : plan.unavailable_reason}</p>

      <div className="mt-8 grid gap-8 xl:grid-cols-[1fr_24rem]">
        <Panel title="Details">
          {manage ? (
            <CatalogForm action={updatePlan} submit="Save plan" pending="Saving…" label="Plan details">
              <input type="hidden" name="id" value={plan.id} />
              <input type="hidden" name="version" value={plan.version} />
              <PlanFields plan={plan} />
            </CatalogForm>
          ) : (
            <p className="type-body-sm">{plan.summary}</p>
          )}
        </Panel>
        <div className="grid content-start gap-8">
          <Panel title="Prices">
            <DataTable head={["Price", "From"]} minWidth="16rem">
              {(plan.prices ?? []).map((p) => (
                <tr key={p.id}>
                  <td className="p-3">{money(p.amount_minor, p.currency)}</td>
                  <td className="p-3">{formatDateTime(p.effective_from)}</td>
                </tr>
              ))}
            </DataTable>
            {manage ? (
              <div className="mt-5">
                <CatalogForm action={addPrice} submit="Add price version" pending="Adding…" label="New price">
                  <input type="hidden" name="id" value={plan.id} />
                  <Labelled label="Weekly price (TTD)">
                    <TextInput name="amount" inputMode="decimal" placeholder="330.00" required />
                  </Labelled>
                  <Labelled label="Starts" hint="Trinidad and Tobago time. Empty means now.">
                    <TextInput name="effective_from" type="datetime-local" />
                  </Labelled>
                  <Labelled label="Note">
                    <TextInput name="note" maxLength={200} placeholder="Approved by…" />
                  </Labelled>
                </CatalogForm>
              </div>
            ) : null}
          </Panel>
          {manage ? (
            <Panel title="Sale">
              <CatalogForm
                action={setPlanStatus}
                submit={plan.status === "active" ? "Retire plan" : "Activate plan"}
                pending="Updating…"
                label="Plan status"
              >
                <input type="hidden" name="id" value={plan.id} />
                <input type="hidden" name="action" value={plan.status === "active" ? "retire" : "activate"} />
                <p className="type-body-sm text-muted">
                  {plan.status === "active"
                    ? "Retiring stops new sales. Subscribers keep their plan."
                    : "Activating puts the plan on sale at its current price."}
                </p>
              </CatalogForm>
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}
