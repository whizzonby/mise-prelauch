import { ApiError } from "@mise/api-client";
import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { DataTable } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatValue, ProjectionView, Section } from "@/components/mi-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { archiveScenario } from "../../../mi-actions";

export const metadata: Metadata = { title: "Scenario" };

/** A saved scenario: its immutable versions, one shown in full, and a side-by-side comparison. */
export default async function ScenarioPage({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Promise<{ v?: string; compare?: string | string[] }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "intelligence_scenario.run")) notFound();
  const { id } = await params;
  const sp = await searchParams;
  let sc;
  try {
    sc = await api.mi.scenario(token, id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const version = sc.versions.find((v) => v.id === sp.v) ?? sc.versions[0];
  const check = version ? await api.mi.scenarioVersion(token, version.id).catch(() => null) : null;
  const ids = (Array.isArray(sp.compare) ? sp.compare : sp.compare ? [sp.compare] : []).slice(0, 4);
  const comparison = ids.length >= 2 ? await api.mi.compareScenarios(token, ids).catch(() => null) : null;
  return (
    <>
      <PageHeader
        title={sc.name}
        back={{ href: "/intelligence/command-center?tab=outlook", label: "Outlook" }}
        description={sc.description || `${humanize(sc.preset)} scenario, created ${formatDateTime(sc.created_at)}.`}
        actions={
          !sc.archived ? (
            <KForm action={archiveScenario} submit="Archive" pending="Archiving…" label="Archive scenario" variant="secondary" inline>
              <input type="hidden" name="id" value={sc.id} />
            </KForm>
          ) : undefined
        }
      />
      {sc.archived ? <p className="mb-4"><Tag tone="neutral">Archived</Tag></p> : null}
      <Section title="Versions" note="Versions never change. Tick two to four to compare them.">
        <form aria-label="Compare versions">
          <DataTable head={["", "Version", "Horizon", "Inputs", "Saved", ""]} label="Versions" minWidth="40rem">
            {sc.versions.map((v) => (
              <tr key={v.id}>
                <td className="p-3">
                  <input type="checkbox" name="compare" value={v.id} defaultChecked={ids.includes(v.id)} aria-label={`Compare version ${v.version}`} />
                </td>
                <td className="p-3">
                  <Link className="underline underline-offset-4" href={`/intelligence/scenarios/${sc.id}?v=${v.id}`}>
                    Version {v.version}
                  </Link>
                  {v.note ? <span className="type-caption block text-muted">{v.note}</span> : null}
                </td>
                <td className="p-3">{v.horizon_days} days</td>
                <td className="type-caption p-3 text-muted">{v.input_hash.slice(0, 12)}…</td>
                <td className="p-3">{formatDateTime(v.created_at)}</td>
                <td className="p-3">{v.id === version?.id ? <Tag tone="accent">Shown</Tag> : null}</td>
              </tr>
            ))}
          </DataTable>
          <button className="type-body-sm mt-3 min-h-11 rounded-md border border-foreground px-4 font-semibold">Compare selected</button>
        </form>
      </Section>
      {comparison ? (
        <Section title="Comparison" note={`${comparison.statement} ${comparison.note}`}>
          <DataTable head={["", ...comparison.columns.map((c) => `${c.scenario} v${c.version} (${humanize(c.preset)})`)]} label="Comparison" minWidth="40rem">
            {(
              [
                ["Sales", (c: (typeof comparison.columns)[number]) => c.totals?.sales_minor, "money_minor"],
                ["Contribution", (c: (typeof comparison.columns)[number]) => c.totals?.contribution_minor, "money_minor"],
                ["Operating result", (c: (typeof comparison.columns)[number]) => c.totals?.operating_result_minor, "money_minor"],
                ["End subscriptions", (c: (typeof comparison.columns)[number]) => c.totals?.end_active_subscriptions, "count"],
                ["Break-even subscriptions", (c: (typeof comparison.columns)[number]) => c.break_even?.subscriptions, "count"],
                ["Peak cash need", (c: (typeof comparison.columns)[number]) => c.cash.peak_need_minor, "money_minor"],
              ] as const
            ).map(([label, get, unit]) => (
              <tr key={label}>
                <th scope="row" className="p-3 text-left font-semibold">
                  {label}
                </th>
                {comparison.columns.map((c) => (
                  <td key={c.version_id} className="type-figures p-3">
                    {c.complete ? formatValue(get(c) ?? null, unit) : "Not calculated"}
                  </td>
                ))}
              </tr>
            ))}
          </DataTable>
        </Section>
      ) : null}
      {version ? (
        <Section
          title={`Version ${version.version}`}
          note={check ? (check.reproduced ? "Recomputed from its stored inputs: identical to the stored result." : "This version was made by an earlier engine version.") : undefined}
        >
          <ProjectionView p={version.projection} />
        </Section>
      ) : null}
    </>
  );
}
