import type { components } from "@mise/api-client";
import { cx, Tag } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { formatDateTime, humanize, money, number } from "@/lib/format";

/*
 * Mise Intelligence building blocks (Sprint 9.2). Every figure shows its
 * definition and freshness; missing values read "Not available", never 0;
 * every recommendation carries its evidence in a disclosure.
 */

type S = components["schemas"];
export type Fact = S["IntelligenceFact"];
export type ToolResult = S["IntelligenceToolResult"];
export type Risk = S["IntelligenceRisk"];
export type Opportunity = S["IntelligenceOpportunity"];
export type Recommendation = S["IntelligenceRecommendation"];
export type Briefing = S["IntelligenceBriefing"];
export type BriefingMetric = S["IntelligenceBriefingMetric"];
export type Projection = S["ScenarioProjection"];
export type Run = S["IntelligenceRun"];

/** A value in its unit; null is "Not available". */
export function formatValue(v: number | null | undefined, unit: string, currency = "TTD"): string {
  if (v === null || v === undefined) return "Not available";
  switch (unit) {
    case "money_minor":
    case "money":
      return money(Math.round(v), currency || "TTD");
    case "ratio":
      return `${(v * 100).toLocaleString("en-GB", { maximumFractionDigits: 1 })}%`;
    case "percent":
      return `${v.toLocaleString("en-GB", { maximumFractionDigits: 2 })}%`;
    case "percentage_points":
      return `${v > 0 ? "+" : ""}${v.toLocaleString("en-GB", { maximumFractionDigits: 2 })} pts`;
    case "basis_points":
      return `${(v / 100).toLocaleString("en-GB", { maximumFractionDigits: 2 })}%`;
    case "hours":
      return `${v.toLocaleString("en-GB", { maximumFractionDigits: 1 })} h`;
    case "weeks":
      return v >= 99 ? "Not within the horizon" : `${number.format(v)} weeks`;
    case "number":
      return v.toLocaleString("en-GB", { maximumFractionDigits: 2 });
    default:
      return number.format(v);
  }
}

/** A single figure with its definition and where it came from. */
export function FactTile({ label, value, unit, currency, definition, freshness, note, previous }: {
  label: string;
  value: number | null | undefined;
  unit: string;
  currency?: string;
  definition?: string;
  freshness?: string;
  note?: string;
  /** The same figure for the previous period, when there is one. */
  previous?: number | null;
}) {
  const missing = value === null || value === undefined;
  return (
    <div className="flex flex-col border border-border bg-surface p-4">
      <p className="type-caption font-semibold uppercase tracking-[0.06em] text-muted">{label}</p>
      <p className={cx("type-figures mt-1 font-display leading-tight", missing ? "text-[1.1rem] text-muted" : "text-[1.75rem]")}>{formatValue(value, unit, currency)}</p>
      {previous !== undefined ? <p className="type-caption text-muted">Previous period: {formatValue(previous, unit, currency)}</p> : null}
      {note ? <p className="type-caption mt-1 text-muted">{note}</p> : null}
      <details className="mt-2">
        <summary className="type-caption cursor-pointer text-muted underline decoration-dotted underline-offset-2">Definition and freshness</summary>
        {definition ? <p className="type-caption mt-1">{definition}</p> : null}
        {freshness ? <p className="type-caption mt-1 text-muted">As of {formatDateTime(freshness)}</p> : null}
      </details>
    </div>
  );
}

export function FactGrid({ children }: { children: ReactNode }) {
  return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{children}</div>;
}

/**
 * Facts of a tool result, with their evidence. A fact's previous-period value
 * (key ".previous") is shown inside its tile rather than as a tile of its own.
 * With `headline`, those facts lead and the rest sit behind "All figures".
 */
export function ToolFacts({ r, headline }: { r: ToolResult; headline?: string[] }) {
  const previous = new Map(r.facts.filter((f) => f.key.endsWith(".previous")).map((f) => [f.key.slice(0, -".previous".length), f.value]));
  const main = r.facts.filter((f) => !f.key.endsWith(".previous"));
  const tile = (f: Fact) => (
    <FactTile
      key={f.key}
      label={f.label}
      value={f.value}
      unit={f.unit}
      currency={f.currency}
      definition={f.definition}
      freshness={r.evidence.freshness}
      note={f.note}
      previous={previous.has(f.key) ? previous.get(f.key) : undefined}
    />
  );
  if (!headline) return <FactGrid>{main.map(tile)}</FactGrid>;
  const lead = headline.map((k) => main.find((f) => f.key === k)).filter((f): f is Fact => !!f);
  const rest = main.filter((f) => !headline.includes(f.key));
  return (
    <>
      <FactGrid>{lead.map(tile)}</FactGrid>
      {rest.length ? (
        <details className="mt-4">
          <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">All figures ({rest.length} more)</summary>
          <div className="mt-3">
            <FactGrid>{rest.map(tile)}</FactGrid>
          </div>
        </details>
      ) : null}
    </>
  );
}

export function Evidence({ title = "Evidence", children }: { title?: string; children: ReactNode }) {
  return (
    <details className="mt-3 border-t border-border pt-2">
      <summary className="type-caption cursor-pointer font-semibold text-muted underline decoration-dotted underline-offset-2">{title}</summary>
      <div className="type-caption mt-2 grid gap-1 text-muted">{children}</div>
    </details>
  );
}

const sevTone = { high: "error", watch: "warning", none: "success" } as const;

export function SeverityTag({ risk }: { risk: Pick<Risk, "status" | "severity"> }) {
  if (risk.status === "not_evaluable") return <Tag tone="neutral">Not evaluated</Tag>;
  if (risk.status === "clear") return <Tag tone="success">Clear</Tag>;
  return <Tag tone={sevTone[risk.severity as keyof typeof sevTone] ?? "neutral"}>{risk.severity === "high" ? "High" : "Watch"}</Tag>;
}

export function BasisTag({ basis }: { basis: string }) {
  const label: Record<string, string> = { observed: "Observed", ai_hypothesis: "AI hypothesis", business_proposal: "Business proposal", validated: "Validated" };
  return <Tag tone={basis === "validated" ? "success" : basis === "ai_hypothesis" ? "accent" : "neutral"}>{label[basis] ?? humanize(basis)}</Tag>;
}

/** One risk indicator: value, threshold, comparison and the figures behind it. */
export function RiskRow({ r, action }: { r: Risk; action?: ReactNode }) {
  return (
    <li className="border-b border-border py-4 last:border-b-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 max-w-[48rem]">
          <p className="type-body font-semibold">
            {r.title} <SeverityTag risk={r} />
          </p>
          <p className="type-body-sm mt-1 text-muted">{r.explanation}</p>
          <p className="type-body-sm mt-1">
            Review:{" "}
            <Link className="underline underline-offset-4" href={r.review_href}>
              {r.review_action}
            </Link>
          </p>
        </div>
        <div className="text-right">
          <p className="type-figures font-display text-[1.4rem] leading-tight">{formatValue(r.value, r.threshold.unit)}</p>
          <p className="type-caption text-muted">
            Watch {r.threshold.direction === "above" ? "≥" : "≤"} {formatValue(r.threshold.warn, r.threshold.unit)} · High {r.threshold.direction === "above" ? "≥" : "≤"}{" "}
            {formatValue(r.threshold.critical, r.threshold.unit)}
          </p>
          {action}
        </div>
      </div>
      <Evidence>
        <p>Measure: {r.measure}.</p>
        <p>
          Period {r.from} to {r.to}; compared with {r.previous_from} to {r.previous_to}. Evaluated {formatDateTime(r.freshness)}.
        </p>
        {r.metrics.map((m) => (
          <p key={m.id}>
            {m.label} ({m.id}): {formatValue(m.current, m.unit)}
            {m.previous !== null && m.previous !== undefined ? `, previous ${formatValue(m.previous, m.unit)}` : ""}
          </p>
        ))}
        {r.limitations.map((l) => (
          <p key={l}>{l}</p>
        ))}
        <p>Indicators are rules with thresholds. They never state a probability.</p>
      </Evidence>
    </li>
  );
}

export function EvidenceMap({ ev }: { ev: Record<string, string> }) {
  return (
    <>
      {Object.entries(ev).map(([k, v]) => (
        <p key={k}>
          {humanize(k)}: {v}
        </p>
      ))}
    </>
  );
}

/** AI not available: say so plainly, and that every deterministic view still works. */
export function AIUnavailable({ reason }: { reason: string }) {
  return (
    <section className="border-l-4 border-border-strong bg-surface p-5">
      <h2 className="type-h4">AI unavailable</h2>
      <p className="type-body-sm mt-1">{reason}</p>
      <p className="type-body-sm mt-2 text-muted">
        Every figure, risk, scenario and briefing here is calculated without AI and works as normal. AI only writes explanations, and only from these
        figures. It is switched on by an owner once a provider, a model, a monthly budget and prices are approved.
      </p>
    </section>
  );
}

/** A small single-series bar chart from a zero baseline (inline SVG; a table follows for screen readers). */
export function Bars({ label, points, unit }: { label: string; points: { x: string; y: number }[]; unit: string }) {
  if (!points.length) return <Empty>No data to chart.</Empty>;
  const max = Math.max(...points.map((p) => Math.abs(p.y)), 1);
  const hasNeg = points.some((p) => p.y < 0);
  const h = 140;
  const zero = hasNeg ? h / 2 : h;
  const scale = (hasNeg ? h / 2 : h) / max;
  const w = 100 / points.length;
  return (
    <figure>
      <figcaption className="type-caption mb-2 font-semibold text-muted">{label}</figcaption>
      <svg viewBox={`0 0 100 ${h}`} preserveAspectRatio="none" className="h-36 w-full" role="img" aria-label={label}>
        <line x1="0" x2="100" y1={zero} y2={zero} className="stroke-border-strong" strokeWidth="0.3" vectorEffect="non-scaling-stroke" />
        {points.map((p, i) => {
          const bh = Math.abs(p.y) * scale;
          return (
            <rect key={p.x} x={i * w + w * 0.15} width={w * 0.7} y={p.y >= 0 ? zero - bh : zero} height={Math.max(bh, 0.5)} className={p.y >= 0 ? "fill-primary" : "fill-accent"}>
              <title>
                {p.x}: {formatValue(p.y, unit)}
              </title>
            </rect>
          );
        })}
      </svg>
      <details>
        <summary className="type-caption cursor-pointer text-muted underline decoration-dotted underline-offset-2">Chart data</summary>
        <DataTable head={["", "Value"]} minWidth="16rem" label={label}>
          {points.map((p) => (
            <tr key={p.x}>
              <td className="p-2">{p.x}</td>
              <td className="type-figures p-2">{formatValue(p.y, unit)}</td>
            </tr>
          ))}
        </DataTable>
      </details>
    </figure>
  );
}

export function Change({ m }: { m: BriefingMetric }) {
  if (m.previous === null || m.previous === undefined) return <span className="type-caption text-muted">No comparison</span>;
  if (m.change_pct === null || m.change_pct === undefined) return <span className="type-caption text-muted">Previous {formatValue(m.previous, m.unit)}</span>;
  const up = m.change_pct > 0;
  return (
    <span className="type-caption text-muted">
      <span aria-hidden="true">{m.change_pct === 0 ? "→" : up ? "↑" : "↓"}</span> {up ? "+" : ""}
      {m.change_pct.toLocaleString("en-GB", { maximumFractionDigits: 1 })}% vs {formatValue(m.previous, m.unit)}
    </span>
  );
}

function ItemList({ items, empty }: { items: S["IntelligenceBriefingItem"][]; empty: string }) {
  if (!items.length) return <p className="type-body-sm text-muted">{empty}</p>;
  return (
    <ul className="grid gap-2">
      {items.map((it, i) => (
        <li key={i} className="type-body-sm">
          <span className="font-semibold">{it.href ? <Link className="underline underline-offset-4" href={it.href}>{it.title}</Link> : it.title}</span>
          {it.level ? <> <Tag tone={it.level === "high" ? "error" : "warning"}>{it.level === "high" ? "High" : "Watch"}</Tag></> : null}
          {it.detail ? <span className="block text-muted">{it.detail}</span> : null}
        </li>
      ))}
    </ul>
  );
}

/** A briefing version: deterministic sections, then the AI narrative if there is one. */
export function BriefingView({ b }: { b: Briefing }) {
  const c = b.content;
  return (
    <div className="grid gap-5">
      <p className="type-body-sm text-muted">
        {humanize(b.cadence)} briefing for {c.from} to {c.to}, compared with {c.previous_from} to {c.previous_to}. Version {b.version}, {b.source === "schedule" ? "scheduled" : "generated on request"}{" "}
        {formatDateTime(b.created_at)}.
      </p>
      {b.narrative ? (
        <Panel title="Summary (AI)" note={`Written by ${b.narrative.model} (${b.narrative.prompt_version}) from the figures below only; numbers not in them are marked [unverified].`}>
          <p className="type-body whitespace-pre-wrap">{b.narrative.text}</p>
        </Panel>
      ) : (
        <p className="type-body-sm border-l-2 border-border-strong pl-3 text-muted">{b.ai_note}</p>
      )}
      <Panel title="Business performance">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          {c.performance.map((m) => (
            <div key={m.id} className="border border-border p-3">
              <p className="type-caption font-semibold text-muted">{m.name}</p>
              <p className={cx("type-figures font-display", m.current === null ? "text-muted" : "text-[1.3rem]")}>{formatValue(m.current, m.unit)}</p>
              <Change m={m} />
              <details>
                <summary className="type-caption cursor-pointer text-muted underline decoration-dotted">Definition</summary>
                <p className="type-caption">{m.definition}</p>
                <p className="type-caption text-muted">{m.id}</p>
              </details>
            </div>
          ))}
        </div>
      </Panel>
      <div className="grid gap-5 lg:grid-cols-2">
        <Panel title="Significant changes" note="20 % or more against the previous period (counts need at least 5 before).">
          <ItemList items={c.significant_changes} empty="No significant changes." />
        </Panel>
        <Panel title="Financial position" note="Management figures; not audited statements.">
          <ItemList items={c.financial_position} empty="Nothing recorded." />
        </Panel>
        <Panel title="Customer trends">
          <ul className="grid gap-2">
            {c.customer_trends.map((m) => (
              <li key={m.id} className="type-body-sm flex flex-wrap justify-between gap-2">
                <span>{m.name}</span>
                <span>
                  <span className="type-figures font-semibold">{formatValue(m.current, m.unit)}</span> <Change m={m} />
                </span>
              </li>
            ))}
          </ul>
        </Panel>
        <Panel title="Operational exceptions">
          <ItemList items={c.operational_exceptions} empty="No open exceptions recorded." />
        </Panel>
        <Panel title="Key risks">
          <ItemList items={c.key_risks} empty="No risk indicator is triggered." />
        </Panel>
        <Panel title="Growth opportunities" note="Observations from records, not forecasts.">
          <ItemList items={c.growth_opportunities} empty="No observed opportunities." />
        </Panel>
      </div>
      <Panel title="Recommended management actions" note="Reviews through the existing workflows; nothing is done automatically.">
        <ItemList items={c.recommended_actions} empty="Nothing to review." />
      </Panel>
      {c.data_limitations.length ? (
        <Panel title="Data limitations">
          <ul className="type-body-sm list-disc pl-5 text-muted">
            {c.data_limitations.map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
        </Panel>
      ) : null}
      <p className="type-caption text-muted">{c.statement}</p>
    </div>
  );
}

const sourceTone = { observed: "neutral", preset: "accent", assumption: "warning", what_if: "warning" } as const;
const OPTIONAL_INPUTS = new Set(["capacity_kits_per_week", "opening_cash_minor"]);

/** A scenario projection: inputs with their sources, totals, break-even, cash and the weeks. */
export function ProjectionView({ p }: { p: Projection }) {
  const o = p.output;
  return (
    <div className="grid gap-5">
      <p className="type-caption text-muted">{o.statement}</p>
      {!o.complete ? (
        <section className="border-l-4 border-accent bg-surface p-4">
          <h3 className="type-body-sm font-semibold">Not calculated: some inputs are missing</h3>
          <p className="type-body-sm mt-1">
            Enter these as assumptions, or record the data they come from:{" "}
            {o.missing.map((k) => p.inputs.find((i) => i.key === k)?.label ?? k).join(", ")}.
          </p>
        </section>
      ) : (
        <>
          <FactGrid>
            <FactTile label="Sales" value={o.totals?.sales_minor} unit="money_minor" definition="Paid kits × order value after the price change, over the horizon." />
            <FactTile label="Contribution" value={o.totals?.contribution_minor} unit="money_minor" definition="Sales − provider fees − ingredients − packaging − delivery." />
            <FactTile label="Operating result" value={o.totals?.operating_result_minor} unit="money_minor" definition="Contribution − fixed costs, labour and marketing − one-off investments." />
            <FactTile
              label="Break-even subscriptions"
              value={o.break_even?.subscriptions ?? null}
              unit="count"
              definition={`${o.break_even?.formula ?? ""} ${o.break_even?.note ?? ""}`}
            />
          </FactGrid>
          <div className="grid gap-5 lg:grid-cols-2">
            <Bars label="Operating result by week" unit="money_minor" points={o.weeks.map((w) => ({ x: `Week ${String(w.week)}`, y: Number(w.operating_result_minor) }))} />
            <Panel title="Cash" note={o.cash.note}>
              <dl className="type-body-sm grid gap-1">
                <div className="flex justify-between gap-3">
                  <dt>Peak cash need</dt>
                  <dd className="type-figures font-semibold">{formatValue(o.cash.peak_need_minor ?? 0, "money_minor")}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt>Lowest cash</dt>
                  <dd className="type-figures">{formatValue(o.cash.lowest_cash_minor, "money_minor")}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt>Ending cash</dt>
                  <dd className="type-figures">{formatValue(o.cash.ending_cash_minor, "money_minor")}</dd>
                </div>
                <div className="flex justify-between gap-3">
                  <dt>First week below zero</dt>
                  <dd>{o.cash.first_negative_week ?? "None"}</dd>
                </div>
              </dl>
            </Panel>
          </div>
        </>
      )}
      <DataTable head={["Input", "Value", "Source", "Basis"]} label="Scenario inputs" minWidth="40rem">
        {p.inputs.map((i) => (
          <tr key={i.key}>
            <td className="p-3">{i.label}</td>
            <td className="type-figures p-3">{formatValue(i.value, i.unit === "money_minor" ? "money_minor" : i.unit)}</td>
            <td className="p-3">
              {i.source ? (
                <Tag tone={sourceTone[i.source as keyof typeof sourceTone] ?? "neutral"}>{humanize(i.source)}</Tag>
              ) : OPTIONAL_INPUTS.has(i.key) ? (
                <Tag tone="neutral">Not set (optional)</Tag>
              ) : (
                <Tag tone="error">Missing</Tag>
              )}
            </td>
            <td className="type-caption p-3 text-muted">{i.basis}</td>
          </tr>
        ))}
      </DataTable>
      {p.assumptions.length ? (
        <Panel title="Assumptions" note={p.preset_note}>
          <ul className="type-body-sm list-disc pl-5">
            {p.assumptions.map((a) => (
              <li key={a}>{a}</li>
            ))}
          </ul>
        </Panel>
      ) : null}
      {o.limitations.length ? (
        <ul className="type-caption list-disc pl-5 text-muted">
          {o.limitations.map((l) => (
            <li key={l}>{l}</li>
          ))}
        </ul>
      ) : null}
      {o.complete ? (
        <details>
          <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Week by week</summary>
          <div className="mt-3">
            <DataTable head={["Week", "Active", "Paid kits", "Sales", "Variable costs", "Contribution", "Fixed", "One-off", "Result", "Cumulative"]} label="Weeks" minWidth="64rem">
              {o.weeks.map((w) => (
                <tr key={String(w.week)} className={w.capacity_breach ? "bg-accent/10" : undefined}>
                  <td className="p-2">{String(w.week)}{w.capacity_breach ? " (over capacity)" : ""}</td>
                  <td className="type-figures p-2">{number.format(Number(w.active_start))}</td>
                  <td className="type-figures p-2">{number.format(Number(w.paid_kits))}</td>
                  {["sales_minor", "variable_costs_minor", "contribution_minor", "fixed_costs_minor", "investments_minor", "operating_result_minor", "cumulative_result_minor"].map((k) => (
                    <td key={k} className="type-figures p-2">
                      {money(Number(w[k]))}
                    </td>
                  ))}
                </tr>
              ))}
            </DataTable>
          </div>
        </details>
      ) : null}
      <p className="type-caption text-muted">
        Engine {o.engine_version} · inputs {o.input_hash.slice(0, 12)}…: the same inputs always give these results.
      </p>
    </div>
  );
}

export function Section({ title, note, actions, children }: { title: string; note?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="mt-8 first:mt-0">
      <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="type-h3">{title}</h2>
          {note ? <p className="type-body-sm mt-1 max-w-[56rem] text-muted">{note}</p> : null}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}
