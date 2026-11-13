import { cx, Tag } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

import { commerceTone, humanize, money, number } from "@/lib/format";

/*
 * Shared building blocks for the administration pages (Sprint 4): a page
 * header, status tags, tabs, pagination, an activity timeline, metric tiles
 * with their definitions, and a small weekly bar chart. Business patterns,
 * not wrappers around HTML elements.
 */

export function PageHeader({ title, description, actions, back }: { title: string; description?: ReactNode; actions?: ReactNode; back?: { href: string; label: string } }) {
  return (
    <header className="mb-6">
      {back ? (
        <Link href={back.href} className="type-body-sm underline underline-offset-4">
          {back.label}
        </Link>
      ) : null}
      <div className={cx("flex flex-wrap items-end justify-between gap-4", back && "mt-2")}>
        <h1 className="font-display text-[2rem] leading-tight">{title}</h1>
        {actions ? <div className="flex flex-wrap gap-3">{actions}</div> : null}
      </div>
      {description ? <p className="type-body-sm mt-2 max-w-[56rem] text-muted">{description}</p> : null}
    </header>
  );
}

export function StatusTag({ status, label }: { status: string; label?: string }) {
  return <Tag tone={commerceTone(status)}>{label ?? humanize(status)}</Tag>;
}

/** Tabs as links, so each tab has a URL and works without JavaScript. */
export function Tabs({ tabs, current, base }: { tabs: { key: string; label: string; hidden?: boolean }[]; current: string; base: string }) {
  return (
    <nav aria-label="Sections" className="-mx-1 overflow-x-auto px-1">
      <ul className="flex gap-1 border-b border-border">
        {tabs
          .filter((t) => !t.hidden)
          .map((t) => (
            <li key={t.key} className="shrink-0">
              <Link
                href={t.key === tabs[0]!.key ? base : `${base}${base.includes("?") ? "&" : "?"}tab=${t.key}`}
                aria-current={t.key === current ? "page" : undefined}
                className={cx(
                  "type-body-sm flex min-h-11 items-center border-b-2 px-3 font-semibold",
                  t.key === current ? "border-accent text-foreground" : "border-transparent text-muted hover:text-foreground",
                )}
              >
                {t.label}
              </Link>
            </li>
          ))}
      </ul>
    </nav>
  );
}

/** Previous/next links that keep the other query parameters. */
export function Pager({ page, size, total, params }: { page: number; size: number; total: number; params: Record<string, string | undefined> }) {
  const pages = Math.max(1, Math.ceil(total / size));
  const href = (p: number) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) if (v) q.set(k, v);
    if (p > 1) q.set("page", String(p));
    const s = q.toString();
    return s ? `?${s}` : "?";
  };
  return (
    <nav aria-label="Pages" className="type-body-sm mt-4 flex flex-wrap items-center justify-between gap-3">
      <span className="text-muted">
        {number.format(total)} {total === 1 ? "result" : "results"} · page {page} of {pages}
      </span>
      <span className="flex gap-4">
        {page > 1 ? (
          <Link href={href(page - 1)} className="font-semibold underline underline-offset-4">
            Previous
          </Link>
        ) : null}
        {page < pages ? (
          <Link href={href(page + 1)} className="font-semibold underline underline-offset-4">
            Next
          </Link>
        ) : null}
      </span>
    </nav>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <p className="type-body-sm border border-dashed border-border-strong p-6 text-muted">{children}</p>;
}

export function MetricTile({ label, value, kind, currency, definition }: { label: string; value: number; kind: string; currency?: string; definition: string }) {
  return (
    <div className="border border-border bg-surface p-4">
      <p className="type-caption font-semibold uppercase tracking-[0.06em] text-muted">{label}</p>
      <p className="type-figures mt-1 font-display text-[1.75rem] leading-tight">{kind === "money" ? money(value, currency) : kind === "percent" ? `${number.format(value)}%` : number.format(value)}</p>
      <details className="mt-1">
        <summary className="type-caption cursor-pointer text-muted underline decoration-dotted underline-offset-2">What this counts</summary>
        <p className="type-caption mt-1 text-muted">{definition}</p>
      </details>
    </div>
  );
}

export interface TimelineItem {
  at: string;
  title: ReactNode;
  detail?: ReactNode;
}

export function Timeline({ items, empty = "Nothing yet." }: { items: TimelineItem[]; empty?: string }) {
  if (!items.length) return <EmptyState>{empty}</EmptyState>;
  return (
    <ol className="border-l border-border-strong">
      {items.map((it, i) => (
        <li key={i} className="relative pb-4 pl-5">
          <span aria-hidden="true" className="absolute -left-[5px] top-1.5 size-2.5 rounded-full bg-primary" />
          <p className="type-caption text-muted">{new Date(it.at).toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short", timeZone: "America/Port_of_Spain" })}</p>
          <p className="type-body-sm font-semibold">{it.title}</p>
          {it.detail ? <div className="type-body-sm text-muted">{it.detail}</div> : null}
        </li>
      ))}
    </ol>
  );
}

const weekLabel = (d: string) => new Date(`${d}T00:00:00Z`).toLocaleDateString("en-GB", { day: "numeric", month: "short", timeZone: "UTC" });

/**
 * One series by week: thin bars from a zero baseline, one hue, a 2px gap,
 * a tooltip on each bar and a table for screen readers. The title names the
 * series, so there is no legend.
 */
export function WeeklyBars({ label, points, kind, currency, daily = false }: { label: string; points: { period: string; value: number }[]; kind: string; currency?: string; daily?: boolean }) {
  const unit = daily ? "Day" : "Week of";
  const fmt = (v: number) => (kind === "money" ? money(v, currency) : number.format(v));
  const max = Math.max(1, ...points.map((p) => p.value));
  const W = 360;
  const H = 120;
  const gap = 2;
  const bw = points.length ? (W - gap * (points.length - 1)) / points.length : W;
  const total = points.reduce((s, p) => s + p.value, 0);
  return (
    <figure>
      <svg viewBox={`0 0 ${W} ${H + 18}`} className="h-auto w-full" role="img" aria-label={`${label}: ${fmt(total)} over ${points.length} ${daily ? "days" : "weeks"}`}>
        <line x1="0" x2={W} y1={H} y2={H} className="stroke-border-strong" strokeWidth="1" />
        {points.map((p, i) => {
          const h = p.value === 0 ? 0 : Math.max(2, (p.value / max) * (H - 8));
          const x = i * (bw + gap);
          return (
            <g key={p.period}>
              <rect x={x} y={0} width={bw} height={H} fill="transparent">
                <title>{`${unit} ${weekLabel(p.period)}: ${fmt(p.value)}`}</title>
              </rect>
              {h > 0 ? (
                <path
                  d={`M${x},${H} V${H - h + 4} Q${x},${H - h} ${x + 4},${H - h} H${x + bw - 4} Q${x + bw},${H - h} ${x + bw},${H - h + 4} V${H} Z`}
                  className="pointer-events-none fill-primary"
                />
              ) : null}
            </g>
          );
        })}
        {points.length ? (
          <>
            <text x="0" y={H + 14} className="fill-muted text-[10px]">
              {weekLabel(points[0]!.period)}
            </text>
            <text x={W} y={H + 14} textAnchor="end" className="fill-muted text-[10px]">
              {weekLabel(points[points.length - 1]!.period)}
            </text>
          </>
        ) : null}
      </svg>
      <figcaption className="type-caption mt-1 text-muted">
        Highest {daily ? "day" : "week"}: {fmt(Math.max(0, ...points.map((p) => p.value)))} · total {fmt(total)}
      </figcaption>
      <table className="sr-only">
        <caption>{label}</caption>
        <thead>
          <tr>
            <th scope="col">{unit}</th>
            <th scope="col">Value</th>
          </tr>
        </thead>
        <tbody>
          {points.map((p) => (
            <tr key={p.period}>
              <td>{weekLabel(p.period)}</td>
              <td>{fmt(p.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}

/** A small form that submits with GET: filters live in the URL. */
export function FilterBar({ children }: { children: ReactNode }) {
  return (
    <form className="mb-5 flex flex-wrap items-end gap-3" role="search">
      {children}
      <button type="submit" className="type-body-sm min-h-11 rounded-md border border-foreground px-4 font-semibold hover:bg-foreground hover:text-surface">
        Apply
      </button>
    </form>
  );
}
