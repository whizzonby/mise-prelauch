import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { CHECK_TYPES, Field, Options, TASK_KINDS } from "@/components/kitchen-ui";
import { humanize, WEEKDAYS } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveProductionSettings, saveQualityTemplate, saveTaskTemplate } from "../../kitchen-actions";

export const metadata: Metadata = { title: "Kitchen settings" };

/**
 * Configuration that must come from Mise's own approved procedures: quality
 * checks, kitchen tasks, capacity and the weekly schedule. Nothing is
 * pre-filled, because none of it may be invented.
 */
export default async function SettingsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.configure")) notFound();
  const [settings, tasks, checks] = await Promise.all([api.kitchen.settings(token), api.kitchen.taskTemplates(token), api.kitchen.qualityTemplates(token)]);
  return (
    <>
      <PageHeader title="Kitchen settings" description="Enter only what an approved operating procedure says. Changes apply to plans released and checks recorded from now on." />
      <div className="grid gap-6 xl:grid-cols-2">
        <Panel title="Quality checklist" note="Every active check must be recorded for each batch. A failed critical check holds the batch.">
          {checks.items.length ? (
            <ul className="mb-5 grid gap-4">
              {checks.items.map((c) => (
                <li key={c.id} className="border-b border-border pb-4">
                  <details>
                    <summary className="type-body cursor-pointer font-semibold">
                      {c.name} {c.critical ? <StatusTag status="failed" label="Critical" /> : null} {!c.active ? <StatusTag status="retired" label="Inactive" /> : null}
                    </summary>
                    <div className="mt-3">
                      <QualityFields c={c} />
                    </div>
                  </details>
                  <p className="type-caption text-muted">
                    {humanize(c.check_type)}
                    {c.procedure_ref ? ` · ${c.procedure_ref}` : ""}
                  </p>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>No checks yet: batches cannot complete until there is a checklist.</EmptyState>
          )}
          <h3 className="type-body mb-2 mt-4 font-semibold">Add a check</h3>
          <QualityFields />
        </Panel>
        <Panel title="Kitchen task templates" note="Releasing a plan creates one task per active template for each batch.">
          {tasks.items.length ? (
            <ul className="mb-5 grid gap-4">
              {tasks.items.map((t) => (
                <li key={t.id} className="border-b border-border pb-4">
                  <details>
                    <summary className="type-body cursor-pointer font-semibold">
                      {t.name} {!t.active ? <StatusTag status="retired" label="Inactive" /> : null}
                    </summary>
                    <div className="mt-3">
                      <TaskFields t={t} />
                    </div>
                  </details>
                  <p className="type-caption text-muted">
                    {humanize(t.kind)} · {t.priority} priority{t.due_offset_minutes != null ? ` · due ${Math.floor(t.due_offset_minutes / 60)}:${String(t.due_offset_minutes % 60).padStart(2, "0")} on the production day` : ""}
                  </p>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState>No templates yet: released batches will have no tasks.</EmptyState>
          )}
          <h3 className="type-body mb-2 mt-4 font-semibold">Add a task</h3>
          <TaskFields />
        </Panel>
        <Panel title="Capacity and schedule" note="The Monday/Wednesday preparation and Tuesday/Thursday delivery rhythm is the business plan's proposal. Mark it approved only when the business confirms it.">
          <KForm action={saveProductionSettings} submit="Save" pending="Saving…" label="Capacity and schedule">
            <Field>
              <Labelled label="Kits the kitchen can make in a day" hint="Blank: not recorded.">
                <TextInput type="number" min={0} name="capacity_kits_per_day" defaultValue={settings.capacity_kits_per_day ?? ""} />
              </Labelled>
              <Labelled label="How this was measured">
                <TextInput name="capacity_note" defaultValue={settings.capacity_note} maxLength={500} />
              </Labelled>
            </Field>
            {settings.schedule.map((s) => (
              <Labelled key={s.id} label={`${WEEKDAYS[s.prep_weekday]} preparation → ${WEEKDAYS[s.delivery_weekday]} delivery`}>
                <SelectInput name={`slot:${s.id}`} defaultValue={s.status}>
                  <option value="proposed">Proposed (not approved)</option>
                  <option value="approved">Approved by the business</option>
                  <option value="retired">Not used</option>
                </SelectInput>
              </Labelled>
            ))}
          </KForm>
        </Panel>
      </div>
    </>
  );
}

function QualityFields({ c }: { c?: Awaited<ReturnType<typeof api.kitchen.qualityTemplates>>["items"][number] }) {
  return (
    <KForm action={saveQualityTemplate} submit={c ? "Save check" : "Add check"} pending="Saving…" label={c ? `Edit ${c.name}` : "Add a quality check"} reset={!c}>
      {c ? <input type="hidden" name="id" value={c.id} /> : null}
      <Field>
        <Labelled label="Name">
          <TextInput name="name" defaultValue={c?.name} required maxLength={100} />
        </Labelled>
        <Labelled label="Type">
          <SelectInput name="check_type" defaultValue={c?.check_type ?? "general_inspection"}>
            <Options values={CHECK_TYPES} />
          </SelectInput>
        </Labelled>
      </Field>
      <Labelled label="What the inspector checks" hint="As written in the approved procedure.">
        <TextArea name="criteria" rows={3} defaultValue={c?.criteria} required maxLength={2000} />
      </Labelled>
      <Field>
        <Labelled label="Procedure reference">
          <TextInput name="procedure_ref" defaultValue={c?.procedure_ref} maxLength={200} />
        </Labelled>
        <Labelled label="Order">
          <TextInput type="number" name="position" defaultValue={c?.position ?? 0} />
        </Labelled>
      </Field>
      <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
        <input type="checkbox" name="critical" defaultChecked={c?.critical} className="size-5" /> Critical: a failure holds the batch
      </label>
      {c ? (
        <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
          <input type="checkbox" name="active" defaultChecked={c.active} className="size-5" /> Active
        </label>
      ) : null}
    </KForm>
  );
}

function TaskFields({ t }: { t?: Awaited<ReturnType<typeof api.kitchen.taskTemplates>>["items"][number] }) {
  return (
    <KForm action={saveTaskTemplate} submit={t ? "Save task" : "Add task"} pending="Saving…" label={t ? `Edit ${t.name}` : "Add a task template"} reset={!t}>
      {t ? <input type="hidden" name="id" value={t.id} /> : null}
      <Field>
        <Labelled label="Name">
          <TextInput name="name" defaultValue={t?.name} required maxLength={100} />
        </Labelled>
        <Labelled label="Kind">
          <SelectInput name="kind" defaultValue={t?.kind ?? "preparation"}>
            <Options values={TASK_KINDS} />
          </SelectInput>
        </Labelled>
        <Labelled label="Priority">
          <SelectInput name="priority" defaultValue={t?.priority ?? "normal"}>
            <option value="low">Low</option>
            <option value="normal">Normal</option>
            <option value="high">High</option>
          </SelectInput>
        </Labelled>
        <Labelled label="Due (minutes after midnight of the production day)" hint="Blank: no due time. 600 = 10:00.">
          <TextInput type="number" min={0} name="due_offset_minutes" defaultValue={t?.due_offset_minutes ?? ""} />
        </Labelled>
        <Labelled label="Order">
          <TextInput type="number" name="position" defaultValue={t?.position ?? 0} />
        </Labelled>
      </Field>
      <Labelled label="Instructions">
        <TextArea name="instructions" rows={2} defaultValue={t?.instructions} maxLength={1000} />
      </Labelled>
      {t ? (
        <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
          <input type="checkbox" name="active" defaultChecked={t.active} className="size-5" /> Active
        </label>
      ) : null}
    </KForm>
  );
}
