import { ApiError } from "@mise/api-client";
import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, Timeline } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { Evidence, Section } from "@/components/mi-ui";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { transitionProposal } from "../../../mi-actions";

export const metadata: Metadata = { title: "Meal proposal" };

const NEXT: Record<string, string[]> = {
  proposed: ["review", "reject"],
  in_review: ["approve", "reject"],
  approved_for_development: ["link_recipe", "reject"],
  in_development: ["close"],
};
const ACTION_LABEL: Record<string, string> = {
  review: "Start management review",
  approve: "Approve for chef development",
  reject: "Reject",
  link_recipe: "Link the chef's recipe",
  close: "Close",
};

type Table = { title: string; columns: string[]; rows: string[][] };

/** A draft meal concept, its evidence and its path through the culinary workflow. */
export default async function ProposalPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "intelligence_operational.read")) notFound();
  const { id } = await params;
  let p;
  try {
    p = await api.mi.proposal(token, id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const ev = p.evidence as Record<string, unknown>;
  const tables = ["demand_by_category", "top_meals", "approved_supplier_products", "availability_issues", "category_preferences"]
    .map((k) => ev[k] as Table | undefined)
    .filter((t): t is Table => !!t && Array.isArray(t.rows));
  const actions = can(admin, "intelligence_recommendation.review") ? (NEXT[p.status] ?? []) : [];
  return (
    <>
      <PageHeader
        title={p.name}
        back={{ href: "/intelligence/command-center?tab=meals", label: "Meals" }}
        description={p.description || undefined}
      />
      <p className="mb-4">
        {p.origin === "ai" ? <Tag tone="accent">AI draft</Tag> : <Tag tone="neutral">Written by a manager</Tag>} <Tag tone="neutral">{humanize(p.status)}</Tag>{" "}
        <Tag tone="neutral">Complexity: {humanize(p.complexity)}</Tag>
      </p>
      <Notice tone="info" title="A draft, not a meal" items={p.limitations} />
      <div className="mt-6 grid gap-5 lg:grid-cols-2">
        <Panel title="Proposed ingredients" note="Matched to the ingredient catalogue by name. Quantities are suggestions for the chef.">
          {p.ingredients.length ? (
            <ul className="type-body-sm grid gap-1">
              {p.ingredients.map((i) => (
                <li key={i.name}>
                  <span className="font-semibold">{i.name}</span> {i.quantity ? `· ${i.quantity}` : ""} {i.note ? <span className="text-muted">· {i.note}</span> : null}{" "}
                  {i.ingredient_id ? <Tag tone="success">In catalogue</Tag> : <Tag tone="warning">Not in catalogue</Tag>}
                </li>
              ))}
            </ul>
          ) : (
            <p className="type-body-sm text-muted">No ingredients listed.</p>
          )}
          {p.availability_notes.length ? (
            <ul className="type-caption mt-3 list-disc pl-5 text-muted">
              {p.availability_notes.map((n) => (
                <li key={n}>{n}</li>
              ))}
            </ul>
          ) : null}
        </Panel>
        <Panel title="Target audience and cost">
          <p className="type-body-sm">{p.target_audience || "Not stated."}</p>
          <p className="type-body-sm mt-3 text-muted">{p.cost_note}</p>
          {p.recipe_id ? (
            <p className="type-body-sm mt-3">
              Recipe: <a className="underline" href={`/recipes/${p.recipe_id}`}>{p.recipe_id.slice(0, 8)}</a> ({humanize(p.recipe_status ?? "draft")}, in the recipe workflow)
            </p>
          ) : null}
        </Panel>
      </div>
      <Section title="Required approvals" note="The proposal ends at chef development; every later step happens in the existing culinary workflow.">
        <ol className="type-body-sm list-decimal pl-5">
          {p.required_approvals.map((a) => (
            <li key={a}>{a}</li>
          ))}
        </ol>
      </Section>
      {actions.length ? (
        <Section title="Next step">
          <KForm action={transitionProposal} submit="Apply" pending="Saving…" label="Proposal step">
            <input type="hidden" name="id" value={p.id} />
            <div className="grid gap-4 sm:grid-cols-3">
              <Labelled label="Step">
                <SelectInput name="action" defaultValue={actions[0]}>
                  {actions.map((a) => (
                    <option key={a} value={a}>
                      {ACTION_LABEL[a] ?? humanize(a)}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Decision note" hint="Needed to approve or reject">
                <TextInput name="note" maxLength={1000} />
              </Labelled>
              {p.status === "approved_for_development" ? (
                <Labelled label="Recipe id" hint="Of a recipe the chef created in Culinary">
                  <TextInput name="recipe_id" />
                </Labelled>
              ) : null}
            </div>
          </KForm>
        </Section>
      ) : null}
      <Section title="Evidence" note="The deterministic evidence pack captured when the proposal was made.">
        {tables.map((t) => (
          <div key={t.title} className="mb-5">
            <h3 className="type-body-sm mb-2 font-semibold">{t.title}</h3>
            {t.rows.length ? (
              <DataTable head={t.columns} label={t.title} minWidth="32rem">
                {t.rows.slice(0, 20).map((row, i) => (
                  <tr key={i}>
                    {row.map((c, j) => (
                      <td key={j} className="p-2">
                        {c}
                      </td>
                    ))}
                  </tr>
                ))}
              </DataTable>
            ) : (
              <p className="type-caption text-muted">Nothing recorded.</p>
            )}
          </div>
        ))}
        <Evidence title="Notes">
          {((ev.notes as string[] | undefined) ?? []).map((n) => (
            <p key={n}>{n}</p>
          ))}
          <p>
            Period {String(ev.from ?? "")} to {String(ev.to ?? "")}.
          </p>
        </Evidence>
      </Section>
      <Section title="History">
        <Timeline items={p.events.map((e) => ({ at: e.created_at, title: humanize(e.action), detail: e.note || `${humanize(e.from_status || "new")} → ${humanize(e.to_status)}` }))} />
      </Section>
    </>
  );
}
