import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { CountTile } from "@/components/delivery-ui";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime, number } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "System status" };

/** Is the platform working right now: worker, queues, event consumers, payment webhooks, email, database. */
export default async function SystemPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "launch.read")) notFound();
  const st = await api.launch.system(token);
  const problems = [
    ...(st.worker_healthy ? [] : ["No worker has reported in the last 90 seconds: emails, payments, renewals and events are not being processed."]),
    ...(st.failed_jobs_24h ? [`${st.failed_jobs_24h} job(s) failed in the last 24 hours.`] : []),
    ...(st.email_failures_24h ? [`${st.email_failures_24h} email(s) failed in the last 24 hours.`] : []),
    ...(st.payment_webhook_failures ? [`${st.payment_webhook_failures} payment webhook(s) failed.`] : []),
  ];

  return (
    <>
      <PageHeader
        title="System status"
        description={`API ${st.api.version}${st.api.commit ? ` (${st.api.commit.slice(0, 12)})` : ""} · ${st.environment} · checked ${formatDateTime(st.checked_at)}`}
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {problems.length ? <Notice tone="error" title="Needs attention" items={problems} /> : <Notice tone="info" title="No problems detected." items={["Worker alive, no failed jobs or webhooks in the last 24 hours."]} />}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <CountTile label="Failed jobs (24 h)" value={st.failed_jobs_24h} tone={st.failed_jobs_24h ? "bad" : undefined} />
          <CountTile label="Failed emails (24 h)" value={st.email_failures_24h} tone={st.email_failures_24h ? "bad" : undefined} />
          <CountTile label="Webhooks failed" value={st.payment_webhook_failures} tone={st.payment_webhook_failures ? "bad" : undefined} />
          <CountTile label="Webhooks waiting" value={st.payment_webhooks_pending} tone={st.payment_webhooks_pending ? "warn" : undefined} />
        </div>
        <Panel title="Workers" note={`Database round trip ${st.database_ms.toFixed(1)} ms.`}>
          {st.workers.length ? (
            <DataTable head={["Worker", "Version", "Started", "Last heartbeat", "State"]} minWidth="36rem" label="Workers">
              {st.workers.map((w) => (
                <tr key={w.worker}>
                  <td className="p-3 font-semibold">{w.worker}</td>
                  <td className="p-3">{w.version}</td>
                  <td className="p-3">{formatDateTime(w.started_at)}</td>
                  <td className="p-3">{formatDateTime(w.beat_at)}</td>
                  <td className="p-3">
                    <StatusTag status={w.stale ? "missing" : "active"} label={w.stale ? "Stopped" : "Running"} />
                  </td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <Empty>No worker has ever reported. Start the worker process.</Empty>
          )}
        </Panel>
        <Panel title="Event consumers" note={`Latest event #${number.format(st.latest_event_id)}. Lag counts every later event, including types a consumer ignores, so it is an upper bound.`}>
          <DataTable head={["Consumer", "Read up to", "Lag", "Updated"]} minWidth="32rem" label="Event consumers">
            {st.consumers.map((c) => (
              <tr key={c.consumer}>
                <td className="p-3 font-semibold">{c.consumer}</td>
                <td className="p-3">#{number.format(c.last_event_id)}</td>
                <td className="p-3">{number.format(c.lag)}</td>
                <td className="p-3">{formatDateTime(c.updated_at)}</td>
              </tr>
            ))}
          </DataTable>
        </Panel>
        <Panel title="Job queues">
          {st.jobs.length ? (
            <DataTable head={["Kind", "Waiting", "Oldest waiting", "Failed (24 h)"]} minWidth="32rem" label="Job queues">
              {st.jobs.map((j) => (
                <tr key={j.kind}>
                  <td className="p-3 font-semibold">{j.kind}</td>
                  <td className="p-3">{j.waiting}</td>
                  <td className="p-3">{j.oldest_waiting ? formatDateTime(j.oldest_waiting) : "—"}</td>
                  <td className="p-3">{j.failed_24h}</td>
                </tr>
              ))}
            </DataTable>
          ) : (
            <Empty>No queued, running or failed jobs.</Empty>
          )}
        </Panel>
      </div>
    </>
  );
}
