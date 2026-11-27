import { ApiError } from "@mise/api-client";
import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { Bars, Evidence, Section } from "@/components/mi-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { reviewRun } from "../../../mi-actions";

export const metadata: Metadata = { title: "Intelligence run" };

const usd = (m: number | null) => (m === null ? "Not estimated" : `US$${(m / 1_000_000).toLocaleString("en-GB", { maximumFractionDigits: 6 })}`);

/** One stored run: the answer, the claims and the evidence they cite, the validation and the review. */
export default async function RunPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "intelligence.read")) notFound();
  const { id } = await params;
  let r;
  try {
    r = await api.mi.run(token, id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
  const out = r.output;
  const evidence = new Map(r.evidence.map((e) => [e.ref, e]));
  return (
    <>
      <PageHeader
        title={r.question || humanize(r.kind)}
        back={{ href: "/intelligence/command-center?tab=history", label: "History" }}
        description={`${humanize(r.kind)} · ${formatDateTime(r.created_at)} · ${r.provider || "no provider"} ${r.model} · prompt ${r.prompt_version}${r.from ? ` · period ${r.from} to ${r.to}` : ""}`}
      />
      <p className="mb-4">
        <Tag tone={r.status === "completed" ? "success" : r.status === "partial" ? "warning" : "error"}>{humanize(r.status)}</Tag>{" "}
        <Tag tone={r.review_status === "flagged" ? "error" : r.review_status === "reviewed" ? "success" : "neutral"}>{humanize(r.review_status)}</Tag>
      </p>
      {r.error ? <Notice tone="error" title="No answer" items={[r.error]} /> : null}
      {out ? (
        <div className="grid gap-5">
          <Panel title="Answer" note="Every number was checked against the tool results cited below. Anything unsupported was removed and marked [unverified].">
            <p className="type-body whitespace-pre-wrap">{out.answer}</p>
          </Panel>
          {out.claims.length ? (
            <Section title="Supporting figures">
              <DataTable head={["Claim", "Value", "Source"]} label="Claims" minWidth="40rem">
                {out.claims.map((c, i) => {
                  const ev = evidence.get(c.ref);
                  return (
                    <tr key={i}>
                      <td className="p-3">{c.text}</td>
                      <td className="type-figures p-3">{c.value ?? "Not available"}</td>
                      <td className="type-caption p-3 text-muted">
                        {c.ref} · {ev?.tool} · {c.fact}
                        {ev?.from ? ` · ${ev.from} to ${ev.to}` : ""}
                      </td>
                    </tr>
                  );
                })}
              </DataTable>
            </Section>
          ) : null}
          {out.tables.map((t, ti) => (
            <Section key={ti} title={t.title} note={`From ${t.ref} (${evidence.get(t.ref)?.tool ?? "unknown"}).`}>
              <DataTable head={t.columns} label={t.title} minWidth="32rem">
                {t.rows.map((row, ri) => (
                  <tr key={ri}>
                    {row.map((cell, ci) => (
                      <td key={ci} className="p-3">
                        {cell}
                      </td>
                    ))}
                  </tr>
                ))}
              </DataTable>
              {out.charts
                .filter((c) => c.table === ti)
                .map((c, ci) => (
                  <div key={ci} className="mt-4">
                    <Bars label={c.title} unit="number" points={t.rows.map((row) => ({ x: row[c.label_column] ?? "", y: Number(String(row[c.value_column] ?? "").replace(/[^0-9.-]/g, "")) || 0 }))} />
                  </div>
                ))}
            </Section>
          ))}
          <div className="grid gap-5 lg:grid-cols-2">
            {out.assumptions.length ? (
              <Panel title="Assumptions">
                <ul className="type-body-sm list-disc pl-5">
                  {out.assumptions.map((a) => (
                    <li key={a}>{a}</li>
                  ))}
                </ul>
              </Panel>
            ) : null}
            <Panel title="Limitations">
              <ul className="type-body-sm list-disc pl-5">
                {out.limitations.map((a) => (
                  <li key={a}>{a}</li>
                ))}
              </ul>
            </Panel>
          </div>
          {out.follow_ups.length ? (
            <Panel title="You could also ask">
              <ul className="type-body-sm list-disc pl-5">
                {out.follow_ups.map((a) => (
                  <li key={a}>{a}</li>
                ))}
              </ul>
            </Panel>
          ) : null}
        </div>
      ) : null}
      <Section title="Evidence and checks">
        <Evidence title="Tools called">
          {r.tool_calls.length ? (
            r.tool_calls.map((c, i) => (
              <p key={i}>
                {c.ref || "—"} {c.tool} {JSON.stringify(c.args)} {c.ok ? "✓" : `refused: ${c.error}`}
              </p>
            ))
          ) : (
            <p>None.</p>
          )}
        </Evidence>
        <Evidence title="Evidence references">
          {r.evidence.map((e) => (
            <p key={e.ref}>
              {e.ref}: {e.tool} ({e.domain}){e.from ? `, ${e.from} to ${e.to}` : ""}, as of {formatDateTime(e.freshness)}. Sources: {e.sources.join(", ") || "—"}.
            </p>
          ))}
        </Evidence>
        <Evidence title="Numeric validation">
          <p>
            {r.validation.claims_checked} claims and {r.validation.numbers_checked} numbers checked; {r.validation.unsupported.length} unsupported
            {r.validation.regenerated ? ", answer regenerated once" : ""}; {r.validation.removed} removed.
          </p>
          {r.validation.unsupported.map((u, i) => (
            <p key={i}>
              {u.where}: {u.value} — {u.reason}
            </p>
          ))}
        </Evidence>
        <p className="type-caption mt-3 text-muted">
          {r.model_calls} model calls · {r.input_tokens + r.output_tokens} tokens · estimated {usd(r.cost_usd_micros)} · {r.duration_ms} ms
        </p>
      </Section>
      <Section title="Review" note="Mark the answer reviewed, or flag it with what is wrong.">
        <KForm action={reviewRun} submit="Save review" pending="Saving…" label="Review this run" inline>
          <input type="hidden" name="id" value={r.id} />
          <Labelled label="Status">
            <SelectInput name="status" defaultValue={r.review_status === "unreviewed" ? "reviewed" : r.review_status}>
              <option value="reviewed">Reviewed</option>
              <option value="flagged">Flagged</option>
              <option value="unreviewed">Unreviewed</option>
            </SelectInput>
          </Labelled>
          <Labelled label="Note">
            <TextInput name="note" maxLength={1000} defaultValue={r.review_note} />
          </Labelled>
        </KForm>
      </Section>
    </>
  );
}
