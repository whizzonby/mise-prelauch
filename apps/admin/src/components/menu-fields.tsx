import type { AdminMeal } from "@mise/api-client";

import { Labelled, TextArea, TextInput } from "@/components/commerce-ui";
import { localInputValue } from "@/lib/format";

/**
 * The meals and settings of a weekly menu. Only published meals can be on a
 * published menu; drafts are listed so a menu can be prepared ahead.
 */
export function MenuFields({
  meals,
  plans,
  selected,
  value,
  locked,
}: {
  meals: Pick<AdminMeal, "id" | "name" | "status" | "is_demo">[];
  plans: { id: string; name: string }[];
  selected: Map<string, { max: number | null; chosen: boolean }>;
  value?: { week: string; title: string | null; note: string | null; cutoff_at: string; eligible_plan_ids: string[] };
  /** Published: the cutoff is fixed and chosen meals stay. */
  locked?: boolean;
}) {
  return (
    <>
      <div className="grid gap-4 sm:grid-cols-3">
        <Labelled label="Delivery week (its Monday)">
          <TextInput name="week" type="date" defaultValue={value?.week} required disabled={!!value} />
        </Labelled>
        <Labelled label="Title" hint="Optional, e.g. Divali week.">
          <TextInput name="title" defaultValue={value?.title ?? ""} maxLength={80} />
        </Labelled>
        <Labelled label="Selection closes" hint={locked ? "Fixed once published." : "Empty: the usual Thursday 23:59 before the week."}>
          <TextInput name="cutoff_at" type="datetime-local" defaultValue={localInputValue(value?.cutoff_at)} disabled={locked} />
        </Labelled>
      </div>
      {locked && value ? <input type="hidden" name="cutoff_at" value={localInputValue(value.cutoff_at)} /> : null}
      <Labelled label="Note for customers">
        <TextArea name="note" defaultValue={value?.note ?? ""} rows={2} maxLength={500} />
      </Labelled>
      {plans.length ? (
        <fieldset>
          <legend className="type-label">Plans that can choose from it (none ticked: every plan)</legend>
          <div className="mt-2 flex flex-wrap gap-4">
            {plans.map((p) => (
              <label key={p.id} className="type-body-sm flex items-center gap-2">
                <input type="checkbox" name="eligible_plan_ids" value={p.id} defaultChecked={value?.eligible_plan_ids.includes(p.id)} className="size-4" />
                {p.name}
              </label>
            ))}
          </div>
        </fieldset>
      ) : null}
      <fieldset>
        <legend className="type-label">Meals</legend>
        <ul className="mt-2 grid gap-2 sm:grid-cols-2">
          {meals.map((m) => {
            const sel = selected.get(m.id);
            return (
              <li key={m.id} className="flex items-center justify-between gap-3 border border-border p-3">
                <label className="type-body-sm flex items-center gap-2">
                  <input type="checkbox" name="meal_ids" value={m.id} defaultChecked={!!sel} className="size-4" />
                  <span>
                    {m.name}
                    <span className="type-caption block text-muted">
                      {m.status}
                      {m.is_demo ? " · demo" : ""}
                      {sel?.chosen ? " · chosen by customers" : ""}
                    </span>
                  </span>
                </label>
                <input
                  name={`cap_${m.id}`}
                  type="number"
                  min={1}
                  max={14}
                  defaultValue={sel?.max ?? ""}
                  aria-label={`Most of ${m.name} per customer`}
                  placeholder="Max"
                  className="min-h-9 w-20 rounded-md border border-border-strong bg-surface px-2"
                />
              </li>
            );
          })}
        </ul>
      </fieldset>
    </>
  );
}
