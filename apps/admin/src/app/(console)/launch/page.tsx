import type { LaunchGate } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable, Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { CountTile } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { Notice } from "@/components/kitchen-ui";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { signOffGate } from "./actions";

export const metadata: Metadata = { title: "Launch readiness" };

const CATEGORIES: { key: LaunchGate["category"]; label: string }[] = [
  { key: "commercial", label: "Commercial" },
  { key: "business", label: "Menu and food" },
  { key: "operations", label: "Operations" },
  { key: "content", label: "Content and legal" },
  { key: "infrastructure", label: "Infrastructure" },
  { key: "security", label: "Security" },
  { key: "engineering", label: "Engineering" },
];

const STATUS_LABEL: Record<LaunchGate["status"], string> = {
  verified: "Verified",
  configured: "Configured, not signed off",
  blocked: "Blocked",
  not_applicable: "Not applicable",
};

/**
 * Launch control centre. Each gate shows what the system can see for itself
 * and what a person signed off, separately. The page never says "ready"
 * while a critical gate is open: the API decides, and a sign-off cannot
 * override evidence that contradicts it.
 */
export default async function LaunchPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "launch.read")) notFound();
  const ov = await api.launch.gates(token);
  const counts = { verified: 0, configured: 0, blocked: 0, not_applicable: 0 };
  for (const g of ov.gates) counts[g.status]++;
  const signer = can(admin, "launch.signoff");

  return (
    <>
      <PageHeader
        title="Launch readiness"
        description="Engineering, commercial, operational and content gates for go-live. Configured is not the same as approved: each critical gate needs evidence and a recorded sign-off."
      />
      <div className="grid gap-6 [&>*]:min-w-0">
        {ov.ready ? (
          <Notice tone="info" title="Every critical gate is verified or signed off as not applicable." items={["Public launch still needs the owner's explicit go-live decision."]} />
        ) : (
          <Notice tone="error" title={`Not ready to launch: ${ov.critical_blockers} critical gate(s) open.`} items={[`Every critical gate needs evidence and a recorded sign-off. Checked ${formatDateTime(ov.checked_at)}.`]} />
        )}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <CountTile label="Verified" value={counts.verified} />
          <CountTile label="Configured, awaiting sign-off" value={counts.configured} tone={counts.configured ? "warn" : undefined} />
          <CountTile label="Blocked" value={counts.blocked} tone={counts.blocked ? "bad" : undefined} />
          <CountTile label="Not applicable" value={counts.not_applicable} />
        </div>

        {CATEGORIES.map((cat) => {
          const gates = ov.gates.filter((g) => g.category === cat.key);
          if (!gates.length) return null;
          return (
            <Panel key={cat.key} title={cat.label}>
              <DataTable head={["Gate", "Status", "What the system sees", "Sign-off"]} minWidth="52rem" label={`${cat.label} gates`}>
                {gates.map((g) => (
                  <tr key={g.key} className="align-top">
                    <td className="p-3">
                      <span className="font-semibold">{g.name}</span>
                      <span className="type-caption block text-muted">
                        {g.owner}
                        {g.critical ? " · critical" : ""}
                      </span>
                    </td>
                    <td className="p-3">
                      <StatusTag status={g.status} label={STATUS_LABEL[g.status]} />
                    </td>
                    <td className="type-body-sm p-3">{g.evidence ?? <span className="text-muted">Not observable by the system: needs a sign-off with a reference.</span>}</td>
                    <td className="type-body-sm p-3">
                      {g.signoff ? (
                        <>
                          {g.signoff.decision === "approved" ? "Approved" : "Not applicable"} by {g.signoff.by}, {formatDateTime(g.signoff.at)}
                          <span className="block text-muted">Ref: {g.signoff.reference}</span>
                          {g.signoff.note ? <span className="block text-muted">{g.signoff.note}</span> : null}
                        </>
                      ) : (
                        <span className="text-muted">None</span>
                      )}
                    </td>
                  </tr>
                ))}
              </DataTable>
            </Panel>
          );
        })}

        {signer ? (
          <Panel title="Record a sign-off" note="Approving is refused while the gate's own evidence does not hold. Every decision is kept; revoking adds a new entry.">
            <KForm action={signOffGate} submit="Record sign-off" pending="Recording…" label="Record a launch gate sign-off">
              <div className="grid gap-4 md:grid-cols-3">
                <Labelled label="Gate">
                  <SelectInput name="gate" required defaultValue="">
                    <option value="" disabled>
                      Choose a gate
                    </option>
                    {ov.gates.map((g) => (
                      <option key={g.key} value={g.key}>
                        {g.name}
                      </option>
                    ))}
                  </SelectInput>
                </Labelled>
                <Labelled label="Decision">
                  <SelectInput name="decision" required defaultValue="approved">
                    <option value="approved">Approved</option>
                    <option value="not_applicable">Not applicable</option>
                    <option value="revoked">Revoke the current sign-off</option>
                  </SelectInput>
                </Labelled>
                <Labelled label="Reference" hint="Where the evidence or approval is recorded.">
                  <TextInput name="reference" required minLength={3} maxLength={300} />
                </Labelled>
              </div>
              <Labelled label="Note" hint="Required for not applicable: say why." className="mt-4">
                <TextArea name="note" rows={3} maxLength={2000} />
              </Labelled>
            </KForm>
          </Panel>
        ) : null}
      </div>
    </>
  );
}
