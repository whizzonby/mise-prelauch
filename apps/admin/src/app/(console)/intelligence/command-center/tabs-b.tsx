import { Tag } from "@mise/ui";
import Link from "next/link";

import { DataTable, Empty, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { BasisTag, Evidence, EvidenceMap, formatValue, RiskRow, Section } from "@/components/mi-ui";
import { formatDateTime, humanize, money, number } from "@/lib/format";
import { api, can } from "@/lib/session";

import { createProposal, generateProposals, proposeRecommendation, reviewRecommendation, saveOpportunity, saveRisk, setThreshold } from "../../mi-actions";
import type { TabProps } from "./page";

const soft = <T,>(p: Promise<T>) => p.catch(() => null);

// ---------------------------------------------------------------- customers

export async function CustomersTab({ token }: TabProps) {
  const [f, segments, rs] = await Promise.all([soft(api.mi.funnel(token)), soft(api.mi.segments(token)), soft(api.mi.cancellationReasons(token))]);
  const segs = segments?.items ?? [];
  return (
    <div className="grid gap-5 lg:grid-cols-2">
      <Panel
        title="Conversion funnel"
        note={f ? `${f.from} to ${f.to}. First-party events from the customer app (none under Global Privacy Control or Do Not Track), joined with real payments.` : undefined}
      >
        {f?.steps.length ? (
          <>
            <ul className="type-body-sm grid gap-2">
              {f.steps.map((s) => (
                <li key={s.key} className="flex justify-between gap-3">
                  <span>
                    {s.label} <span className="type-caption text-muted">({s.source})</span>
                  </span>
                  <span className="type-figures font-semibold">{number.format(s.people)}</span>
                </li>
              ))}
            </ul>
            <p className="type-body-sm mt-3">
              Checkouts started {number.format(f.checkouts_started)}, abandoned {number.format(f.checkouts_abandoned)}, pending {number.format(f.checkouts_pending)}.
            </p>
            <p className="type-caption mt-1 text-muted">{f.abandon_rule}</p>
          </>
        ) : (
          <Empty>No funnel data yet.</Empty>
        )}
        {f?.limitations.length ? <p className="type-caption mt-3 text-muted">{f.limitations.join(" ")}</p> : null}
      </Panel>
      <Panel title="Segments" note="Explainable rules only: no sensitive attributes, no churn scores.">
        {segs.length ? (
          <ul className="type-body-sm grid gap-2">
            {segs.map((s) => (
              <li key={s.key}>
                <span className="flex justify-between gap-3">
                  <span className="font-semibold">{s.name}</span>
                  <span className="type-figures">{number.format(s.count)}</span>
                </span>
                <span className="type-caption text-muted">{s.definition}</span>
              </li>
            ))}
          </ul>
        ) : (
          <Empty>No segments.</Empty>
        )}
      </Panel>
      <Panel title="Cancellation reasons" note={rs?.note}>
        {rs?.items.length ? (
          <ul className="type-body-sm grid gap-1">
            {rs.items.map((r) => (
              <li key={r.reason} className="flex justify-between gap-3">
                <span>{humanize(r.reason)}</span>
                <span className="type-figures">{number.format(r.count)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <Empty>No cancellations with a reason in the period.</Empty>
        )}
      </Panel>
    </div>
  );
}

// ---------------------------------------------------------------- meals and proposals

const LABELS: Record<string, string> = {
  high_demand: "High demand",
  availability_constrained: "Offered too rarely",
  underperforming: "Underperforming",
  high_cost: "High cost",
};

export async function MealsTab({ token, admin }: TabProps) {
  const [mi, proposals, ai] = await Promise.all([
    can(admin, "analytics_production.read") ? soft(api.mi.meals(token)) : null,
    soft(api.mi.proposals(token)),
    soft(api.mi.ai(token)),
  ]);
  const review = can(admin, "intelligence_recommendation.review");
  return (
    <>
      {mi ? (
        <Section title="Meal performance" note={`${mi.from} to ${mi.to}: ${mi.weeks_in_period} weeks with a published menu. Demand is kits per week on the menu, so availability is taken into account.`}>
          <DataTable head={["Meal", "Weeks on menu", "Kits ordered", "Kits / week on menu", "Repeat customers", "Cost / serving", "Labels"]} label="Meals" minWidth="56rem">
            {mi.meals.map((m) => (
              <tr key={m.meal_id}>
                <td className="p-3">
                  <span className="font-semibold">{m.name}</span>
                  {m.demo ? <> <Tag tone="neutral">Demo</Tag></> : null}
                  <span className="type-caption block text-muted">{m.category}</span>
                </td>
                <td className="type-figures p-3">{m.weeks_on_menu}</td>
                <td className="type-figures p-3">{number.format(m.kits_ordered)}</td>
                <td className="type-figures p-3">{m.kits_per_week_on_menu ?? "Not on a menu"}</td>
                <td className="type-figures p-3">
                  {m.repeat_customers} of {m.customers}
                </td>
                <td className="p-3">
                  <span className="type-figures">{m.cost_complete && m.cost_per_serving_minor !== null ? money(m.cost_per_serving_minor) : "Not available"}</span>
                  <span className="type-caption block text-muted">{m.cost_note}</span>
                </td>
                <td className="p-3">
                  {m.labels.map((l) => (
                    <Tag key={l} tone={l === "high_demand" ? "success" : l === "availability_constrained" ? "accent" : "warning"}>
                      {LABELS[l] ?? l}
                    </Tag>
                  ))}
                </td>
              </tr>
            ))}
          </DataTable>
          <Evidence title="Rules and limitations">
            {[...mi.rules, ...mi.limitations].map((r) => (
              <p key={r}>{r}</p>
            ))}
          </Evidence>
        </Section>
      ) : null}
      <Section
        title="Meal proposals"
        note="Draft concepts only. A proposal never creates or publishes a meal: once approved for development, a chef writes a recipe through the culinary workflow (validation, nutrition and allergen review, costing, publishing approval) and the proposal links to it."
      >
        {proposals?.items.length ? (
          <DataTable head={["Proposal", "Origin", "Status", "Created"]} label="Meal proposals" minWidth="36rem">
            {proposals.items.map((p) => (
              <tr key={p.id}>
                <td className="p-3">
                  <Link className="font-semibold underline underline-offset-4" href={`/intelligence/proposals/${p.id}`}>
                    {p.name}
                  </Link>
                </td>
                <td className="p-3">{p.origin === "ai" ? <Tag tone="accent">AI draft</Tag> : <Tag tone="neutral">Manager</Tag>}</td>
                <td className="p-3">{humanize(p.status)}</td>
                <td className="p-3">{formatDateTime(p.created_at)}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No proposals yet.</Empty>
        )}
      </Section>
      {review ? (
        <div className="mt-6 grid gap-5 lg:grid-cols-2">
          <Panel title="Propose a meal concept" note="One ingredient per line: name | quantity | note. Names are matched to the ingredient catalogue.">
            <KForm action={createProposal} submit="Add proposal" pending="Adding…" label="New meal proposal">
              <Labelled label="Name">
                <TextInput name="name" maxLength={120} required />
              </Labelled>
              <Labelled label="Description">
                <TextArea name="description" rows={3} maxLength={2000} />
              </Labelled>
              <Labelled label="Target audience">
                <TextInput name="target_audience" maxLength={500} />
              </Labelled>
              <Labelled label="Production complexity">
                <SelectInput name="complexity" defaultValue="unknown">
                  {["unknown", "low", "medium", "high"].map((c) => (
                    <option key={c} value={c}>
                      {humanize(c)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Ingredients">
                <TextArea name="ingredients" rows={4} placeholder={"Callaloo | 200 g | fresh\nCoconut milk | 400 ml"} />
              </Labelled>
            </KForm>
          </Panel>
          <Panel title="AI draft concepts" note="Up to three drafts from the evidence pack (demand, approved supplier products, availability, preferences). Numbers not in the evidence are marked [unverified].">
            {ai?.status.available && can(admin, "intelligence.ask") ? (
              <KForm action={generateProposals} submit="Draft concepts" pending="Drafting…" label="Draft concepts with AI">
                <Labelled label="Brief (optional)">
                  <TextInput name="brief" maxLength={1000} placeholder="e.g. vegetarian weeknight meals" />
                </Labelled>
              </KForm>
            ) : (
              <p className="type-body-sm text-muted">AI unavailable{ai ? `: ${ai.status.reason}` : "."} Proposals can still be written by a manager.</p>
            )}
          </Panel>
        </div>
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------- operations

export async function OperationsTab({ token, admin }: TabProps) {
  const [risks, opps] = await Promise.all([soft(api.mi.risks(token)), soft(api.mi.opportunities(token))]);
  const ops = risks?.items.filter((r) => r.domain === "operational") ?? [];
  const oo = opps?.items.filter((o) => o.domain === "operational") ?? [];
  const links = [
    ["analytics_production.read", "/intelligence/production", "Production: planned vs actual, accepted, rejected, rework, holds"],
    ["analytics_procurement.read", "/intelligence/procurement", "Procurement: supplier delays, rejected deliveries, overdue orders"],
    ["analytics_inventory.read", "/intelligence/inventory", "Inventory: stock value, expiry, quarantine, waste"],
    ["analytics_delivery.read", "/intelligence/delivery", "Delivery: first-attempt and on-time rates, failures, costs"],
  ] as const;
  return (
    <>
      <Section title="Operational risks" note="Production, supply and delivery indicators. Food safety controls and quality holds are never overridden here.">
        {ops.length ? (
          <ul className="border border-border bg-surface px-4">
            {ops.map((r) => (
              <RiskRow key={r.key} r={r} />
            ))}
          </ul>
        ) : (
          <Empty>No operational indicators for your role.</Empty>
        )}
      </Section>
      <Section title="Operational opportunities">
        {oo.length ? (
          <ul className="type-body-sm grid gap-2">
            {oo.map((o) => (
              <li key={o.key}>
                <span className="font-semibold">{o.title}</span> <span className="text-muted">{o.summary}</span>
              </li>
            ))}
          </ul>
        ) : (
          <Empty>No observed operational opportunities.</Empty>
        )}
      </Section>
      <Section title="Detailed figures">
        <ul className="type-body-sm grid gap-1">
          {links
            .filter(([p]) => can(admin, p))
            .map(([, href, label]) => (
              <li key={href}>
                <Link className="underline underline-offset-4" href={href}>
                  {label}
                </Link>
              </li>
            ))}
        </ul>
      </Section>
    </>
  );
}

// ---------------------------------------------------------------- risks

export async function RisksTab({ token, admin }: TabProps) {
  const [risks, thresholds] = await Promise.all([api.mi.risks(token), soft(api.mi.thresholds(token))]);
  const review = can(admin, "intelligence_recommendation.review");
  const order = { triggered: 0, not_evaluable: 1, clear: 2 } as Record<string, number>;
  const items = [...risks.items].sort((a, b) => (order[a.status] ?? 3) - (order[b.status] ?? 3) || (a.severity === "high" ? -1 : 1));
  return (
    <>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">{risks.note} Each indicator lists its figures, comparison period and threshold under Evidence.</p>
      {items.length ? (
        <ul className="border border-border bg-surface px-4">
          {items.map((r) => (
            <RiskRow
              key={r.key}
              r={r}
              action={
                review && r.status === "triggered" ? (
                  <div className="mt-2">
                    <KForm action={saveRisk} submit="Save for review" pending="Saving…" label={`Save ${r.title}`} variant="secondary" inline>
                      <input type="hidden" name="key" value={r.key} />
                      <input type="hidden" name="from" value={r.from} />
                      <input type="hidden" name="to" value={r.to} />
                    </KForm>
                  </div>
                ) : undefined
              }
            />
          ))}
        </ul>
      ) : (
        <Empty>No indicators are available for your role.</Empty>
      )}
      {thresholds ? (
        <Section title="Thresholds" note="Engineering defaults until an owner changes them. Changes need a reason and are audited.">
          <DataTable head={["Indicator", "Watch", "High", "Status", ""]} label="Risk thresholds" minWidth="48rem">
            {thresholds.items.map((t) => (
              <tr key={t.key}>
                <td className="p-3">
                  {humanize(t.key)}
                  <span className="type-caption block text-muted">
                    Worse is {t.direction === "above" ? "higher" : "lower"} · {t.unit}
                  </span>
                </td>
                <td className="type-figures p-3">{formatValue(t.warn, t.unit)}</td>
                <td className="type-figures p-3">{formatValue(t.critical, t.unit)}</td>
                <td className="p-3">
                  {t.is_default ? <Tag tone="neutral">Default</Tag> : <Tag tone="accent">Set by owner</Tag>} {t.enabled ? null : <Tag tone="warning">Off</Tag>}
                </td>
                <td className="p-3">
                  {can(admin, "intelligence.configure") ? (
                    <details>
                      <summary className="type-caption cursor-pointer underline">Change</summary>
                      <KForm action={setThreshold} submit="Save" pending="Saving…" label={`Threshold ${t.key}`}>
                        <input type="hidden" name="key" value={t.key} />
                        <Labelled label="Watch at">
                          <TextInput name="warn" inputMode="decimal" defaultValue={String(t.warn)} />
                        </Labelled>
                        <Labelled label="High at">
                          <TextInput name="critical" inputMode="decimal" defaultValue={String(t.critical)} />
                        </Labelled>
                        <label className="type-body-sm flex items-center gap-2">
                          <input type="checkbox" name="enabled" defaultChecked={t.enabled} /> Indicator on
                        </label>
                        <Labelled label="Reason">
                          <TextInput name="note" maxLength={300} required />
                        </Labelled>
                      </KForm>
                    </details>
                  ) : null}
                </td>
              </tr>
            ))}
          </DataTable>
        </Section>
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------- opportunities

export async function OpportunitiesTab({ token, admin }: TabProps) {
  const opps = await api.mi.opportunities(token);
  const review = can(admin, "intelligence_recommendation.review");
  return (
    <>
      <p className="type-body-sm mb-4 max-w-[56rem] text-muted">{opps.note}</p>
      {opps.items.length ? (
        <ul className="grid gap-3">
          {opps.items.map((o) => (
            <li key={o.key} className="border border-border bg-surface p-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="max-w-[48rem]">
                  <p className="type-body font-semibold">
                    {o.title} <BasisTag basis={o.basis} />
                  </p>
                  <p className="type-body-sm mt-1">{o.summary}</p>
                  <p className="type-body-sm mt-1">
                    Review:{" "}
                    <Link className="underline underline-offset-4" href={o.review_href}>
                      {o.review}
                    </Link>
                  </p>
                </div>
                {review ? (
                  <KForm action={saveOpportunity} submit="Save for review" pending="Saving…" label={`Save ${o.title}`} variant="secondary" inline>
                    <input type="hidden" name="key" value={o.key} />
                  </KForm>
                ) : null}
              </div>
              <Evidence>
                <EvidenceMap ev={o.evidence} />
                <p>{o.caution}</p>
              </Evidence>
            </li>
          ))}
        </ul>
      ) : (
        <Empty>No opportunities observed in your areas. Opportunities appear when records show them (for example a meal that sells well but is rarely on the menu).</Empty>
      )}
    </>
  );
}

// ---------------------------------------------------------------- recommendations

const DOMAINS = ["general", "executive", "financial", "customer", "operational"] as const;
const DOMAIN_PERM = {
  general: "intelligence.read",
  executive: "intelligence_executive.read",
  financial: "intelligence_financial.read",
  customer: "intelligence_customer.read",
  operational: "intelligence_operational.read",
} as const;

export async function RecommendationsTab({ token, admin, sp }: TabProps) {
  const status = typeof sp.status === "string" ? sp.status : "";
  const recs = await api.mi.recommendations(token, status);
  const review = can(admin, "intelligence_recommendation.review");
  const actions = (s: string) =>
    ({ open: ["save", "dismiss", "validate"], saved: ["done", "dismiss", "validate"], assigned: ["done", "dismiss", "validate"] })[s] ?? [];
  return (
    <>
      <Notice tone="info" title="Advisory only" items={[recs.advisory]} />
      <nav aria-label="Status" className="my-4 flex flex-wrap gap-2">
        {["", "open", "saved", "assigned", "done", "dismissed"].map((s) => (
          <Link
            key={s || "all"}
            href={`/intelligence/command-center?tab=recommendations${s ? `&status=${s}` : ""}`}
            aria-current={s === status ? "page" : undefined}
            className={`type-body-sm rounded-md border px-3 py-1.5 font-semibold ${s === status ? "border-foreground bg-foreground text-surface" : "border-border-strong"}`}
          >
            {s ? humanize(s) : "All"}
          </Link>
        ))}
      </nav>
      {recs.items.length ? (
        <ul className="grid gap-3">
          {recs.items.map((r) => (
            <li key={r.id} className="border border-border bg-surface p-4">
              <p className="type-body font-semibold">
                {r.title} <BasisTag basis={r.basis} /> <Tag tone="neutral">{humanize(r.status)}</Tag>
              </p>
              <p className="type-body-sm mt-1">{r.summary}</p>
              <p className="type-caption mt-1 text-muted">
                {humanize(r.kind)} · {humanize(r.domain)} · {formatDateTime(r.created_at)}
                {r.review_href ? (
                  <>
                    {" "}
                    ·{" "}
                    <Link className="underline" href={r.review_href}>
                      Where to act
                    </Link>
                  </>
                ) : null}
              </p>
              <Evidence>
                <pre className="whitespace-pre-wrap break-words">{JSON.stringify(r.evidence, null, 2).slice(0, 4000)}</pre>
              </Evidence>
              {review && actions(r.status).length ? (
                <div className="mt-3">
                  <KForm action={reviewRecommendation} submit="Apply" pending="Saving…" label={`Review ${r.title}`} inline>
                    <input type="hidden" name="id" value={r.id} />
                    <Labelled label="Action">
                      <SelectInput name="action" defaultValue={actions(r.status)[0]}>
                        {actions(r.status).map((a) => (
                          <option key={a} value={a}>
                            {humanize(a)}
                          </option>
                        ))}
                      </SelectInput>
                    </Labelled>
                    <Labelled label="Note (needed to dismiss or validate)">
                      <TextInput name="note" maxLength={1000} />
                    </Labelled>
                  </KForm>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <Empty>Nothing here. Save a risk or an opportunity, or record a proposal below.</Empty>
      )}
      {review ? (
        <Section title="Record a business proposal" note="Your own proposal, labelled as such until a reviewer validates it.">
          <KForm action={proposeRecommendation} submit="Record proposal" pending="Saving…" label="New business proposal" reset>
            <div className="grid items-start gap-4 sm:grid-cols-2">
              <Labelled label="Area">
                <SelectInput name="domain" defaultValue="general">
                  {DOMAINS.filter((d) => can(admin, DOMAIN_PERM[d])).map((d) => (
                    <option key={d} value={d}>
                      {humanize(d)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Title">
                <TextInput name="title" maxLength={200} required />
              </Labelled>
            </div>
            <Labelled label="Summary">
              <TextArea name="summary" rows={3} maxLength={2000} />
            </Labelled>
          </KForm>
        </Section>
      ) : null}
    </>
  );
}
