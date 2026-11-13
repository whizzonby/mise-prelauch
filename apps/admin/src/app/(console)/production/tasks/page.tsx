import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, Tabs } from "@/components/admin-ui";
import { TaskRow } from "@/components/task-row";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Kitchen tasks" };

type Search = { tab?: string; status?: string };

/**
 * The kitchen's task board, for tablets and phones: large text, one big
 * button per task, blocked tasks first.
 */
export default async function TasksPage({ searchParams }: { searchParams: Promise<Search> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "production.read")) notFound();
  const sp = await searchParams;
  const tab = sp.tab ?? "today";
  const query = tab === "mine" ? { mine: true } : tab === "all" ? {} : { date: "today" };
  const [tasks, today] = await Promise.all([
    api.kitchen.tasks(token, { ...query, status: sp.status }),
    api.kitchen.batches(token, { date: "today", open: true }),
  ]);
  const canWork = can(admin, "production.execute");
  const toStart = today.items.filter((b) => b.status === "released");
  return (
    <>
      <PageHeader title="Kitchen tasks" description="Take a task, then press Done. If something stops you, say what — the production manager is alerted." />
      {toStart.length && canWork ? (
        <p className="type-body mb-4 border-l-4 border-accent bg-surface p-4">
          {toStart.length === 1 ? "1 batch is" : `${toStart.length} batches are`} waiting to be started today:{" "}
          {toStart.map((b, i) => (
            <span key={b.id}>
              {i ? ", " : ""}
              <Link href={`/production/batches/${b.id}`} className="font-semibold underline underline-offset-4">
                {b.meal_name}
              </Link>
            </span>
          ))}
          .
        </p>
      ) : null}
      <Tabs
        base="/production/tasks"
        current={tab}
        tabs={[
          { key: "today", label: "Today" },
          { key: "mine", label: "Mine" },
          { key: "all", label: "All open" },
        ]}
      />
      <div className="mt-4">
        {tasks.items.length ? (
          <ul className="grid gap-3 lg:grid-cols-2">
            {tasks.items.map((t) => (
              <TaskRow key={t.id} task={t} back="/production/tasks" canWork={canWork} showBatch />
            ))}
          </ul>
        ) : (
          <EmptyState>{tab === "mine" ? "You have no open tasks." : "No open tasks."}</EmptyState>
        )}
      </div>
    </>
  );
}
