import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { FilterBar, PageHeader } from "@/components/admin-ui";
import { Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { DASHBOARDS, DashboardView, PathTabs } from "@/components/intelligence-ui";
import { Notice } from "@/components/kitchen-ui";
import { validDay } from "@/components/supply-ui";
import { api, can, requireAdmin } from "@/lib/session";

type Params = { from?: string; to?: string; plan_id?: string; meal_id?: string; zone_id?: string; supplier_id?: string };

export async function generateMetadata({ params }: { params: Promise<{ kind: string }> }): Promise<Metadata> {
  const { kind } = await params;
  return { title: `${DASHBOARDS.find((d) => d.kind === kind)?.label ?? "Insights"} insights` };
}

const uuid = (s?: string) => (s && /^[0-9a-f-]{36}$/i.test(s) ? s : undefined);

/** One dashboard. Every figure comes from the metric registry, with its definition one click away. */
export default async function DashboardPage({ params, searchParams }: { params: Promise<{ kind: string }>; searchParams: Promise<Params> }) {
  const { token, admin } = await requireAdmin();
  const { kind } = await params;
  const spec = DASHBOARDS.find((d) => d.kind === kind);
  if (!spec || !can(admin, spec.perm)) notFound();
  const sp = await searchParams;
  const query = {
    from: validDay(sp.from),
    to: validDay(sp.to),
    plan_id: uuid(sp.plan_id),
    meal_id: uuid(sp.meal_id),
    zone_id: uuid(sp.zone_id),
    supplier_id: uuid(sp.supplier_id),
  };
  let d;
  try {
    d = await api.intelligence.dashboard(token, kind, query);
  } catch (e) {
    if (e instanceof ApiError && e.status === 422) {
      return (
        <>
          <PageHeader title="Insights" />
          <Notice tone="error" title="That range cannot be shown" items={[e.message]} />
        </>
      );
    }
    throw e;
  }
  const filters = new Set(d.filters);
  const [plans, meals, zones, suppliers] = await Promise.all([
    filters.has("plan") && can(admin, "plans.manage") ? api.admin.plans(token).then((r) => r.items).catch(() => []) : Promise.resolve([]),
    filters.has("meal") && can(admin, "catalog.read") ? api.admin.catalogMeals(token, {}).then((r) => r.items).catch(() => []) : Promise.resolve([]),
    filters.has("zone") && can(admin, "delivery_zones.read") ? api.delivery.zones(token).then((r) => r.items).catch(() => []) : Promise.resolve([]),
    filters.has("supplier") && can(admin, "suppliers.read") ? api.supply.suppliers(token, { page_size: 100 }).then((r) => r.items).catch(() => []) : Promise.resolve([]),
  ]);
  const options = (name: string, label: string, all: string, items: { id: string; name: string }[], value?: string) =>
    items.length ? (
      <Labelled label={label}>
        <SelectInput name={name} defaultValue={value ?? ""}>
          <option value="">{all}</option>
          {items.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </SelectInput>
      </Labelled>
    ) : null;
  const qs = new URLSearchParams(Object.entries(query).filter(([, v]) => v) as [string, string][]).toString();
  return (
    <>
      <PageHeader
        title={d.title}
        description="Figures from authoritative records, each defined once in the metric catalogue. Money is TTD unless stated."
        actions={
          can(admin, "analytics.export") ? (
            <a
              className="type-body-sm inline-flex min-h-11 items-center rounded-md border border-foreground px-4 font-semibold hover:bg-foreground hover:text-surface"
              href={`/intelligence/${kind}/export${qs ? `?${qs}` : ""}`}
            >
              Download CSV
            </a>
          ) : undefined
        }
      />
      <PathTabs tabs={DASHBOARDS.filter((x) => can(admin, x.perm)).map((x) => ({ href: `/intelligence/${x.kind}`, label: x.label }))} current={`/intelligence/${kind}`} />
      <FilterBar>
        <Labelled label="From">
          <TextInput type="date" name="from" defaultValue={d.from} />
        </Labelled>
        <Labelled label="To">
          <TextInput type="date" name="to" defaultValue={d.to} />
        </Labelled>
        {options("plan_id", "Plan", "All plans", plans, query.plan_id)}
        {options("meal_id", "Meal", "All meals", meals, query.meal_id)}
        {options("zone_id", "Zone", "All zones", zones, query.zone_id)}
        {options("supplier_id", "Supplier", "All suppliers", suppliers, query.supplier_id)}
      </FilterBar>
      <DashboardView d={d} />
      <p className="type-caption mt-8 text-muted">
        <Link className="underline" href="/intelligence/metrics">
          Metric catalogue
        </Link>{" "}
        ·{" "}
        <Link className="underline" href="/intelligence/quality">
          Data quality and freshness
        </Link>
      </p>
    </>
  );
}
