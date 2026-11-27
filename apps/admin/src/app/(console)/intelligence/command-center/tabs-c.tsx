import { Tag } from "@mise/ui";
import Link from "next/link";

import { DataTable, Empty, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { AIUnavailable, Section } from "@/components/mi-ui";
import { formatDateTime, humanize, number } from "@/lib/format";
import { api, can } from "@/lib/session";

import { askMise, saveAISettings, saveBriefingSchedule } from "../../mi-actions";
import type { TabProps } from "./page";

const soft = <T,>(p: Promise<T>) => p.catch(() => null);
const usd = (micros: number | null | undefined) => (micros === null || micros === undefined ? "" : String(micros / 1_000_000));
const usdText = (micros: number | null | undefined) => (micros === null || micros === undefined ? "Not set" : `US$${(micros / 1_000_000).toLocaleString("en-GB", { maximumFractionDigits: 4 })}`);

const EXAMPLES = [
  "How is our business performing this month?",
  "What are our biggest expenses?",
  "How many subscribers do we need to break even?",
  "Which meals sell best when they are on the menu?",
  "Which risks should management review first?",
];

// ---------------------------------------------------------------- ask

export async function AskTab({ token, admin }: TabProps) {
  const info = await api.mi.ai(token);
  const runs = await soft(api.mi.runs(token, 10));
  return (
    <>
      {!info.status.available ? (
        <AIUnavailable reason={info.status.reason} />
      ) : !info.can_ask || !can(admin, "intelligence.ask") ? (
        <p className="type-body-sm text-muted">Your role does not include asking Mise AI.</p>
      ) : (
        <Panel
          title="Ask Mise AI"
          note={`Answers use only the read-only tools your permissions allow; every number is checked against them, and anything unsupported is removed and marked. Today: ${info.usage.today_requests} of ${info.usage.today_limit} questions.`}
        >
          <KForm action={askMise} submit="Ask" pending="Working it out…" label="Ask Mise AI">
            <Labelled label="Question">
              <TextArea name="question" rows={3} maxLength={2000} required placeholder={EXAMPLES[0]} />
            </Labelled>
            <div className="grid items-start gap-4 sm:grid-cols-2">
              <Labelled label="From (optional)">
                <TextInput type="date" name="from" />
              </Labelled>
              <Labelled label="To (optional)">
                <TextInput type="date" name="to" />
              </Labelled>
            </div>
          </KForm>
          <p className="type-caption mt-3 text-muted">Try: {EXAMPLES.join(" · ")}</p>
        </Panel>
      )}
      <Section title="Your recent questions">
        <RunList runs={runs?.items ?? []} />
      </Section>
    </>
  );
}

function RunList({ runs }: { runs: { id: string; kind: string; question: string; status: string; review_status: string; created_at: string; model: string }[] }) {
  if (!runs.length) return <Empty>No questions yet.</Empty>;
  return (
    <DataTable head={["Question", "Kind", "Status", "Review", "When"]} label="Intelligence history" minWidth="44rem">
      {runs.map((r) => (
        <tr key={r.id}>
          <td className="p-3">
            <Link className="underline underline-offset-4" href={`/intelligence/runs/${r.id}`}>
              {r.question || humanize(r.kind)}
            </Link>
          </td>
          <td className="p-3">{humanize(r.kind)}</td>
          <td className="p-3">
            <Tag tone={r.status === "completed" ? "success" : r.status === "partial" ? "warning" : "error"}>{humanize(r.status)}</Tag>
          </td>
          <td className="p-3">{humanize(r.review_status)}</td>
          <td className="p-3">{formatDateTime(r.created_at)}</td>
        </tr>
      ))}
    </DataTable>
  );
}

// ---------------------------------------------------------------- saved

export async function SavedTab({ token, admin }: TabProps) {
  const [scenarios, briefings] = await Promise.all([
    can(admin, "intelligence_scenario.run") ? soft(api.mi.scenarios(token)) : null,
    can(admin, "intelligence_executive.read") ? soft(api.mi.briefings(token)) : null,
  ]);
  return (
    <div className="grid gap-5 lg:grid-cols-2">
      <Panel title="Saved scenarios" note="Immutable versions with their inputs and sources.">
        {scenarios?.items.length ? (
          <ul className="type-body-sm grid gap-1">
            {scenarios.items.map((s) => (
              <li key={s.id}>
                <Link className="underline underline-offset-4" href={`/intelligence/scenarios/${s.id}`}>
                  {s.name}
                </Link>{" "}
                <span className="text-muted">
                  {humanize(s.preset)}, {s.versions} version{s.versions === 1 ? "" : "s"}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <Empty>{scenarios ? "No saved scenarios." : "Scenarios need the scenario permission."}</Empty>
        )}
      </Panel>
      <Panel title="Briefings" note="Every stored version, with its period.">
        {briefings?.items.length ? (
          <ul className="type-body-sm grid gap-1">
            {briefings.items.map((b) => (
              <li key={b.id}>
                <Link className="underline underline-offset-4" href={`/intelligence/command-center?tab=briefing&cadence=${b.cadence}&briefing=${b.id}`}>
                  {humanize(b.cadence)}: {b.from} to {b.to}, v{b.version}
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <Empty>{briefings ? "No briefings yet." : "Briefings need executive access."}</Empty>
        )}
      </Panel>
    </div>
  );
}

// ---------------------------------------------------------------- history

export async function HistoryTab({ token, admin }: TabProps) {
  const runs = await api.mi.runs(token, 100);
  return (
    <>
      <p className="type-body-sm mb-4 text-muted">
        {can(admin, "intelligence.configure") ? "Every intelligence run in the organisation." : "Your own intelligence runs."} Each keeps the provider, model, prompt version,
        tools called, evidence, limitations, validation, tokens and estimated cost, for the retention period set by the owner.
      </p>
      <RunList runs={runs.items} />
    </>
  );
}

// ---------------------------------------------------------------- readiness

export async function ReadinessTab({ token }: TabProps) {
  const r = await api.mi.readiness(token);
  const tone = { ready: "success", limited: "warning", not_ready: "error" } as const;
  return (
    <>
      <p className="type-body-sm mb-4 text-muted">
        Overall: <Tag tone={tone[r.overall]}>{humanize(r.overall)}</Tag> · checked {formatDateTime(r.generated_at)}. What is not ready is shown as missing in every view, never as zero.
      </p>
      <ul className="divide-y divide-border border border-border bg-surface">
        {r.checks.map((c) => (
          <li key={c.key} className="flex flex-wrap items-start justify-between gap-3 p-4">
            <div className="max-w-[48rem]">
              <p className="type-body-sm font-semibold">{c.label}</p>
              <p className="type-body-sm text-muted">{c.detail}</p>
            </div>
            <Tag tone={tone[c.status]}>{humanize(c.status)}</Tag>
          </li>
        ))}
      </ul>
    </>
  );
}

// ---------------------------------------------------------------- settings

export async function SettingsTab({ token }: TabProps) {
  const [info, schedules, staff] = await Promise.all([api.mi.ai(token), api.mi.briefingSchedules(token), soft(api.admin.staff(token))]);
  const s = info.settings;
  const u = info.usage;
  const recipients = (staff?.items ?? []).filter((x) => x.roles?.some((r: string) => r === "super_admin" || r === "admin"));
  return (
    <>
      <Section title="AI provider" note="The provider, model and key are set on the server (AI_PROVIDER, AI_MODEL, AI_API_KEY) and never shown here. AI stays off until it is switched on with a budget, prices and an approval.">
        <div className="mb-4 grid gap-3 sm:grid-cols-3">
          <Panel title="Status">
            <p className="type-body-sm">
              {info.status.available ? <Tag tone="success">Available</Tag> : <Tag tone="neutral">Unavailable</Tag>} {info.status.provider || "No provider"}
              {info.status.model ? ` · ${info.status.model}` : ""}
            </p>
            {!info.status.available ? <p className="type-caption mt-1 text-muted">{info.status.reason}</p> : null}
          </Panel>
          <Panel title="This month" note={u.note}>
            <p className="type-body-sm">
              {number.format(u.month_requests)} requests · {number.format(u.month_input_tokens + u.month_output_tokens)} tokens
            </p>
            <p className="type-body-sm">
              Estimated {usdText(u.month_cost_usd_micros)}
              {u.budget_usd_micros === null ? " · no budget set" : ` of a ${usdText(u.budget_usd_micros)} budget`}
            </p>
          </Panel>
          <Panel title="Prompt">
            <p className="type-body-sm">{info.prompt_version}</p>
          </Panel>
        </div>
        {s ? (
          <Panel title="Limits and budget">
            <KForm action={saveAISettings} submit="Save AI settings" pending="Saving…" label="AI settings">
              <label className="type-body-sm flex items-center gap-2">
                <input type="checkbox" name="enabled" defaultChecked={s.enabled} /> AI switched on
              </label>
              <div className="grid items-start gap-4 sm:grid-cols-3">
                <Labelled label="Monthly budget (US$)" hint="Empty: no budget, AI unavailable">
                  <TextInput name="budget_usd" inputMode="decimal" defaultValue={usd(s.monthly_budget_usd_micros)} />
                </Labelled>
                <Labelled label="Input price (US$ per million tokens)">
                  <TextInput name="input_price_usd" inputMode="decimal" defaultValue={usd(s.input_price_usd_micros_per_mtok)} />
                </Labelled>
                <Labelled label="Output price (US$ per million tokens)">
                  <TextInput name="output_price_usd" inputMode="decimal" defaultValue={usd(s.output_price_usd_micros_per_mtok)} />
                </Labelled>
              </div>
              <Labelled label="Where the prices come from" hint="The provider's price page and the date read">
                <TextInput name="price_reference" maxLength={300} defaultValue={s.price_reference} />
              </Labelled>
              <div className="grid items-start gap-4 sm:grid-cols-3">
                <Labelled label="Questions per person per day">
                  <TextInput type="number" name="user_daily_requests" min={1} max={1000} defaultValue={s.user_daily_requests} />
                </Labelled>
                <Labelled label="Tool calls per question">
                  <TextInput type="number" name="max_tool_calls" min={1} max={20} defaultValue={s.max_tool_calls} />
                </Labelled>
                <Labelled label="Seconds per question">
                  <TextInput type="number" name="max_seconds" min={10} max={300} defaultValue={s.max_seconds} />
                </Labelled>
                <Labelled label="Output tokens per reply">
                  <TextInput type="number" name="max_output_tokens" min={200} max={8000} defaultValue={s.max_output_tokens} />
                </Labelled>
                <Labelled label="Input tokens per question">
                  <TextInput type="number" name="max_input_tokens" min={2000} max={200000} defaultValue={s.max_input_tokens} />
                </Labelled>
                <Labelled label="Keep history for (days)">
                  <TextInput type="number" name="retention_days" min={7} max={3650} defaultValue={s.retention_days} />
                </Labelled>
              </div>
              <Labelled label="Approval reference" hint="Who approved the provider, the data processing and the budget">
                <TextInput name="approval_ref" maxLength={300} defaultValue={s.approval_ref} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </Section>
      <Section title="Briefing schedules" note={schedules.note}>
        <div className="grid gap-4 lg:grid-cols-3">
          {schedules.items.map((sc) => (
            <Panel key={sc.cadence} title={humanize(sc.cadence)} note={sc.last_period_from ? `Last sent for the period from ${sc.last_period_from}.` : "Not sent yet."}>
              <KForm action={saveBriefingSchedule} submit="Save schedule" pending="Saving…" label={`${sc.cadence} schedule`}>
                <input type="hidden" name="cadence" value={sc.cadence} />
                <label className="type-body-sm flex items-center gap-2">
                  <input type="checkbox" name="enabled" defaultChecked={sc.enabled} /> On
                </label>
                <Labelled label="Recipients" hint="Staff who may read executive intelligence">
                  <SelectInput name="recipients" multiple defaultValue={sc.recipients} className="min-h-24">
                    {recipients.map((x) => (
                      <option key={x.id} value={x.id}>
                        {x.email}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Approval reference">
                  <TextInput name="approval_ref" maxLength={300} defaultValue={sc.approval_ref} />
                </Labelled>
              </KForm>
            </Panel>
          ))}
        </div>
      </Section>
    </>
  );
}
