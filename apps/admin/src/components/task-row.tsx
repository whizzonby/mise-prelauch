import type { KitchenTask } from "@mise/api-client";
import { cx } from "@mise/ui";
import Link from "next/link";

import { updateTask } from "@/app/(console)/kitchen-actions";
import { StatusTag } from "@/components/admin-ui";
import { TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, humanize } from "@/lib/format";

/**
 * One kitchen task with the next step as a large button. Taking a task
 * assigns it to you; blocking it needs a short note of what is wrong, and
 * alerts the production manager.
 */
export function TaskRow({ task: t, back, canWork, showBatch = false }: { task: KitchenTask; back: string; canWork: boolean; showBatch?: boolean }) {
  const overdue = t.due_at && t.status !== "done" && new Date(t.due_at) < new Date();
  const hidden = (status: string) => (
    <>
      <input type="hidden" name="id" value={t.id} />
      <input type="hidden" name="back" value={back} />
      <input type="hidden" name="status" value={status} />
    </>
  );
  return (
    <li className={cx("border bg-surface p-4", t.status === "blocked" ? "border-error" : "border-border")}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-[1.25rem] font-semibold leading-snug">{t.title}</p>
          {showBatch ? (
            <p className="type-body-sm">
              <Link href={`/production/batches/${t.batch_id}`} className="underline underline-offset-4">
                {t.batch_code} · {t.meal_name}
              </Link>{" "}
              · {t.planned_kits} kits
            </p>
          ) : null}
          <p className="type-caption text-muted">
            {humanize(t.kind)}
            {t.priority === "high" ? " · high priority" : ""}
            {t.due_at ? ` · due ${formatDateTime(t.due_at)}` : ""}
            {t.assignee_name ? ` · ${t.assignee_name}` : " · not taken"}
          </p>
        </div>
        <div className="flex gap-2">
          {overdue ? <StatusTag status="failed" label="Overdue" /> : null}
          <StatusTag status={t.status} />
        </div>
      </div>
      {t.instructions ? <p className="type-body-sm mt-2">{t.instructions}</p> : null}
      {t.exception ? <p className="type-body-sm mt-2 border-l-2 border-error pl-2">Exception: {t.exception}</p> : null}
      {canWork && t.batch_status === "released" ? <p className="type-caption mt-2 text-muted">Start the batch to work its tasks.</p> : null}
      {canWork && t.batch_status !== "released" && t.status !== "done" && t.status !== "cancelled" ? (
        <div className="mt-3">
          {t.status === "todo" || t.status === "blocked" ? (
            <KForm action={updateTask} submit={t.status === "blocked" ? "Resume" : "Take and start"} pending="…" label={`Start ${t.title}`} size="lg" inline>
              {hidden("in_progress")}
              {!t.assigned_to ? <input type="hidden" name="assigned_to" value="me" /> : null}
            </KForm>
          ) : (
            <div className="grid gap-3">
              <KForm action={updateTask} submit="Done" pending="…" label={`Finish ${t.title}`} size="lg" inline>
                {hidden("done")}
              </KForm>
              <details>
                <summary className="type-body-sm cursor-pointer font-semibold underline underline-offset-4">Something is wrong</summary>
                <div className="mt-2">
                  <KForm action={updateTask} submit="Mark blocked" pending="…" label={`Block ${t.title}`} variant="secondary" inline>
                    {hidden("blocked")}
                    <TextInput name="exception" placeholder="What is stopping it?" aria-label="What is stopping it?" required maxLength={1000} className="w-72" />
                  </KForm>
                </div>
              </details>
            </div>
          )}
        </div>
      ) : null}
    </li>
  );
}
