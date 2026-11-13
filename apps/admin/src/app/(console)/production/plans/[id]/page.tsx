import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { BatchCard, Notice } from "@/components/kitchen-ui";
import { formatDateTime, formatDay, quantity } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { adjustItem, planAction } from "../../../kitchen-actions";

export const metadata: Metadata = { title: "Production plan" };

export default async function PlanPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const { id } = await params;
  const p = await api.kitchen.plan(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const draft = p.status === "draft";
  const canPlan = can(admin, "production.plan") && draft;
  const canApprove = can(admin, "production.approve");
  return (
    <>
      <PageHeader
        back={{ href: "/production/plans", label: "All plans" }}
        title={`Plan ${p.number}`}
        description={
          <>
            <StatusTag status={p.status} /> · delivery week of {formatDay(p.week)} · production {formatDay(p.production_date)} · {p.planned_kits} kits planned for{" "}
            {p.demand_kits} ordered · demand snapshot revision {p.snapshot_revision}
            {p.approved_at ? ` · approved by ${p.approved_by_name} ${formatDateTime(p.approved_at)}` : ""}
            {p.cancel_reason ? ` · cancelled: ${p.cancel_reason}` : ""}
          </>
        }
        actions={
          <Link href={`/production/requirements?plan_id=${p.id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
            Requirements list and export
          </Link>
        }
      />
      <div className="grid gap-6">
        <Notice tone="error" title="Blocking approval" items={p.blocking} />
        <Notice tone="warning" title="For your judgement" items={p.warnings} />
        <Panel
          title="Meals"
          note="Kits ordered come from the snapshot and never change here. A different quantity to make needs a reason, and is audited. Customer orders are not edited."
        >
          <DataTable head={["Meal", "Recipe", "Ordered", "To make", "Reason"]} minWidth="48rem">
            {(p.items ?? []).map((it) => (
              <tr key={it.id}>
                <td className="p-3 font-semibold">
                  {it.meal_name}
                  <span className="type-caption block text-muted">{it.servings_per_kit} servings per kit</span>
                </td>
                <td className="p-3">
                  {it.recipe_id ? (
                    <Link href={`/recipes/${it.recipe_id}?v=${it.recipe_version}&step=ingredients`} className="underline underline-offset-4">
                      {it.recipe_name} v{it.recipe_version}
                    </Link>
                  ) : (
                    <StatusTag status="failed" label="No published recipe" />
                  )}
                </td>
                <td className="p-3">{it.demand_kits}</td>
                <td className="p-3">
                  {canPlan ? (
                    <KForm action={adjustItem} submit="Set" pending="…" label={`Kits to make: ${it.meal_name}`} variant="secondary" inline>
                      <input type="hidden" name="id" value={p.id} />
                      <input type="hidden" name="item" value={it.id} />
                      <Labelled label="Kits">
                        <TextInput type="number" min={0} name="planned_kits" defaultValue={it.planned_kits} className="w-24" />
                      </Labelled>
                      <Labelled label="Reason if different">
                        <TextInput name="reason" defaultValue={it.adjustment_reason ?? ""} maxLength={500} className="w-64" />
                      </Labelled>
                    </KForm>
                  ) : (
                    <strong>{it.planned_kits}</strong>
                  )}
                </td>
                <td className="p-3">{it.adjustment_reason ?? "—"}</td>
              </tr>
            ))}
          </DataTable>
          <p className="type-caption mt-2 text-muted">
            Capacity: {p.capacity?.kits_per_day ? `${p.capacity.kits_per_day} kits a day` : "not recorded"}
            {p.capacity?.note ? ` · ${p.capacity.note}` : ""}
          </p>
        </Panel>
        <Panel
          title="Ingredient requirements"
          note="Per ingredient and preparation, in the ingredient's base unit: quantity × kits × servings per kit ÷ recipe yield, ÷ usable share where recorded. Exact arithmetic, rounded once. Different preparations are never merged."
        >
          <DataTable head={["Ingredient", "Preparation", "Required", "Status", "From"]} minWidth="48rem">
            {(p.requirements ?? []).map((r) => (
              <tr key={r.id}>
                <td className="p-3 font-semibold">{r.ingredient_name}</td>
                <td className="p-3">{r.preparation || "—"}</td>
                <td className="p-3 font-semibold">
                  {quantity(r.quantity, r.unit)}
                  {r.approximate ? <span className="type-caption block font-normal text-muted">Includes an average conversion: weigh before portioning.</span> : null}
                </td>
                <td className="p-3">
                  <StatusTag status={r.status} />
                  {r.note ? <span className="type-caption block text-muted">{r.note}</span> : null}
                </td>
                <td className="type-caption p-3">
                  {r.sources.map((s, i) => (
                    <span key={i} className="block">
                      {s.meal_name} (v{s.recipe_version}
                      {s.batch_code ? `, ${s.batch_code}` : ""}): {s.quantity} {s.unit}
                    </span>
                  ))}
                </td>
              </tr>
            ))}
          </DataTable>
        </Panel>
        {p.batches?.length ? (
          <Panel title="Batches">
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {p.batches.map((b) => (
                <BatchCard key={b.id} batch={b} />
              ))}
            </div>
          </Panel>
        ) : null}
        {canApprove && (p.status === "draft" || p.status === "approved") ? (
          <Panel title={p.status === "draft" ? "Approve" : "Release to the kitchen"}>
            <div className="flex flex-wrap items-start gap-8">
              {p.status === "draft" ? (
                <KForm action={planAction} submit="Approve plan" pending="Approving…" label="Approve plan" size="lg">
                  <input type="hidden" name="id" value={p.id} />
                  <input type="hidden" name="action" value="approve" />
                </KForm>
              ) : (
                <KForm action={planAction} submit="Release to kitchen" pending="Releasing…" label="Release plan" size="lg">
                  <input type="hidden" name="id" value={p.id} />
                  <input type="hidden" name="action" value="release" />
                </KForm>
              )}
              <KForm action={planAction} submit="Cancel plan" pending="Cancelling…" label="Cancel plan" variant="secondary">
                <input type="hidden" name="id" value={p.id} />
                <input type="hidden" name="action" value="cancel" />
                <Labelled label="Reason">
                  <TextInput name="reason" required maxLength={500} />
                </Labelled>
              </KForm>
            </div>
            <p className="type-caption mt-3 text-muted">
              {p.status === "draft"
                ? "Approving freezes the requirements and prepares one batch per meal from its exact recipe version."
                : "Releasing makes the kitchen tasks from the configured templates and moves the week's orders to awaiting production."}
            </p>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
