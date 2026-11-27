import type { ScenarioRequest } from "@mise/api-client";
import { Tag } from "@mise/ui";
import Link from "next/link";

import { Empty, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { BriefingView, FactGrid, FactTile, formatValue, ProjectionView, RiskRow, Section, ToolFacts } from "@/components/mi-ui";
import { validDay } from "@/components/supply-ui";
import { formatDateTime, humanize, money } from "@/lib/format";
import { api, can } from "@/lib/session";

import { generateBriefing, narrateBriefing, saveScenario } from "../../mi-actions";
import type { TabProps } from "./page";

const str = (v: string | string[] | undefined) => (typeof v === "string" ? v : undefined);
const soft = <T,>(p: Promise<T>) => p.catch(() => null);

// ---------------------------------------------------------------- overview

export async function OverviewTab({ token, admin }: TabProps) {
  const [overview, readiness, risks, opps, recs, briefings] = await Promise.all([
    can(admin, "intelligence_executive.read") && can(admin, "analytics_executive.read") ? soft(api.mi.tool(token, "business_overview")) : null,
    soft(api.mi.readiness(token)),
    soft(api.mi.risks(token)),
    soft(api.mi.opportunities(token)),
    soft(api.mi.recommendations(token)),
    can(admin, "intelligence_executive.read") ? soft(api.mi.briefings(token)) : null,
  ]);
  const triggered = risks?.items.filter((r) => r.status === "triggered") ?? [];
  const openRecs = recs?.items.filter((r) => ["open", "saved", "assigned"].includes(r.status)) ?? [];
  const latest = briefings?.items[0];
  const notReady = readiness?.checks.filter((c) => c.status === "not_ready") ?? [];
  return (
    <>
      {readiness ? (
        <Panel
          title={`Data readiness: ${humanize(readiness.overall)}`}
          note="What the figures below can and cannot be relied on for. Missing data is shown as missing, never as zero."
        >
          {notReady.length ? (
            <ul className="type-body-sm grid gap-1">
              {notReady.map((c) => (
                <li key={c.key}>
                  <span className="font-semibold">{c.label}:</span> <span className="text-muted">{c.detail}</span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="type-body-sm">Every check is ready or limited.</p>
          )}
          <p className="type-caption mt-3">
            <Link className="underline underline-offset-4" href="/intelligence/command-center?tab=readiness">
              All checks
            </Link>
          </p>
        </Panel>
      ) : null}
      {overview ? (
        <Section
          title="Executive overview"
          note={`${overview.evidence.from} to ${overview.evidence.to}. Collected money is cash received, not recognised revenue. These operational figures include test-provider payments (shown separately); the Finance tab excludes them.`}
        >
          <ToolFacts
            r={overview}
            headline={["revenue.collected", "revenue.net_collected", "revenue.collected_test", "subscriptions.active", "subscriptions.new", "subscriptions.cancelled", "meals.kits_ordered", "delivery.first_attempt_rate"]}
          />
          {overview.limitations.length ? <div className="mt-4"><Notice tone="info" title="Limitations" items={overview.limitations} /></div> : null}
        </Section>
      ) : null}
      <div className="mt-8 grid gap-5 lg:grid-cols-3">
        <Panel title="Business risks" note="Rules with thresholds; no probabilities.">
          {triggered.length ? (
            <ul className="type-body-sm grid gap-2">
              {triggered.slice(0, 5).map((r) => (
                <li key={r.key}>
                  <Tag tone={r.severity === "high" ? "error" : "warning"}>{r.severity === "high" ? "High" : "Watch"}</Tag> {r.title}
                </li>
              ))}
            </ul>
          ) : (
            <p className="type-body-sm text-muted">No indicator you can see is triggered.</p>
          )}
          <Link className="type-caption mt-3 inline-block underline underline-offset-4" href="/intelligence/command-center?tab=risks">
            All risks and their evidence
          </Link>
        </Panel>
        <Panel title="Growth opportunities" note="Observations from records, not outcomes.">
          {opps?.items.length ? (
            <ul className="type-body-sm grid gap-2">
              {opps.items.slice(0, 5).map((o) => (
                <li key={o.key}>{o.title}</li>
              ))}
            </ul>
          ) : (
            <p className="type-body-sm text-muted">No observed opportunities in your areas.</p>
          )}
          <Link className="type-caption mt-3 inline-block underline underline-offset-4" href="/intelligence/command-center?tab=opportunities">
            All opportunities
          </Link>
        </Panel>
        <Panel title="Recommended actions" note="Items waiting for review.">
          <p className="type-figures font-display text-[1.75rem]">{openRecs.length}</p>
          <Link className="type-caption inline-block underline underline-offset-4" href="/intelligence/command-center?tab=recommendations">
            Review recommendations
          </Link>
          {latest ? (
            <p className="type-caption mt-3 text-muted">
              Latest briefing: {latest.cadence}, {latest.from} to {latest.to}.{" "}
              <Link className="underline" href="/intelligence/command-center?tab=briefing">
                Open
              </Link>
            </p>
          ) : null}
        </Panel>
      </div>
    </>
  );
}

// ---------------------------------------------------------------- briefing

export async function BriefingTab({ token, admin, sp }: TabProps) {
  const cadence = (["daily", "weekly", "monthly"].includes(str(sp.cadence) ?? "") ? str(sp.cadence) : "weekly") as "daily" | "weekly" | "monthly";
  const list = await api.mi.briefings(token, cadence);
  const id = str(sp.briefing);
  const b = id ? await soft(api.mi.briefing(token, id)) : list.items[0];
  const ai = await soft(api.mi.ai(token));
  return (
    <>
      <nav aria-label="Cadence" className="mb-4 flex flex-wrap gap-2">
        {(["daily", "weekly", "monthly"] as const).map((c) => (
          <Link
            key={c}
            href={`/intelligence/command-center?tab=briefing&cadence=${c}`}
            aria-current={c === cadence ? "page" : undefined}
            className={`type-body-sm rounded-md border px-3 py-1.5 font-semibold ${c === cadence ? "border-foreground bg-foreground text-surface" : "border-border-strong"}`}
          >
            {humanize(c)}
          </Link>
        ))}
      </nav>
      <div className="mb-6 flex flex-wrap items-start gap-4">
        <KForm action={generateBriefing} submit={`Prepare the ${cadence} briefing`} pending="Preparing…" label="Prepare briefing" inline>
          <input type="hidden" name="cadence" value={cadence} />
        </KForm>
        {b && !b.narrative && ai?.status.available && can(admin, "intelligence.ask") ? (
          <KForm action={narrateBriefing} submit="Write an AI summary" pending="Writing…" label="AI summary" variant="secondary" inline>
            <input type="hidden" name="id" value={b.id} />
          </KForm>
        ) : null}
      </div>
      <p className="type-caption mb-4 text-muted">
        The briefing covers the last complete {cadence === "daily" ? "day" : cadence === "weekly" ? "Monday-to-Sunday week" : "calendar month"}. Preparing it again with
        unchanged data keeps the same version.
      </p>
      {b ? <BriefingView b={b} /> : <Empty>No {cadence} briefing yet. Prepare one above.</Empty>}
      {list.items.length > 1 ? (
        <Section title="Earlier versions">
          <ul className="type-body-sm grid gap-1">
            {list.items.map((x) => (
              <li key={x.id}>
                <Link className="underline underline-offset-4" href={`/intelligence/command-center?tab=briefing&cadence=${cadence}&briefing=${x.id}`}>
                  {x.from} to {x.to}, version {x.version}
                </Link>{" "}
                <span className="text-muted">{formatDateTime(x.created_at)}</span>
              </li>
            ))}
          </ul>
        </Section>
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------- finance

function lastMonth() {
  const now = new Date();
  const first = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1));
  const last = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 0));
  return [first.toISOString().slice(0, 10), last.toISOString().slice(0, 10)] as const;
}

const coverageTone = { complete: "success", estimated: "accent", partial: "warning", missing: "error" } as const;

export async function FinanceTab({ token, sp }: TabProps) {
  const [df, dt] = lastMonth();
  const from = validDay(str(sp.from)) ?? df;
  const to = validDay(str(sp.to)) ?? dt;
  const s = await api.finance.summary(token, from, to);
  const risks = await soft(api.mi.risks(token, { from, to }));
  const finRisks = risks?.items.filter((r) => r.domain === "financial") ?? [];
  return (
    <>
      <form className="mb-6 flex flex-wrap items-end gap-3" aria-label="Period">
        <input type="hidden" name="tab" value="finance" />
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={to} />
        </Labelled>
        <button className="type-body-sm min-h-11 rounded-md border border-foreground px-4 font-semibold">Show</button>
        <Link className="type-body-sm ml-auto underline underline-offset-4" href="/finance/expenses">
          Expenses, recurring costs, coverage and cash
        </Link>
      </form>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">{s.statement}</p>
      <Section title="Results" note="A result is withheld (with the reasons) whenever a cost it needs is not recorded or confirmed. Estimates are labelled.">
        <FactGrid>
          {s.results.map((r) => (
            <FactTile
              key={r.key}
              label={r.label}
              value={r.withheld ? null : r.amount_minor}
              unit="money_minor"
              definition={r.formula}
              freshness={s.generated_at}
              note={r.withheld ? `Withheld: ${r.reasons.join(" ")}` : r.estimated ? "Includes estimates." : undefined}
            />
          ))}
        </FactGrid>
      </Section>
      <Section title="Lines">
        <ul className="divide-y divide-border border border-border bg-surface">
          {s.lines.map((l) => (
            <li key={l.key} className="flex flex-wrap items-start justify-between gap-3 p-3">
              <div>
                <p className="type-body-sm font-semibold">
                  {l.label} <Tag tone={coverageTone[l.coverage as keyof typeof coverageTone] ?? "neutral"}>{humanize(l.coverage)}</Tag>
                </p>
                <p className="type-caption text-muted">{[...l.notes, `Source: ${l.sources.join("; ")}`].join(" ")}</p>
              </div>
              <p className="type-figures font-semibold">{formatValue(l.amount_minor, "money_minor")}</p>
            </li>
          ))}
        </ul>
      </Section>
      <div className="mt-8 grid gap-5 lg:grid-cols-2">
        <Panel title="Expense coverage" note="A category counts only when recorded, or confirmed as nothing to record, for every month.">
          <ul className="type-body-sm grid gap-1">
            {s.categories.map((c) => (
              <li key={c.key} className="flex flex-wrap justify-between gap-2">
                <span>
                  {c.name} <Tag tone={coverageTone[c.coverage as keyof typeof coverageTone] ?? "neutral"}>{humanize(c.coverage)}</Tag>
                </span>
                <span className="type-figures">{money(c.actual_minor + c.estimated_minor)}</span>
              </li>
            ))}
          </ul>
        </Panel>
        <Panel title="Cash view" note="Collected customer payments are not bank cash.">
          <p className="type-body-sm">
            Inflows ({humanize(s.cash.inflow_basis)}): <span className="type-figures font-semibold">{money(s.cash.inflows_minor)}</span>
          </p>
          <p className="type-body-sm">
            Outflows (paid expenses): <span className="type-figures font-semibold">{money(s.cash.outflows_minor)}</span>
          </p>
          <p className="type-body-sm">
            Recorded balance: <span className="type-figures font-semibold">{formatValue(s.cash.recorded_balance_minor, "money_minor")}</span>
            {s.cash.recorded_balance_as_of ? ` as of ${s.cash.recorded_balance_as_of}` : ""}
          </p>
          {!s.cash.complete ? <p className="type-caption mt-2 text-muted">Incomplete: {s.cash.reasons.join(" ")}</p> : null}
        </Panel>
      </div>
      {finRisks.length ? (
        <Section title="Financial risks">
          <ul className="border border-border bg-surface px-4">
            {finRisks.map((r) => (
              <RiskRow key={r.key} r={r} />
            ))}
          </ul>
        </Section>
      ) : null}
      {s.notes.length ? <div className="mt-6"><Notice tone="info" title="Notes" items={s.notes} /></div> : null}
    </>
  );
}

// ---------------------------------------------------------------- outlook (scenarios)

const PRESETS = ["base", "conservative", "optimistic"] as const;

/** Values typed in the form: money in TTD dollars, rates in percent; converted to the engine's units. */
function readValues(sp: TabProps["sp"], vars: { key: string; unit: string }[]) {
  const values: Record<string, number> = {};
  for (const v of vars) {
    const raw = str(sp[`v_${v.key}`])?.replace(/[,\s]/g, "");
    if (!raw) continue;
    const n = Number(raw);
    if (!Number.isFinite(n)) continue;
    values[v.key] = v.unit === "money_minor" ? Math.round(n * 100) : v.unit === "basis_points" ? Math.round(n * 100) : Math.round(n);
  }
  return values;
}

function display(value: number | null | undefined, unit: string) {
  if (value === null || value === undefined) return "";
  if (unit === "money_minor") return String(value / 100);
  if (unit === "basis_points") return String(value / 100);
  return String(value);
}

export async function OutlookTab({ token, sp }: TabProps) {
  const model = await api.mi.scenarioModel(token);
  const vars = model.variables as { key: string; label: string; unit: string; required: boolean; description: string }[];
  const horizon = Number(str(sp.horizon) ?? 90);
  const preset = (PRESETS as readonly string[]).includes(str(sp.preset) ?? "") ? (str(sp.preset) as (typeof PRESETS)[number]) : "base";
  const values = readValues(sp, vars);
  const request: ScenarioRequest = { horizon_days: model.horizons.includes(horizon) ? (horizon as 30) : 90, preset, values };
  let projection = null;
  let problem: string | null = null;
  try {
    projection = await api.mi.runScenario(token, request);
  } catch (e) {
    problem = e instanceof Error ? e.message : "The scenario could not be run.";
  }
  const observed = new Map(model.observed.values.map((v) => [v.key, v]));
  const saved = await soft(api.mi.scenarios(token));
  return (
    <>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">
        A deterministic scenario engine. Observed values come from records for {model.observed.from} to {model.observed.to}; anything you type is an
        assumption and is labelled as one. {model.preset_note}
      </p>
      <form aria-label="Scenario" className="grid gap-4 border border-border bg-surface p-5">
        <input type="hidden" name="tab" value="outlook" />
        <div className="flex flex-wrap gap-4">
          <Labelled label="Horizon">
            <SelectInput name="horizon" defaultValue={String(request.horizon_days)}>
              {model.horizons.map((h) => (
                <option key={h} value={h}>
                  {h === 182 ? "Six months" : h === 365 ? "Twelve months" : `${h} days`}
                </option>
              ))}
            </SelectInput>
          </Labelled>
          <Labelled label="Preset">
            <SelectInput name="preset" defaultValue={preset}>
              {PRESETS.map((p) => (
                <option key={p} value={p}>
                  {humanize(p)}
                </option>
              ))}
            </SelectInput>
          </Labelled>
        </div>
        <div className="grid items-start gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {vars.map((v) => {
            const o = observed.get(v.key);
            const hint = !o
              ? "Not taken from records: no change (0 %) is assumed unless you enter a value."
              : o.value !== null && o.value !== undefined
                ? `Observed: ${formatValue(o.value, v.unit)}. ${o.basis}`
                : `Not observed: ${o.basis}`;
            return (
              <Labelled key={v.key} label={`${v.label}${v.unit === "money_minor" ? " (TTD)" : v.unit === "basis_points" ? " (%)" : ""}${v.required ? "" : " (optional)"}`} hint={hint}>
                <TextInput name={`v_${v.key}`} inputMode="decimal" defaultValue={display(values[v.key], v.unit)} placeholder={o?.value !== null && o?.value !== undefined ? display(o.value, v.unit) : "Enter an assumption"} />
              </Labelled>
            );
          })}
        </div>
        <div>
          <button className="type-body-sm min-h-11 rounded-md bg-primary px-5 font-semibold text-primary-foreground">Run scenario</button>
          <span className="type-caption ml-3 text-muted">Leave a field empty to use the observed value (or the preset&apos;s).</span>
        </div>
      </form>
      <div className="mt-6">
        {problem ? <Notice tone="error" title="The scenario cannot be run" items={[problem]} /> : null}
        {projection ? <ProjectionView p={projection} /> : null}
      </div>
      {projection?.output.complete ? (
        <Section title="Save this scenario" note="Saved scenarios keep immutable versions; identical inputs are not stored twice.">
          <KForm action={saveScenario} submit="Save scenario" pending="Saving…" label="Save scenario">
            <input type="hidden" name="request" value={JSON.stringify(request)} />
            <div className="grid items-start gap-4 sm:grid-cols-2">
              <Labelled label="Name">
                <TextInput name="name" maxLength={120} required />
              </Labelled>
              <Labelled label="Description (optional)">
                <TextInput name="description" maxLength={1000} />
              </Labelled>
            </div>
          </KForm>
        </Section>
      ) : null}
      {saved?.items.length ? (
        <Section title="Saved scenarios">
          <ul className="type-body-sm grid gap-1">
            {saved.items.map((x) => (
              <li key={x.id}>
                <Link className="underline underline-offset-4" href={`/intelligence/scenarios/${x.id}`}>
                  {x.name}
                </Link>{" "}
                <span className="text-muted">
                  {humanize(x.preset)} · {x.versions} version{x.versions === 1 ? "" : "s"}
                </span>
              </li>
            ))}
          </ul>
        </Section>
      ) : null}
    </>
  );
}
