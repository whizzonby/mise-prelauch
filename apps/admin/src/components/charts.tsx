"use client";

import { useState } from "react";

import { formatDay, niceMax, number, percent } from "@/lib/format";

/*
 * Charts for the admin. Both are single-series, so there is one colour (the
 * primary green) and no legend: the panel title says what is plotted. Values
 * and labels use text colours, never the mark colour. Each chart has a table
 * view, so nothing is available only by sight or by hover.
 */

type Day = { date: string; count: number };

/** Signups per day as columns: a count over time, so position on a common baseline. */
export function SignupsChart({ days }: { days: Day[] }) {
  const [hovered, setHovered] = useState<number | null>(null);
  const top = niceMax(Math.max(...days.map((d) => d.count), 0));
  const ticks = [top, top / 2, 0];
  const total = days.reduce((sum, d) => sum + d.count, 0);
  const peak = days.reduce((best, d, i) => (d.count > days[best]!.count ? i : best), 0);
  const active = hovered === null ? null : days[hovered]!;
  // Label the first, middle and last day; the tooltip carries the rest.
  const labelled = new Set([0, Math.floor((days.length - 1) / 2), days.length - 1]);

  return (
    <figure>
      <figcaption className="sr-only">
        Signups per day over the last {days.length} days. {number.format(total)} in total.
      </figcaption>

      <div aria-hidden="true" className="grid grid-cols-[auto_1fr] gap-x-3">
        <div className="type-caption type-figures flex h-52 flex-col justify-between text-right text-muted">
          {ticks.map((tick) => (
            <span key={tick} className="-translate-y-1/2 leading-none last:translate-y-0">
              {number.format(tick)}
            </span>
          ))}
        </div>

        <div className="relative h-52" onMouseLeave={() => setHovered(null)}>
          {/* Recessive hairline grid. */}
          {ticks.map((tick, index) => (
            <span key={tick} className="absolute inset-x-0 h-px bg-border" style={{ top: `${(index / (ticks.length - 1)) * 100}%` }} />
          ))}

          <div className="absolute inset-0 flex items-end">
            {days.map((day, index) => (
              // The whole slot is the hover target; the bar inside stays thin.
              <div key={day.date} className="relative flex h-full flex-1 items-end justify-center" onMouseEnter={() => setHovered(index)}>
                {hovered === index ? <span className="absolute inset-y-0 w-full bg-border/50" /> : null}
                <span
                  className="relative w-full max-w-6 rounded-t-md bg-primary"
                  style={{ height: `${(day.count / top) * 100}%`, marginInline: "1px", minHeight: day.count > 0 ? "2px" : 0 }}
                />
                {index === peak && day.count > 0 && hovered === null ? (
                  <span
                    className="type-caption type-figures absolute left-1/2 -translate-x-1/2 font-semibold"
                    style={{ bottom: `calc(${(day.count / top) * 100}% + 4px)` }}
                  >
                    {number.format(day.count)}
                  </span>
                ) : null}
              </div>
            ))}
          </div>

          {active ? (
            <div
              className="type-caption pointer-events-none absolute top-0 z-10 -translate-x-1/2 -translate-y-full whitespace-nowrap rounded-md bg-foreground px-2.5 py-1.5 text-surface shadow-overlay"
              style={{ left: `clamp(3.5rem, ${((hovered! + 0.5) / days.length) * 100}%, calc(100% - 3.5rem))` }}
            >
              <span className="type-figures font-semibold">{number.format(active.count)}</span>{" "}
              {active.count === 1 ? "signup" : "signups"} on {formatDay(active.date)}
            </div>
          ) : null}
        </div>

        <span />
        <div className="type-caption type-figures relative mt-2 h-4 text-muted">
          {days.map((day, index) =>
            labelled.has(index) ? (
              <span
                key={day.date}
                className="absolute whitespace-nowrap"
                style={
                  index === 0
                    ? { left: 0 }
                    : index === days.length - 1
                      ? { right: 0 }
                      : { left: `${((index + 0.5) / days.length) * 100}%`, transform: "translateX(-50%)" }
                }
              >
                {formatDay(day.date)}
              </span>
            ) : null,
          )}
        </div>
      </div>

      <details className="mt-4">
        <summary className="type-body-sm cursor-pointer underline underline-offset-4">Show as a table</summary>
        <div className="mt-3 max-h-64 overflow-y-auto border border-border">
          <table className="type-body-sm type-figures w-full">
            <thead className="sticky top-0 bg-surface text-left">
              <tr>
                <th scope="col" className="border-b border-border px-3 py-2 font-semibold">
                  Day
                </th>
                <th scope="col" className="border-b border-border px-3 py-2 text-right font-semibold">
                  Signups
                </th>
              </tr>
            </thead>
            <tbody>
              {[...days].reverse().map((day) => (
                <tr key={day.date}>
                  <td className="border-b border-border px-3 py-1.5">{formatDay(day.date)}</td>
                  <td className="border-b border-border px-3 py-1.5 text-right">{number.format(day.count)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  );
}

type Row = { label: string; count: number };

/**
 * A ranked breakdown as horizontal bars: categories compared by length, with
 * the value at the tip of each bar. It is a table underneath, so the label,
 * count and share are all readable without the bars.
 */
export function BarList({ rows, total, empty }: { rows: Row[]; total: number; empty: string }) {
  if (rows.length === 0) {
    return <p className="type-body-sm text-muted">{empty}</p>;
  }
  const max = Math.max(...rows.map((row) => row.count));
  return (
    <table className="w-full border-separate border-spacing-y-2.5">
      <thead className="sr-only">
        <tr>
          <th scope="col">Group</th>
          <th scope="col">Leads</th>
          <th scope="col">Share of leads</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.label} className="group">
            <th scope="row" className="type-body-sm w-[38%] max-w-0 truncate pr-3 text-left font-normal" title={row.label}>
              {row.label}
            </th>
            <td>
              <div className="flex items-center gap-2">
                <span
                  aria-hidden="true"
                  className="block h-2.5 rounded-r-md bg-primary transition-opacity duration-(--duration-fast) group-hover:opacity-80"
                  style={{ width: `${(row.count / max) * 100}%`, minWidth: "2px", maxWidth: "calc(100% - 3rem)" }}
                />
                <span className="type-body-sm type-figures font-semibold">{number.format(row.count)}</span>
              </div>
            </td>
            <td className="type-caption type-figures w-12 text-right text-muted">{percent(row.count, total)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
