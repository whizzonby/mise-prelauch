import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag, Tabs } from "@/components/admin-ui";
import { formatDay } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Production calendar" };

type Search = { from?: string; tab?: string };

const shift = (day: string, n: number) => {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
};

/** Day or week view of batches, plans, cutoffs and the proposed schedule, in Trinidad and Tobago time. */
export default async function CalendarPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const sp = await searchParams;
  const view = sp.tab === "day" ? "day" : "week";
  const c = await api.kitchen.calendar(token, sp.from, view === "day" ? 1 : 7);
  const step = view === "day" ? 1 : 7;
  return (
    <>
      <PageHeader
        title="Production calendar"
        description={`${formatDay(c.from)}${c.from !== c.to ? ` – ${formatDay(c.to)}` : ""}. Schedule slots marked “proposed” are the business plan's suggestion, not an approved schedule.`}
        actions={
          <div className="type-body-sm flex gap-4">
            <Link href={`/production/calendar?tab=${view}&from=${shift(c.from, -step)}`} className="inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Previous
            </Link>
            <Link href={`/production/calendar?tab=${view}`} className="inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Today
            </Link>
            <Link href={`/production/calendar?tab=${view}&from=${shift(c.from, step)}`} className="inline-flex min-h-11 items-center font-semibold underline underline-offset-4">
              Next
            </Link>
          </div>
        }
      />
      <Tabs
        base={`/production/calendar?from=${c.from}`}
        current={view}
        tabs={[
          { key: "week", label: "Week" },
          { key: "day", label: "Day" },
        ]}
      />
      <ol className={`mt-4 grid gap-3 ${view === "week" ? "md:grid-cols-2 xl:grid-cols-7" : ""}`}>
        {c.days.map((d) => (
          <li key={d.date} className="border border-border bg-surface p-3">
            <p className="type-body-sm font-semibold">
              {d.weekday} {formatDay(d.date)}
            </p>
            {d.slots.map((s) => (
              <p key={s} className="type-caption text-muted">
                {s}
              </p>
            ))}
            {d.cutoffs.map((s) => (
              <p key={s} className="type-caption font-semibold">
                {s}
              </p>
            ))}
            {d.plans.map((p) => (
              <p key={p.id} className="type-caption mt-1">
                <Link href={`/production/plans/${p.id}`} className="underline underline-offset-4">
                  Plan {p.number}
                </Link>{" "}
                <StatusTag status={p.status} />
              </p>
            ))}
            <ul className="mt-2 grid gap-1">
              {d.batches.map((b) => (
                <li key={b.id}>
                  <Link href={`/production/batches/${b.id}`} className="type-caption block border-l-2 border-primary pl-2 hover:underline">
                    <span className="font-semibold">{b.meal_name}</span> · {b.planned_kits} kits · {b.status.replace(/_/g, " ")}
                    {b.assigned_team ? ` · ${b.assigned_team}` : ""}
                  </Link>
                </li>
              ))}
            </ul>
            {d.warnings.map((w) => (
              <p key={w} className="type-caption mt-1 text-error">
                {w}
              </p>
            ))}
          </li>
        ))}
      </ol>
    </>
  );
}
