import { cx } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

import { StatusTag } from "@/components/admin-ui";
import { formatDay, humanize } from "@/lib/format";

/*
 * Shared pieces for the Sprint 5 culinary and production screens. Kitchen
 * screens use large text and 44px+ targets; nothing here invents data.
 */

export function Checks({ legend, name, options, selected, hint }: { legend: string; name: string; options: { key: string; label: string }[]; selected: string[]; hint?: string }) {
  return (
    <fieldset>
      <legend className="type-label mb-1.5">{legend}</legend>
      {hint ? <p className="type-caption mb-2 text-muted">{hint}</p> : null}
      <div className="flex flex-wrap gap-x-5 gap-y-2">
        {options.map((o) => (
          <label key={o.key} className="type-body-sm inline-flex min-h-11 items-center gap-2">
            <input type="checkbox" name={name} value={o.key} defaultChecked={selected.includes(o.key)} className="size-5 accent-[var(--color-primary)]" />
            {o.label}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/** A guided sequence of steps as links: each step its own short form. */
export function Steps({ steps, current, base }: { steps: { key: string; label: string; done?: boolean }[]; current: string; base: string }) {
  return (
    <nav aria-label="Recipe steps" className="-mx-1 overflow-x-auto px-1">
      <ol className="flex gap-1 border-b border-border">
        {steps.map((s, i) => (
          <li key={s.key} className="shrink-0">
            <Link
              href={`${base}${base.includes("?") ? "&" : "?"}step=${s.key}`}
              aria-current={s.key === current ? "step" : undefined}
              className={cx(
                "type-body-sm flex min-h-11 items-center gap-2 border-b-2 px-3 font-semibold",
                s.key === current ? "border-accent text-foreground" : "border-transparent text-muted hover:text-foreground",
              )}
            >
              <span
                aria-hidden="true"
                className={cx("inline-flex size-6 items-center justify-center rounded-full border text-[0.75rem]", s.done ? "border-success text-success" : "border-border-strong")}
              >
                {s.done ? "✓" : i + 1}
              </span>
              {s.label}
            </Link>
          </li>
        ))}
      </ol>
    </nav>
  );
}

/** A plain list of problems or warnings, with a heading. */
export function Notice({ tone, title, items }: { tone: "error" | "warning" | "info"; title: string; items: string[] }) {
  if (!items.length) return null;
  const border = tone === "error" ? "border-error" : tone === "warning" ? "border-accent" : "border-border-strong";
  return (
    <section role={tone === "error" ? "alert" : undefined} className={cx("border-l-4 bg-surface p-4", border)}>
      <h2 className="type-body-sm font-semibold">{title}</h2>
      <ul className="type-body-sm mt-1 list-disc pl-5">
        {items.map((it) => (
          <li key={it}>{it}</li>
        ))}
      </ul>
    </section>
  );
}

/** A batch as a card: readable across a kitchen. */
export function BatchCard({
  batch,
}: {
  batch: { id: string; code: string; meal_name: string; status: string; planned_kits: number; actual_kits: number | null; production_date: string; recipe_version: number; tasks_open: number; quality_status: string; packaging_status: string };
}) {
  return (
    <Link href={`/production/batches/${batch.id}`} className="block border border-border bg-surface p-4 hover:border-foreground focus-visible:border-foreground">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <p className="type-caption font-semibold uppercase tracking-[0.06em] text-muted">{batch.code}</p>
        <StatusTag status={batch.status} />
      </div>
      <p className="mt-1 font-display text-[1.375rem] leading-tight">{batch.meal_name}</p>
      <p className="type-body-sm mt-2 text-muted">
        <span className="font-semibold text-foreground">{batch.actual_kits ?? batch.planned_kits}</span> {batch.actual_kits == null ? "kits planned" : "kits accepted"} ·{" "}
        {formatDay(batch.production_date)} · recipe v{batch.recipe_version}
      </p>
      <p className="type-caption mt-1 text-muted">
        {batch.tasks_open} open {batch.tasks_open === 1 ? "task" : "tasks"} · quality {humanize(batch.quality_status).toLowerCase()} · packing {humanize(batch.packaging_status).toLowerCase()}
      </p>
    </Link>
  );
}

export const CATEGORY_OPTIONS = ["produce", "meat", "poultry", "seafood", "dairy", "eggs", "grains", "legumes", "herbs_spices", "condiments", "oils_fats", "baking", "beverages", "packaged", "other"];
export const PREP_STATES = ["raw", "prepared", "cooked", "processed", "dried", "frozen", "canned"];
export const STORAGE = ["unspecified", "ambient", "chilled", "frozen"];
export const NUTRITION_SOURCES = ["laboratory_analysis", "food_composition_database", "supplier_specification", "other"];
export const TASK_KINDS = ["preparation", "portioning", "sauce", "assembly", "labelling", "packaging", "inspection", "other"];
export const CHECK_TYPES = ["ingredient_verification", "quantity_verification", "packaging_integrity", "label_accuracy", "allergen_declaration", "storage_temperature", "general_inspection"];
export const WASTE_CATEGORIES = ["preparation_loss", "spoilage", "quality_rejection", "packaging_damage", "overproduction", "other"];

export function Options({ values, label = humanize }: { values: string[]; label?: (v: string) => string }) {
  return (
    <>
      {values.map((v) => (
        <option key={v} value={v}>
          {label(v)}
        </option>
      ))}
    </>
  );
}

export function Field({ children }: { children: ReactNode }) {
  return <div className="grid gap-4 sm:grid-cols-2">{children}</div>;
}
