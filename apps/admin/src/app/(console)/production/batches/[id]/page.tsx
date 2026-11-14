import { ApiError, type Batch } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Timeline } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice, Options, WASTE_CATEGORIES } from "@/components/kitchen-ui";
import { TaskRow } from "@/components/task-row";
import { formatDateTime, formatDay, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { batchAction, placeHold, recordCheck, recordWaste, resolveHold, savePackaging } from "../../../kitchen-actions";

export const metadata: Metadata = { title: "Batch" };

/**
 * One batch, laid out for a kitchen tablet: the next action first, in large
 * type, then tasks, quality, packing, waste and the label.
 */
export default async function BatchPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const { id } = await params;
  const b = await api.kitchen.batch(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const [label, units] = await Promise.all([api.kitchen.label(token, id), api.kitchen.units(token)]);
  const exec = can(admin, "production.execute");
  const live = !["completed", "cancelled", "planned"].includes(b.status);
  return (
    <>
      <PageHeader
        back={{ href: "/production/batches", label: "All batches" }}
        actions={
          can(admin, "inventory.read") ? (
            <Link href={`/inventory/batches/${b.id}`} className="type-body-sm inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Ingredients from stock
            </Link>
          ) : null
        }
        title={b.meal_name}
        description={
          <>
            <span className="font-semibold">{b.code}</span> · <StatusTag status={b.status} /> · {formatDay(b.production_date)} · plan{" "}
            <Link href={`/production/plans/${b.plan_id}`} className="underline underline-offset-4">
              {b.plan_number}
            </Link>{" "}
            · recipe{" "}
            <Link href={`/recipes/${b.recipe_id}?v=${b.recipe_version}&step=ingredients`} className="underline underline-offset-4">
              {b.recipe_name} v{b.recipe_version}
            </Link>
            {b.assigned_team ? ` · ${b.assigned_team}` : ""}
          </>
        }
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        <section className="grid gap-4 border border-border bg-surface p-5 sm:grid-cols-4">
          <Figure label="Planned kits" value={b.planned_kits} />
          <Figure label="Accepted kits" value={b.yield?.accepted_kits ?? "—"} />
          <Figure label="Quality" value={humanize(b.quality_status)} />
          <Figure label="Packing" value={humanize(b.packaging_status)} />
        </section>
        {b.status === "quality_hold" ? <Notice tone="error" title="On quality hold: cannot be released for fulfilment" items={(b.holds ?? []).filter((h) => h.status === "open").map((h) => h.reason)} /> : null}
        {b.remaining?.length && b.status !== "quality_hold" ? <Notice tone="info" title="Still to do before this batch is ready" items={b.remaining} /> : null}
        {b.status === "cancelled" ? <Notice tone="warning" title="Cancelled" items={[b.cancel_reason ?? ""]} /> : null}

        {exec ? <NextAction b={b} /> : null}

        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          <Panel title="Kitchen tasks" note={b.tasks?.length ? undefined : "No tasks: none were configured when the plan was released."}>
            <ul className="grid gap-3">
              {(b.tasks ?? []).map((t) => (
                <TaskRow key={t.id} task={t} back={`/production/batches/${b.id}`} canWork={exec && live} />
              ))}
            </ul>
          </Panel>
          <QualityPanel b={b} inspect={can(admin, "quality.inspect")} release={can(admin, "quality.release_hold")} />
        </div>

        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          <Panel title="Packing" note="Packed kits must equal accepted kits; rejected kits are never packed for customers. Verify each label against the approved recipe.">
            {b.packaging ? (
              <p className="type-body-sm mb-3">
                {b.packaging.packed_kits} of {b.packaging.required_kits} packed{b.packaging.packaging_type ? ` in ${b.packaging.packaging_type}` : ""} · labels{" "}
                {b.packaging.labels_verified ? "verified" : "not verified"}
                {b.packaging.completed_at ? ` · finished ${formatDateTime(b.packaging.completed_at)}` : ""}
              </p>
            ) : null}
            {can(admin, "packaging.manage") && live && b.status !== "released" && !b.packaging?.completed_at ? (
              <KForm action={savePackaging} submit="Finish packing" pending="Saving…" label="Packing" size="lg" secondary={{ name: "intent", value: "progress", label: "Save progress" }}>
                <input type="hidden" name="id" value={b.id} />
                <div className="grid gap-4 sm:grid-cols-2">
                  <Labelled label="Kits packed">
                    <TextInput type="number" inputMode="numeric" min={0} name="packed_kits" defaultValue={b.packaging?.packed_kits ?? b.yield?.accepted_kits ?? ""} className="text-[1.25rem]" />
                  </Labelled>
                  <Labelled label="Packaging used">
                    <TextInput name="packaging_type" defaultValue={b.packaging?.packaging_type ?? ""} maxLength={100} />
                  </Labelled>
                </div>
                <label className="type-body inline-flex min-h-11 items-center gap-3">
                  <input type="checkbox" name="labels_verified" defaultChecked={b.packaging?.labels_verified} className="size-6" /> Labels checked against the recipe
                </label>
                <Labelled label="Exceptions">
                  <TextArea name="exceptions" rows={2} defaultValue={b.packaging?.exceptions ?? ""} maxLength={1000} />
                </Labelled>
                <input type="hidden" name="assign_me" value="1" />
              </KForm>
            ) : null}
          </Panel>
          <Panel title="Kit label" note={label.compliance_note}>
            <div className="border-2 border-foreground p-4">
              <p className="font-display text-[1.5rem] leading-tight">{label.meal_name}</p>
              <p className="type-caption mt-1">
                Batch {label.batch_code} · recipe {label.recipe_code} v{label.recipe_version} · produced {formatDay(label.production_date)}
              </p>
              <p className="type-body-sm mt-2">
                <strong>Ingredients:</strong> {label.ingredients.join(", ")}
              </p>
              <p className="type-body-sm mt-1">
                <strong>Contains:</strong> {label.allergens_contains.length ? label.allergens_contains.join(", ") : "no allergens declared in the approved recipe"}
                {label.allergens_may_contain.length ? (
                  <>
                    {" "}
                    · <strong>May contain:</strong> {label.allergens_may_contain.join(", ")}
                  </>
                ) : null}
              </p>
              {label.storage_instructions ? <p className="type-body-sm mt-1">{label.storage_instructions}</p> : null}
              <p className="type-body-sm mt-1">
                <strong>Use by:</strong> {label.use_by ?? label.use_by_note}
              </p>
              <p className="type-caption mt-2">Trace: {label.traceability_id}</p>
            </div>
          </Panel>
        </div>

        <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
          <Panel title="Waste" note="Record what was weighed or counted, as it happens.">
            {b.waste?.length ? (
              <ul className="type-body-sm mb-4 grid gap-1">
                {b.waste.map((w) => (
                  <li key={w.id}>
                    {w.quantity} {w.unit} {w.ingredient_name ?? w.item} · {humanize(w.category)} · {w.reason}
                  </li>
                ))}
              </ul>
            ) : null}
            {can(admin, "waste.record") ? <WasteForm batchId={b.id} units={units.items.map((u) => u.code)} /> : null}
          </Panel>
          <Panel title="History">
            <Timeline
              items={(b.history ?? []).map((h) => ({
                at: h.created_at,
                title: `${h.from ? humanize(h.from) + " → " : ""}${humanize(h.to)}${h.actor_name ? ` · ${h.actor_name}` : ""}`,
                detail: h.note ?? undefined,
              }))}
            />
          </Panel>
        </div>
      </div>
    </>
  );
}

function Figure({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <p className="type-caption font-semibold uppercase tracking-[0.06em] text-muted">{label}</p>
      <p className="mt-1 font-display text-[2.25rem] leading-none">{value}</p>
    </div>
  );
}

function NextAction({ b }: { b: Batch }) {
  const hidden = (action: string) => (
    <>
      <input type="hidden" name="id" value={b.id} />
      <input type="hidden" name="action" value={action} />
    </>
  );
  if (b.status === "released") {
    return (
      <Panel title="Start this batch">
        <KForm action={batchAction} submit="Start batch" pending="Starting…" label="Start batch" size="lg" inline>
          {hidden("start")}
          <Labelled label="Team (optional)">
            <TextInput name="assigned_team" maxLength={60} defaultValue={b.assigned_team} />
          </Labelled>
        </KForm>
      </Panel>
    );
  }
  if (b.status === "in_progress" || b.status === "awaiting_quality_check" || b.status === "quality_hold") {
    const y = b.yield;
    return (
      <div className="grid gap-6 [&>*]:min-w-0 xl:grid-cols-2">
        <Panel title={y ? "Correct the output" : "Record the output"} note="Reworked kits failed and were fixed: they count once, inside the accepted kits. Rejected kits are discarded.">
          <KForm action={batchAction} submit={y ? "Update output" : "Record output"} pending="Saving…" label="Record output" size="lg">
            {hidden("output")}
            <div className="grid grid-cols-3 gap-3">
              <Labelled label="Kits made">
                <TextInput type="number" inputMode="numeric" min={0} name="produced_kits" defaultValue={y?.produced_kits ?? b.planned_kits} className="text-[1.25rem]" required />
              </Labelled>
              <Labelled label="Rejected">
                <TextInput type="number" inputMode="numeric" min={0} name="rejected_kits" defaultValue={y?.rejected_kits ?? 0} className="text-[1.25rem]" />
              </Labelled>
              <Labelled label="Reworked">
                <TextInput type="number" inputMode="numeric" min={0} name="reworked_kits" defaultValue={y?.reworked_kits ?? 0} className="text-[1.25rem]" />
              </Labelled>
            </div>
            <Labelled label="Notes">
              <TextInput name="notes" defaultValue={y?.notes ?? ""} maxLength={1000} />
            </Labelled>
          </KForm>
        </Panel>
        {b.status === "awaiting_quality_check" ? (
          <Panel title="Ready for fulfilment?" note="Allowed only when nothing is left in the list above.">
            <KForm action={batchAction} submit="Complete batch" pending="Completing…" label="Complete batch" size="lg">
              {hidden("complete")}
            </KForm>
          </Panel>
        ) : null}
      </div>
    );
  }
  return null;
}

function QualityPanel({ b, inspect, release }: { b: Batch; inspect: boolean; release: boolean }) {
  const open = (b.holds ?? []).find((h) => h.status === "open");
  const canCheck = inspect && ["in_progress", "awaiting_quality_check", "quality_hold"].includes(b.status);
  return (
    <Panel title="Quality checks" note="The checklist comes from the approved operating procedure. A failed critical check holds the batch; a released hold needs a fresh inspection.">
      {b.checklist?.length ? (
        <ul className="grid gap-4">
          {b.checklist.map((c) => (
            <li key={c.template_id} className="border-b border-border pb-4">
              <p className="type-body font-semibold">
                {c.name} {c.critical ? <StatusTag status="failed" label="Critical" /> : null}{" "}
                {c.result ? <StatusTag status={c.result} label={c.result === "pass" ? "Passed" : "Failed"} /> : <StatusTag status="pending" label="Not checked" />}
              </p>
              <p className="type-caption text-muted">{c.criteria}</p>
              {canCheck ? (
                <div className="mt-2">
                  <KForm action={recordCheck} submit="Pass" pending="…" label={`Check: ${c.name}`} inline secondary={{ name: "result", value: "fail", label: "Fail" }}>
                    <input type="hidden" name="batch_id" value={b.id} />
                    <input type="hidden" name="template_id" value={c.template_id} />
                    <Labelled label="Notes (required to fail)">
                      <TextInput name="notes" maxLength={2000} className="w-64" />
                    </Labelled>
                  </KForm>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <Notice tone="warning" title="No checklist configured" items={["A manager must add quality checks from the approved procedure in Kitchen settings. Batches cannot complete without one."]} />
      )}
      {open && release ? (
        <div className="mt-5 border-l-4 border-error pl-4">
          <p className="type-body-sm font-semibold">Resolve the hold</p>
          <KForm action={resolveHold} submit="Release hold" pending="Saving…" label="Resolve hold" secondary={{ name: "decision", value: "discard", label: "Discard batch" }}>
            <input type="hidden" name="id" value={open.id} />
            <input type="hidden" name="batch_id" value={b.id} />
            <Labelled label="Resolution">
              <TextArea name="notes" rows={2} required maxLength={1000} />
            </Labelled>
          </KForm>
        </div>
      ) : null}
      {inspect && !open && ["in_progress", "awaiting_quality_check"].includes(b.status) ? (
        <details className="mt-5">
          <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Put on hold for another reason</summary>
          <div className="mt-3">
            <KForm action={placeHold} submit="Hold batch" pending="Saving…" label="Hold batch" variant="secondary">
              <input type="hidden" name="batch_id" value={b.id} />
              <Labelled label="Reason">
                <TextInput name="reason" required maxLength={500} />
              </Labelled>
            </KForm>
          </div>
        </details>
      ) : null}
      {(b.checks ?? []).length ? (
        <details className="mt-5">
          <summary className="type-body-sm cursor-pointer underline underline-offset-4">Every inspection ({b.checks!.length})</summary>
          <ul className="type-caption mt-2 grid gap-1">
            {b.checks!.map((c) => (
              <li key={c.id}>
                {formatDateTime(c.created_at)} · {c.name}: {c.result} · {c.inspector_name}
                {c.notes ? ` · ${c.notes}` : ""}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
    </Panel>
  );
}

function WasteForm({ batchId, units }: { batchId: string; units: string[] }) {
  return (
    <KForm action={recordWaste} submit="Record waste" pending="Saving…" label="Record waste" reset>
      <input type="hidden" name="batch_id" value={batchId} />
      <div className="grid gap-3 sm:grid-cols-[1fr_7rem_6rem]">
        <Labelled label="What">
          <TextInput name="item" maxLength={100} required />
        </Labelled>
        <Labelled label="Quantity">
          <TextInput name="quantity" inputMode="decimal" required />
        </Labelled>
        <Labelled label="Unit">
          <SelectInput name="unit" defaultValue="g">
            {units.map((u) => (
              <option key={u}>{u}</option>
            ))}
          </SelectInput>
        </Labelled>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Labelled label="Category">
          <SelectInput name="category" defaultValue="preparation_loss">
            <Options values={WASTE_CATEGORIES} />
          </SelectInput>
        </Labelled>
        <Labelled label="Reason">
          <TextInput name="reason" required maxLength={500} />
        </Labelled>
      </div>
    </KForm>
  );
}
