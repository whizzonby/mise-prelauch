import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { EmptyState, FilterBar, PageHeader } from "@/components/admin-ui";
import { Labelled, SelectInput, TextInput } from "@/components/commerce-ui";
import { BatchCard } from "@/components/kitchen-ui";
import { humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Batches" };

const STATUSES = ["planned", "released", "in_progress", "awaiting_quality_check", "quality_hold", "completed", "cancelled"];
type Search = { status?: string; date?: string; open?: string; plan_id?: string };

export default async function BatchesPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const sp = await searchParams;
  const list = await api.kitchen.batches(token, { status: sp.status, date: sp.date, plan_id: sp.plan_id, open: sp.open === "true" });
  return (
    <>
      <PageHeader title="Batches" description="One batch per meal per plan, made from one exact recipe version. A batch completes only after its output is reconciled, its quality checks pass and it is packed." />
      <FilterBar>
        <Labelled label="Status">
          <SelectInput name="status" defaultValue={sp.status ?? ""}>
            <option value="">Any</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {humanize(s)}
              </option>
            ))}
          </SelectInput>
        </Labelled>
        <Labelled label="Production date">
          <TextInput type="date" name="date" defaultValue={sp.date && sp.date !== "today" ? sp.date : ""} />
        </Labelled>
        <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
          <input type="checkbox" name="open" value="true" defaultChecked={sp.open === "true"} className="size-5" /> Not finished only
        </label>
      </FilterBar>
      {list.items.length ? (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {list.items.map((b) => (
            <BatchCard key={b.id} batch={b} />
          ))}
        </div>
      ) : (
        <EmptyState>No batches match.</EmptyState>
      )}
    </>
  );
}
