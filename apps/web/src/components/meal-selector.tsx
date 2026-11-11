"use client";

import type { MenuMeal, SelectionPage } from "@mise/api-client";
import { buttonClasses, cx, MiseButton, Tag } from "@mise/ui";
import { CircleAlert, CircleCheck, Minus, Plus, TriangleAlert } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { useMemo, useState, useTransition } from "react";

import { saveMeals } from "@/app/(app)/menu/actions";
import { deadline, timeLeft } from "@/lib/commerce";
import { minutes } from "@/lib/format";
import { addMeal, countOf, removeMeal, sameChoice, toItems, type Choice } from "@/lib/selection";

/**
 * Choosing a week's meals. Quantities change instantly on screen, but a
 * choice is shown as confirmed only when the server has confirmed it: any
 * local change makes it "not saved" again until it is.
 */
export function MealSelector({ initial }: { initial: SelectionPage }) {
  const [page, setPage] = useState(initial);
  const [choice, setChoice] = useState<Choice>(() => fromPage(initial));
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [pending, start] = useTransition();

  const saved = useMemo(() => fromPage(page), [page]);
  const dirty = !sameChoice(choice, saved);
  const count = countOf(choice);
  const allowance = page.allowance;
  const remaining = Math.max(allowance - count, 0);
  const over = count > allowance;
  const confirmed = page.confirmed && !dirty;
  const open = page.open;
  const meals = page.menu?.meals ?? [];
  const byId = new Map(meals.map((m) => [m.id, m]));

  function submit(confirm: boolean) {
    setError(null);
    setNotice(null);
    start(async () => {
      const res = await saveMeals(page.cycle.id, toItems(choice), page.selection?.version ?? null, confirm);
      if (res.page) {
        setPage(res.page);
        setChoice(fromPage(res.page));
        setNotice(confirm ? "Your meals are confirmed." : "Saved. Confirm your meals before the deadline.");
      } else {
        setError(res.error ?? "Your meals could not be saved.");
        if (res.latest) {
          setPage(res.latest);
          setChoice(fromPage(res.latest));
        }
      }
    });
  }

  const left = timeLeft(page.cycle.cutoff_at, new Date());

  return (
    <div className="grid gap-10 lg:grid-cols-[1fr_22rem] lg:gap-12">
      <div>
        <header>
          <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">Your meals</p>
          <h1 className="type-h2 mt-2">{page.cycle.week_label}</h1>
          <p className="type-body-lg mt-3 text-muted">
            {open ? (
              <>
                Choose {allowance} meals by <strong className="text-foreground">{deadline(page.cycle.cutoff_at)}</strong>
                {left ? <span> ({left} left)</span> : null}.
              </>
            ) : (
              <>Selection for this week has closed.</>
            )}
          </p>
          {page.has_allergies ? (
            <p className="type-body-sm mt-4 max-w-[44rem] border-l-2 border-error pl-3">
              Meals that contain an allergen you told us about are marked. Marks come from the allergens declared for each meal; they are not
              a guarantee.
            </p>
          ) : !page.allergies_reviewed ? (
            <p className="type-body-sm mt-4 max-w-[44rem] text-muted">
              Have allergies?{" "}
              <Link href="/profile/preferences" className="font-semibold text-foreground underline underline-offset-4">
                Tell us
              </Link>{" "}
              and we&apos;ll mark meals that contain them.
            </p>
          ) : null}
        </header>

        {meals.length ? (
          <ul className="mt-10 grid gap-x-6 gap-y-10 sm:grid-cols-2" aria-label="This week's menu">
            {meals.map((m) => (
              <MealOption
                key={m.id}
                meal={m}
                note={page.notes[m.id]}
                qty={choice[m.id] ?? 0}
                canAdd={open && remaining > 0 && (m.max_per_customer == null || (choice[m.id] ?? 0) < m.max_per_customer)}
                open={open}
                onAdd={() => setChoice((c) => addMeal(c, m.id))}
                onRemove={() => setChoice((c) => removeMeal(c, m.id))}
              />
            ))}
          </ul>
        ) : (
          <p className="type-body mt-10 text-muted">The menu for this week isn&apos;t out yet. We&apos;ll email you when it is.</p>
        )}
        <p className="type-caption mt-12 max-w-[44rem] text-muted">{page.cross_contact_notice}</p>
      </div>

      {/* The summary: a sticky column on large screens, a bar above the tab bar on phones. */}
      <aside aria-labelledby="summary-title" className="hidden lg:block">
        <div className="sticky top-24 border border-border bg-surface p-6">
          <Summary
            page={page}
            count={count}
            allowance={allowance}
            choice={choice}
            byId={byId}
            confirmed={confirmed}
            dirty={dirty}
            over={over}
            pending={pending}
            error={error}
            notice={notice}
            onSave={() => submit(false)}
            onConfirm={() => submit(true)}
          />
        </div>
      </aside>
      <div className="fixed inset-x-0 bottom-16 z-30 border-t border-border bg-surface px-(--page-gutter) py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] lg:hidden">
        <div className="flex items-center justify-between gap-3">
          <div aria-live="polite">
            <p className="type-body-sm font-semibold">
              {count} of {allowance} chosen
            </p>
            <p className={cx("type-caption", error ? "text-error" : "text-muted")}>
              {error ?? (confirmed ? "Confirmed" : dirty ? "Not saved yet" : open ? "Not confirmed" : "Closed")}
            </p>
          </div>
          {open ? (
            <MiseButton type="button" onClick={() => submit(true)} disabled={pending || count !== allowance || (confirmed && !dirty)}>
              {pending ? "Saving…" : confirmed ? "Confirmed" : count < allowance ? `Choose ${allowance - count} more` : "Confirm meals"}
            </MiseButton>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function fromPage(p: SelectionPage): Choice {
  const out: Choice = {};
  for (const it of p.selection?.items ?? []) out[it.meal_id] = it.quantity;
  return out;
}

function MealOption({
  meal,
  note,
  qty,
  canAdd,
  open,
  onAdd,
  onRemove,
}: {
  meal: MenuMeal;
  note?: SelectionPage["notes"][string];
  qty: number;
  canAdd: boolean;
  open: boolean;
  onAdd: () => void;
  onRemove: () => void;
}) {
  const allergic = note?.contains_your_allergens ?? [];
  return (
    <li className="flex flex-col">
      <div className={cx("relative aspect-[4/3] overflow-hidden rounded-lg bg-border", qty > 0 && "ring-2 ring-primary ring-offset-2 ring-offset-background")}>
        {meal.image ? (
          <Image src={meal.image.src} alt={meal.image.alt} fill sizes="(min-width: 1024px) 30vw, (min-width: 640px) 45vw, 92vw" className="object-cover" />
        ) : null}
        {qty > 0 ? (
          <span className="type-body-sm absolute left-3 top-3 rounded-sm bg-primary px-2 py-1 font-semibold text-primary-foreground" aria-hidden="true">
            {qty} in your box
          </span>
        ) : null}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <span className="type-caption font-semibold uppercase tracking-[0.08em] text-secondary">{meal.category.name}</span>
        {meal.is_demo ? <Tag tone="warning">Demo content</Tag> : null}
        {note?.matches_preferences.length ? <Tag tone="success">Suits your preferences</Tag> : null}
      </div>
      <h2 className="type-h3 mt-1">
        <Link href={`/meals/${meal.slug}`} className="hover:underline hover:underline-offset-4">
          {meal.name}
        </Link>
      </h2>
      <p className="type-caption type-figures mt-1 text-muted">
        {minutes(meal.total_minutes)} · Serves {meal.servings}
        {meal.dietary.length ? ` · ${meal.dietary.map((d) => d.label).join(", ")}` : ""}
      </p>
      {allergic.length ? (
        <p className="type-body-sm mt-2 flex gap-2 font-semibold text-error">
          <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" strokeWidth={2} />
          Contains {allergic.map((a) => a.label.toLowerCase()).join(" and ")}, which you told us you&apos;re allergic to.
        </p>
      ) : note?.allergens_unconfirmed ? (
        <p className="type-body-sm mt-2 flex gap-2 text-muted">
          <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" strokeWidth={1.75} />
          Allergens not confirmed yet. Don&apos;t choose this meal for an allergy.
        </p>
      ) : null}

      {open ? (
        <div className="mt-auto pt-4">
          {qty === 0 ? (
            <button type="button" onClick={onAdd} disabled={!canAdd} className={buttonClasses("secondary", "md", "w-full")} aria-label={`Add ${meal.name}`}>
              <Plus aria-hidden="true" className="size-4" strokeWidth={2} /> Add
            </button>
          ) : (
            <div className="flex items-center justify-between border border-foreground" role="group" aria-label={`${meal.name} quantity`}>
              <button type="button" onClick={onRemove} className="inline-flex size-11 items-center justify-center hover:bg-border" aria-label={`Remove one ${meal.name}`}>
                <Minus aria-hidden="true" className="size-4" strokeWidth={2} />
              </button>
              <span className="type-body font-semibold" aria-live="polite">
                {qty}
              </span>
              <button
                type="button"
                onClick={onAdd}
                disabled={!canAdd}
                className="inline-flex size-11 items-center justify-center hover:bg-border disabled:opacity-40"
                aria-label={`Add another ${meal.name}`}
              >
                <Plus aria-hidden="true" className="size-4" strokeWidth={2} />
              </button>
            </div>
          )}
          {meal.max_per_customer != null ? <p className="type-caption mt-1 text-muted">Up to {meal.max_per_customer} per box.</p> : null}
        </div>
      ) : qty > 0 ? (
        <p className="type-body-sm mt-3 font-semibold">{qty} in your box</p>
      ) : null}
    </li>
  );
}

function Summary({
  page,
  count,
  allowance,
  choice,
  byId,
  confirmed,
  dirty,
  over,
  pending,
  error,
  notice,
  onSave,
  onConfirm,
}: {
  page: SelectionPage;
  count: number;
  allowance: number;
  choice: Choice;
  byId: Map<string, MenuMeal>;
  confirmed: boolean;
  dirty: boolean;
  over: boolean;
  pending: boolean;
  error: string | null;
  notice: string | null;
  onSave: () => void;
  onConfirm: () => void;
}) {
  const chosen = Object.entries(choice).filter(([, q]) => q > 0);
  return (
    <>
      <h2 id="summary-title" className="type-h3">
        Your box
      </h2>
      <p className="type-body mt-1">
        <span className="font-display text-[2rem] leading-none">{count}</span>
        <span className="text-muted"> of {allowance} meals</span>
      </p>
      <div className="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-border" aria-hidden="true">
        <div className={cx("h-full transition-[width] duration-(--duration-fast)", over ? "bg-error" : "bg-primary")} style={{ width: `${Math.min((count / allowance) * 100, 100)}%` }} />
      </div>
      {chosen.length ? (
        <ul className="type-body-sm mt-5 grid gap-2 border-t border-border pt-4">
          {chosen.map(([id, q]) => (
            <li key={id} className="flex justify-between gap-3">
              <span>{byId.get(id)?.name ?? "A meal no longer on the menu"}</span>
              <span className="type-figures font-semibold">×{q}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-body-sm mt-5 border-t border-border pt-4 text-muted">Add meals from the menu.</p>
      )}

      <div aria-live="polite" className="mt-5">
        {error ? (
          <p role="alert" className="type-body-sm flex gap-2 font-semibold text-error">
            <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" strokeWidth={2} />
            {error}
          </p>
        ) : confirmed ? (
          <p className="type-body-sm flex gap-2 font-semibold text-success">
            <CircleCheck aria-hidden="true" className="mt-0.5 size-4 shrink-0" strokeWidth={2} />
            {notice ?? "Confirmed."} You can change your meals until {deadline(page.cycle.cutoff_at)}.
          </p>
        ) : dirty ? (
          <p className="type-body-sm text-muted">Not saved yet.</p>
        ) : notice ? (
          <p className="type-body-sm">{notice}</p>
        ) : page.selection?.status === "draft" ? (
          <p className="type-body-sm text-muted">Saved, not confirmed.</p>
        ) : null}
      </div>

      {page.open ? (
        <div className="mt-5 grid gap-2">
          <MiseButton type="button" onClick={onConfirm} disabled={pending || count !== allowance || (confirmed && !dirty)}>
            {pending ? "Saving…" : confirmed ? "Confirmed" : count < allowance ? `Choose ${allowance - count} more` : "Confirm meals"}
          </MiseButton>
          {count < allowance && count > 0 ? (
            <MiseButton type="button" variant="secondary" onClick={onSave} disabled={pending || !dirty}>
              Save for later
            </MiseButton>
          ) : null}
        </div>
      ) : (
        <p className="type-body-sm mt-5 text-muted">
          {page.confirmed ? "These meals were confirmed for this week." : "No meals were confirmed for this week. We'll be in touch."}
        </p>
      )}
    </>
  );
}
