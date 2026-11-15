import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { CountTile } from "@/components/delivery-ui";
import { ACTION_LABELS, automationTabs, PathTabs } from "@/components/intelligence-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { evaluateRule, saveRule } from "../intelligence-actions";

export const metadata: Metadata = { title: "Automation" };

/**
 * Rules are typed recipes, not code. Each ships switched off with its
 * thresholds unset: the business decides them, and a rule cannot be enabled
 * until they, a cooldown and an approval reference are recorded. Automation
 * only raises alerts, creates tasks or asks a person to approve; it never
 * refunds, charges, approves, overrides or dispatches.
 */
export default async function AutomationPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "automation.read")) notFound();
  const [o, rules] = await Promise.all([api.intelligence.automationOverview(token), api.intelligence.rules(token)]);
  const manage = can(admin, "automation.manage");
  const execute = can(admin, "automation.execute");
  return (
    <>
      <PageHeader title="Automation" description="Rules that watch operational records and bring the right people in. Every run is recorded; nothing financial or operational is done automatically." />
      <PathTabs tabs={automationTabs(can(admin, "automation.read"))} current="/automation" />
      <div className="grid gap-6 [&>*]:min-w-0">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
          <CountTile label="Rules on" value={o.enabled} />
          <CountTile label="Rules off" value={o.disabled} />
          <CountTile label="Failed executions" value={o.failed_executions} href="/automation/executions?status=failed" tone={o.failed_executions ? "bad" : undefined} />
          <CountTile label="Awaiting approval" value={o.awaiting_approval} href="/automation/executions?status=awaiting_approval" tone={o.awaiting_approval ? "warn" : undefined} />
          <CountTile label="Open tasks" value={o.open_tasks} href="/automation/tasks" />
        </div>
        <p className="type-body-sm text-muted">
          Last successful action: {o.last_success ? formatDateTime(o.last_success) : "none yet"} · {o.executions_last_7_days} execution(s) in the last 7 days.
        </p>
        {rules.items.map((r) => (
          <Panel key={r.id} title={r.name} note={r.description}>
            <div className="mb-4 flex flex-wrap items-center gap-3">
              <StatusTag status={r.enabled ? "active" : "draft"} label={r.enabled ? "On" : "Off"} />
              <span className="type-caption text-muted">
                Version {r.version} · owner: {r.owner_permission} · triggers: {r.event_triggers.length ? `${r.event_triggers.join(", ")} and the schedule` : "the schedule"}
                {r.last_evaluated_at ? ` · last checked ${formatDateTime(r.last_evaluated_at)}` : ""}
                {r.failed_executions ? ` · ${r.failed_executions} failed` : ""}
              </span>
            </div>
            {r.missing.length ? <Notice tone="warning" title="Needs a business decision before it can be switched on" items={r.missing} /> : null}
            <div className="mt-4">
              <KForm action={saveRule} submit="Save rule" pending="Saving…" label={`Configure ${r.name}`}>
                <input type="hidden" name="id" value={r.id} />
                <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                  {r.param_specs.map((p) => (
                    <Labelled key={p.name} label={`${p.label} (${p.unit})`} hint={`${p.min} to ${p.max}`}>
                      <TextInput type="number" name={`param_${p.name}`} min={p.min} max={p.max} defaultValue={r.params[p.name] ?? ""} disabled={!manage} />
                    </Labelled>
                  ))}
                  <Labelled label="Action">
                    <SelectInput name="action" defaultValue={r.action} disabled={!manage}>
                      {Object.entries(ACTION_LABELS).map(([k, v]) => (
                        <option key={k} value={k}>
                          {v}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Priority">
                    <SelectInput name="priority" defaultValue={r.priority} disabled={!manage}>
                      {["low", "normal", "high", "critical"].map((k) => (
                        <option key={k} value={k}>
                          {humanize(k)}
                        </option>
                      ))}
                    </SelectInput>
                  </Labelled>
                  <Labelled label="Cooldown (minutes)" hint="How long before the same record can trigger again">
                    <TextInput type="number" name="cooldown_minutes" min={1} max={43200} defaultValue={r.cooldown_minutes ?? ""} disabled={!manage} />
                  </Labelled>
                  <Labelled label="Approval reference" hint="The decision that approved this rule">
                    <TextInput name="approval_ref" defaultValue={r.approval_ref} maxLength={200} disabled={!manage} />
                  </Labelled>
                </div>
                <label className="flex min-h-11 items-center gap-3">
                  <input type="checkbox" name="enabled" defaultChecked={r.enabled} disabled={!manage} className="size-5" />
                  Rule on
                </label>
              </KForm>
            </div>
            {execute && r.enabled ? (
              <div className="mt-3">
                <KForm action={evaluateRule} submit="Check now" pending="Checking…" label={`Check ${r.name} now`} variant="secondary" inline>
                  <input type="hidden" name="id" value={r.id} />
                </KForm>
              </div>
            ) : null}
          </Panel>
        ))}
      </div>
    </>
  );
}
