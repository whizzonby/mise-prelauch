import { Labelled, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { humanize } from "@/lib/format";

const SEGMENTS = ["everyday", "fitness", "family", "school", "premium", "corporate"];

/** The editable part of a plan. Prices are added separately, as versions. */
export function PlanFields({ plan }: { plan?: { name: string; slug: string; summary: string; description: string; meals_per_week: number; segment: { key: string }; benefits: string[]; self_serve: boolean; sort_order: number } }) {
  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2">
        <Labelled label="Name">
          <TextInput name="name" defaultValue={plan?.name} required maxLength={60} />
        </Labelled>
        <Labelled label="Web address" hint="Left empty, it is made from the name.">
          <TextInput name="slug" defaultValue={plan?.slug} disabled={!!plan} />
        </Labelled>
      </div>
      <Labelled label="Summary">
        <TextInput name="summary" defaultValue={plan?.summary} required maxLength={200} />
      </Labelled>
      <Labelled label="Description">
        <TextArea name="description" defaultValue={plan?.description} rows={3} maxLength={2000} />
      </Labelled>
      <div className="grid gap-4 sm:grid-cols-3">
        <Labelled label="Meals per week">
          <TextInput name="meals_per_week" type="number" min={1} max={14} defaultValue={plan?.meals_per_week ?? 3} />
        </Labelled>
        <Labelled label="Meant for">
          <SelectInput name="segment" defaultValue={plan?.segment.key ?? "everyday"}>
            {SEGMENTS.map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Order on the plans page">
          <TextInput name="sort_order" type="number" defaultValue={plan?.sort_order ?? 0} />
        </Labelled>
      </div>
      <Labelled label="Benefits" hint="One per line, up to 8.">
        <TextArea name="benefits" defaultValue={plan?.benefits.join("\n")} rows={3} />
      </Labelled>
      <label className="type-body-sm flex items-center gap-3">
        <input type="checkbox" name="self_serve" defaultChecked={plan?.self_serve ?? true} className="size-5" />
        Customers can buy it online (otherwise it shows &quot;talk to us&quot;).
      </label>
    </>
  );
}
