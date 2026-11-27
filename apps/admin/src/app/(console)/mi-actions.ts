"use server";

import { ApiError, type ScenarioRequest } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Sprint 9.2 actions: Mise Intelligence and management finance. The API
 * checks every permission and rule again; these pass the form along and
 * report the outcome. Nothing here acts on orders, payments, menus or
 * suppliers: intelligence records are advisory.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const int = (form: FormData, key: string) => {
  const v = text(form, key);
  return v === "" ? undefined : Number.parseInt(v, 10);
};
/** A TTD amount typed in dollars ("1,234.50") as minor units. */
const minor = (form: FormData, key: string) => {
  const v = text(form, key).replace(/[,\s]/g, "");
  if (v === "") return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? Math.round(n * 100) : Number.NaN;
};
/** US dollars typed by the owner, as millionths. */
const usdMicros = (form: FormData, key: string) => {
  const v = text(form, key).replace(/[,\s$]/g, "");
  if (v === "") return null;
  const n = Number(v);
  return Number.isFinite(n) ? Math.round(n * 1_000_000) : Number.NaN;
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

const CC = "/intelligence";

// ---------------------------------------------------------------- AI

export async function askMise(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    const r = await api.mi.ask(token, { question: text(form, "question"), from: text(form, "from") || undefined, to: text(form, "to") || undefined });
    id = r.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath(CC, "layout");
  redirect(`/intelligence/runs/${id}`);
}

export async function reviewRun(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const status = text(form, "status") as "reviewed" | "flagged" | "unreviewed";
  return run((t) => api.mi.reviewRun(t, id, { status, note: text(form, "note") }), [CC], status === "flagged" ? "Flagged for follow-up." : "Review saved.");
}

export async function saveAISettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const input = {
    enabled: form.get("enabled") === "on",
    monthly_budget_usd_micros: usdMicros(form, "budget_usd"),
    input_price_usd_micros_per_mtok: usdMicros(form, "input_price_usd"),
    output_price_usd_micros_per_mtok: usdMicros(form, "output_price_usd"),
    price_reference: text(form, "price_reference"),
    user_daily_requests: int(form, "user_daily_requests") ?? 20,
    max_output_tokens: int(form, "max_output_tokens") ?? 1500,
    max_input_tokens: int(form, "max_input_tokens") ?? 30000,
    max_tool_calls: int(form, "max_tool_calls") ?? 6,
    max_seconds: int(form, "max_seconds") ?? 60,
    retention_days: int(form, "retention_days") ?? 180,
    approval_ref: text(form, "approval_ref"),
  };
  for (const k of ["monthly_budget_usd_micros", "input_price_usd_micros_per_mtok", "output_price_usd_micros_per_mtok"] as const) {
    if (Number.isNaN(input[k])) return { error: "Some fields need attention.", fields: [{ field: k, message: "Enter an amount in US dollars." }] };
  }
  return run((t) => api.mi.saveAISettings(t, input), [CC], "AI settings saved.");
}

// ---------------------------------------------------------------- briefings

export async function generateBriefing(_: CatalogState, form: FormData): Promise<CatalogState> {
  const cadence = text(form, "cadence") as "daily" | "weekly" | "monthly";
  const { token } = await requireAdmin();
  let created = false;
  try {
    created = (await api.mi.generateBriefing(token, cadence)).created;
  } catch (e) {
    return failure(e);
  }
  revalidatePath(CC, "layout");
  return { message: created ? "A new briefing version was stored." : "Nothing changed since the last version, so no new version was stored." };
}

export async function narrateBriefing(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.mi.narrate(t, text(form, "id")), [CC], "Summary written.");
}

export async function saveBriefingSchedule(_: CatalogState, form: FormData): Promise<CatalogState> {
  const recipients = form.getAll("recipients").map(String).filter(Boolean);
  return run(
    (t) => api.mi.setBriefingSchedule(t, text(form, "cadence"), { enabled: form.get("enabled") === "on", recipients, approval_ref: text(form, "approval_ref") }),
    [CC],
    "Schedule saved.",
  );
}

// ---------------------------------------------------------------- risks, opportunities, recommendations

export async function saveRisk(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.mi.saveRisk(t, { key: text(form, "key"), from: text(form, "from") || undefined, to: text(form, "to") || undefined }),
    [CC],
    "Saved for review under Recommendations.",
  );
}

export async function saveOpportunity(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.mi.saveOpportunity(t, { key: text(form, "key") }), [CC], "Saved for review under Recommendations.");
}

export async function setThreshold(_: CatalogState, form: FormData): Promise<CatalogState> {
  const warn = Number(text(form, "warn"));
  const critical = Number(text(form, "critical"));
  return run(
    (t) => api.mi.setThreshold(t, text(form, "key"), { enabled: form.get("enabled") === "on", warn, critical, note: text(form, "note") }),
    [CC],
    "Threshold saved.",
  );
}

export async function proposeRecommendation(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.mi.propose(t, {
        domain: text(form, "domain") as "executive" | "financial" | "customer" | "operational" | "general",
        title: text(form, "title"),
        summary: text(form, "summary"),
      }),
    [CC],
    "Proposal recorded.",
  );
}

export async function reviewRecommendation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const action = text(form, "action") || text(form, "decision");
  return run(
    (t) => api.mi.reviewRecommendation(t, text(form, "id"), action, { note: text(form, "note") || undefined, assignee: text(form, "assignee") || undefined }),
    [CC],
    "Recommendation updated.",
  );
}

// ---------------------------------------------------------------- scenarios

export async function saveScenario(_: CatalogState, form: FormData): Promise<CatalogState> {
  const raw = text(form, "request");
  let request: ScenarioRequest;
  try {
    request = JSON.parse(raw) as ScenarioRequest;
  } catch {
    return { error: "The scenario could not be read. Run it again, then save." };
  }
  const { token } = await requireAdmin();
  let id: string;
  try {
    const sc = await api.mi.createScenario(token, { ...request, name: text(form, "name"), description: text(form, "description") });
    id = sc.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath(CC, "layout");
  redirect(`/intelligence/scenarios/${id}`);
}

export async function archiveScenario(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.mi.archiveScenario(t, text(form, "id")), [CC], "Scenario archived; its versions are kept.");
}

// ---------------------------------------------------------------- meal proposals

export async function createProposal(_: CatalogState, form: FormData): Promise<CatalogState> {
  const ingredients = text(form, "ingredients")
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const [name = "", quantity = "", note = ""] = l.split("|").map((x) => x.trim());
      return { name, quantity, note };
    });
  const { token } = await requireAdmin();
  let id: string;
  try {
    const p = await api.mi.createProposal(token, {
      name: text(form, "name"),
      description: text(form, "description"),
      target_audience: text(form, "target_audience"),
      complexity: (text(form, "complexity") || "unknown") as "low" | "medium" | "high" | "unknown",
      ingredients,
    });
    id = p.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath(CC, "layout");
  redirect(`/intelligence/proposals/${id}`);
}

export async function generateProposals(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.mi.generateProposals(t, text(form, "brief")), [CC], "Draft concepts added below for review.");
}

export async function transitionProposal(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.mi.transitionProposal(t, text(form, "id"), text(form, "action"), { note: text(form, "note") || undefined, recipe_id: text(form, "recipe_id") || undefined }),
    [CC],
    "Proposal updated.",
  );
}

// ---------------------------------------------------------------- finance

const FIN = "/finance";

export async function createExpense(_: CatalogState, form: FormData): Promise<CatalogState> {
  const amount = minor(form, "amount");
  if (amount === undefined || Number.isNaN(amount)) return { error: "Some fields need attention.", fields: [{ field: "amount_minor", message: "Enter the amount in TTD." }] };
  return run(
    (t) =>
      api.finance.createExpense(t, {
        category: text(form, "category"),
        description: text(form, "description"),
        amount_minor: amount,
        kind: (text(form, "kind") || "actual") as "actual" | "estimated",
        expense_date: text(form, "expense_date"),
        payment_date: text(form, "payment_date") || null,
        reference: text(form, "reference"),
      }),
    [FIN, CC],
    "Expense recorded as a draft. Submit it for approval when it is complete.",
  );
}

export async function expenseTransition(_: CatalogState, form: FormData): Promise<CatalogState> {
  const action = text(form, "action") || text(form, "decision");
  return run((t) => api.finance.transitionExpense(t, text(form, "id"), action, { note: text(form, "note") || undefined }), [FIN, CC], "Expense updated.");
}

export async function createRecurring(_: CatalogState, form: FormData): Promise<CatalogState> {
  const amount = minor(form, "amount");
  if (amount === undefined || Number.isNaN(amount)) return { error: "Some fields need attention.", fields: [{ field: "amount_minor", message: "Enter the amount in TTD." }] };
  return run(
    (t) =>
      api.finance.createRecurring(t, {
        category: text(form, "category"),
        description: text(form, "description"),
        amount_minor: amount,
        cadence: text(form, "cadence") as "weekly" | "monthly" | "quarterly" | "yearly",
        starts_on: text(form, "starts_on"),
        ends_on: text(form, "ends_on") || null,
        reference: text(form, "reference"),
      }),
    [FIN, CC],
    "Recurring cost recorded. It counts once approved.",
  );
}

export async function recurringStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.finance.recurringStatus(t, text(form, "id"), text(form, "action"), { note: text(form, "note") || undefined }), [FIN, CC], "Recurring cost updated.");
}

export async function confirmCoverage(_: CatalogState, form: FormData): Promise<CatalogState> {
  const confirmed = text(form, "confirmed") !== "false";
  return run(
    (t) => api.finance.confirmCoverage(t, { category: text(form, "category"), month: text(form, "month"), note: text(form, "note"), confirmed }),
    [FIN, CC],
    confirmed ? "Confirmed: nothing to record for that month." : "Confirmation withdrawn.",
  );
}

export async function recordCashBalance(_: CatalogState, form: FormData): Promise<CatalogState> {
  const amount = minor(form, "amount");
  if (amount === undefined || Number.isNaN(amount)) return { error: "Some fields need attention.", fields: [{ field: "amount_minor", message: "Enter the balance in TTD." }] };
  return run(
    (t) => api.finance.recordCashBalance(t, { as_of: text(form, "as_of"), account_label: text(form, "account_label"), amount_minor: amount, reference: text(form, "reference") }),
    [FIN, CC],
    "Balance recorded.",
  );
}
