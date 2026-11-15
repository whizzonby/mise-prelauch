import type { Metadata } from "next";

import { PageHeader } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { humanize } from "@/lib/format";
import { api, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Metric catalogue" };

/** Every metric this person can see, defined once: dashboards, reports and exports all use these definitions. */
export default async function MetricCataloguePage() {
  const { token } = await requireAdmin();
  const { items } = await api.intelligence.metrics(token);
  const domains = [...new Set(items.map((m) => m.domain))];
  return (
    <>
      <PageHeader
        title="Metric catalogue"
        back={{ href: "/intelligence", label: "Insights" }}
        description="What each figure counts, where it comes from and what it cannot tell you. Only metrics your role may see are listed."
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {domains.map((d) => (
          <Panel key={d} title={humanize(d)}>
            <dl className="grid gap-5">
              {items
                .filter((m) => m.domain === d)
                .map((m) => (
                  <div key={m.id} id={m.id} className="scroll-mt-24 border-t border-border pt-4 first:border-0 first:pt-0">
                    <dt className="font-semibold">
                      {m.name} <code className="type-caption ml-1 text-muted">{m.id}</code>
                    </dt>
                    <dd className="type-body-sm mt-1 grid gap-1">
                      <p>{m.definition}</p>
                      <p className="text-muted">Calculation: {m.calculation}</p>
                      <p className="text-muted">
                        Sources: {m.source_fields.join(", ")} · Period: {m.reporting_period} · Updated: {m.update_frequency}
                        {m.filters.length ? ` · Filters: ${m.filters.join(", ")}` : ""}
                      </p>
                      <p className="text-muted">Missing data: {m.missing_data}</p>
                      {m.currency ? <p className="text-muted">Currency: {m.currency}</p> : null}
                      <p className="text-muted">Limitations: {m.limitations}</p>
                      <p className="text-muted">Owner: {m.owner}</p>
                    </dd>
                  </div>
                ))}
            </dl>
          </Panel>
        ))}
      </div>
    </>
  );
}
