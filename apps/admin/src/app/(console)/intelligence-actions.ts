"use server";

import { ApiError, type AutomationRuleInput, type ForecastSettingsInput } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Sprint 8 actions: analytics refresh and data quality, automation rules,
 * executions, tasks, scheduled reports and forecasting. The API checks
 * every permission and rule again (approvals, cooldowns, readiness); these
 * only pass the form along and report the outcome.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const int = (form: FormData, key: string) => {
  const v = text(form, key);
  return v === "" ? undefined : Number.parseInt(v, 10);
};
const num = (form: FormData, key: string) => {
  const v = text(form, key);
  return v === "" ? undefined : Number(v);
};

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: error.code === "forbidden" ? "Your role does not allow this." : error.message };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Check your connection and try again; what you typed is kept." };
}

async function run(fn: (token: string) => Promise<unknown>, paths: string[], message: string): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await fn(token);
  } catch (e) {
    return failure(e);
  }
  for (const p of paths) revalidatePath(p, "layout");
  return { message };
}

// ---------------------------------------------------------------- analytics

export async function refreshAnalytics(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.intelligence.refresh(t, { from: text(form, "from") || undefined, to: text(form, "to") || undefined }),
    ["/intelligence"],
    "Refresh queued. Snapshots update within a few minutes.",
  );
}

export async function runDataQuality(): Promise<CatalogState> {
  return run((t) => api.intelligence.runDataQuality(t), ["/intelligence"], "Checks run.");
}

// ---------------------------------------------------------------- automation

export async function saveRule(_: CatalogState, form: FormData): Promise<CatalogState> {
  const params: Record<string, number> = {};
  for (const [k, v] of form.entries()) {
    if (k.startsWith("param_") && String(v).trim() !== "") params[k.slice(6)] = Number.parseInt(String(v), 10);
  }
  const input: AutomationRuleInput = {
    params,
    action: text(form, "action") as AutomationRuleInput["action"],
    priority: text(form, "priority") as AutomationRuleInput["priority"],
    cooldown_minutes: int(form, "cooldown_minutes"),
    approval_ref: text(form, "approval_ref"),
    enabled: form.get("enabled") === "on",
  };
  return run((t) => api.intelligence.saveRule(t, text(form, "id"), input), ["/automation"], input.enabled ? "Saved and enabled." : "Saved. The rule is off.");
}

export async function evaluateRule(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    const r = await api.intelligence.evaluateRule(token, text(form, "id"));
    revalidatePath("/automation", "layout");
    return { message: r.executions_created ? `${r.executions_created} new execution(s).` : "Nothing new needs attention (or it is within the cooldown)." };
  } catch (e) {
    return failure(e);
  }
}

export async function retryExecution(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.intelligence.retryExecution(t, text(form, "id")), ["/automation"], "Retry queued.");
}

export async function decideExecution(_: CatalogState, form: FormData): Promise<CatalogState> {
  const decision = text(form, "decision") === "reject" ? "reject" : "approve";
  return run(
    (t) => api.intelligence.decide(t, text(form, "id"), { decision, note: text(form, "note") }),
    ["/automation"],
    decision === "approve" ? "Approved. The owning team has been told; nothing was done automatically." : "Rejected.",
  );
}

export async function closeTask(_: CatalogState, form: FormData): Promise<CatalogState> {
  const status = text(form, "status") === "dismissed" ? "dismissed" : "done";
  return run((t) => api.intelligence.closeTask(t, text(form, "id"), { status, note: text(form, "note") }), ["/automation"], status === "done" ? "Marked done." : "Dismissed.");
}

export async function saveSchedule(_: CatalogState, form: FormData): Promise<CatalogState> {
  const recipients = text(form, "recipients")
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  return run(
    (t) => api.intelligence.saveSchedule(t, text(form, "id"), { recipients, approval_ref: text(form, "approval_ref"), enabled: form.get("enabled") === "on" }),
    ["/automation"],
    "Saved.",
  );
}

export async function runReport(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.intelligence.runReport(token, text(form, "id"))).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/automation", "layout");
  redirect(`/automation/reports/${id}`);
}

// ---------------------------------------------------------------- forecasting

export async function saveForecastSettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const input: ForecastSettingsInput = {
    min_weeks: int(form, "min_weeks"),
    max_missing_share: num(form, "max_missing_share"),
    min_improvement: num(form, "min_improvement"),
    min_interval_residuals: int(form, "min_interval_residuals"),
    horizon_weeks: int(form, "horizon_weeks"),
    approval_ref: text(form, "approval_ref"),
  };
  const season = int(form, "season_weeks");
  if (season === undefined) input.clear_season = true;
  else input.season_weeks = season;
  return run((t) => api.intelligence.saveForecastSettings(t, input), ["/forecasting"], "Criteria saved.");
}

export async function runForecast(): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.intelligence.runForecast(token)).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/forecasting", "layout");
  redirect(`/forecasting/runs/${id}`);
}
