import type { PaymentProviderStatus } from "@mise/api-client";
import { Tag } from "@mise/ui";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Labelled, Panel, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";
import { CountTile } from "@/components/delivery-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { setMerchantStatus, setProviderEnabled, setProviderMode } from "./actions";

export const metadata: Metadata = { title: "Payment providers" };

const MERCHANT: Record<string, string> = { not_started: "Not started", applied: "Applied", verified: "Verified", rejected: "Rejected" };

/**
 * Every payment provider Mise knows about: whether it can take payments now
 * and why not, its mode, currencies, recurring capability, health, masked
 * credential reference and audited changes. Secrets never reach this page.
 */
export default async function PaymentProvidersPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "payment_providers.read")) notFound();
  const { items } = await api.admin.paymentProviders(token);
  const manage = can(admin, "payment_providers.manage");
  const activate = can(admin, "payment_providers.activate");
  const taking = items.filter((p) => p.taking_payments).length;
  const live = items.filter((p) => p.taking_payments && p.setting.mode === "live").length;
  const blocked = items.filter((p) => !p.ready).length;

  return (
    <>
      <PageHeader
        title="Payment providers"
        back={{ href: "/payments", label: "Payments" }}
        description="A provider is offered to customers only when it is configured, enabled here, able to charge the bill's exact currency, and in the mode its credentials allow. Credentials live in the secret store; only masked references are shown."
      />
      <div className="grid gap-8">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <CountTile label="Known providers" value={items.length} />
          <CountTile label="Taking payments" value={taking} />
          <CountTile label="Live" value={live} />
          <CountTile label="Blocked or not configured" value={blocked} tone={blocked ? "warn" : undefined} />
        </div>
        {items.map((p) => (
          <ProviderPanel key={p.provider} p={p} manage={manage} activate={activate} />
        ))}
      </div>
    </>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="type-caption text-muted">{label}</dt>
      <dd className="type-body-sm mt-0.5">{children}</dd>
    </div>
  );
}

function ProviderPanel({ p, manage, activate }: { p: PaymentProviderStatus; manage: boolean; activate: boolean }) {
  const st = p.setting;
  return (
    <Panel
      title={p.name}
      note={
        <span className="flex flex-wrap items-center gap-2">
          {p.taking_payments ? <Tag tone="success">Taking payments</Tag> : <Tag tone={p.ready ? "neutral" : "error"}>Not taking payments</Tag>}
          <Tag tone={st.mode === "live" ? "accent" : "neutral"}>{st.mode === "live" ? "Live" : "Test mode"}</Tag>
          {!p.taking_payments ? <span className="type-body-sm text-muted">{p.not_taking_reason}</span> : null}
        </span>
      }
    >
      <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Fact label="Readiness">{p.ready ? "Configured" : p.readiness_reason}</Fact>
        <Fact label="Credentials">{p.credential_ref ? <span className="font-mono text-[0.8125rem]">{p.credential_ref}</span> : "None configured"}</Fact>
        <Fact label="Currencies">{p.currencies.length ? p.currencies.join(", ") : "None confirmed"}</Fact>
        <Fact label="Weekly renewals">{p.recurring_billing ? "Mise charges the saved method" : "Not supported: customer pays each week"}</Fact>
        <Fact label="Refunds">{p.refunds ? "Supported" : "Not supported"}</Fact>
        <Fact label="Merchant account">
          {MERCHANT[st.merchant_status] ?? humanize(st.merchant_status)}
          {st.merchant_reference ? <span className="block text-muted">Ref: {st.merchant_reference}</span> : null}
        </Fact>
        <Fact label="Live approval">
          {st.live_approved_at ? `${formatDateTime(st.live_approved_at)} · ${st.live_approval_reference}` : "None recorded"}
        </Fact>
        <Fact label="Last successful payment">{p.last_succeeded_at ? formatDateTime(p.last_succeeded_at) : "None"}</Fact>
        <Fact label="Last reconciliation">{p.last_reconciled_at ? formatDateTime(p.last_reconciled_at) : "None"}</Fact>
        <Fact label="Failed payments (24 h)">{p.failures_24h}</Fact>
        <Fact label="Webhooks (24 h)">
          {p.webhooks_received_24h} received{p.webhooks_failed ? `, ${p.webhooks_failed} failed in total` : ""}
        </Fact>
        <Fact label="Last webhook">{p.last_webhook_at ? formatDateTime(p.last_webhook_at) : "None"}</Fact>
      </dl>

      {manage && p.registered ? (
        <div className="mt-6 grid gap-6 border-t border-border pt-6 lg:grid-cols-2">
          <KForm
            action={setProviderEnabled}
            submit={st.enabled ? "Disable for checkout" : "Enable for checkout"}
            pending="Saving…"
            label={`${st.enabled ? "Disable" : "Enable"} ${p.name}`}
            variant={st.enabled ? "secondary" : "primary"}
          >
            <input type="hidden" name="provider" value={p.provider} />
            <input type="hidden" name="enabled" value={st.enabled ? "false" : "true"} />
            <Labelled label="Note" hint="Why, for the audit trail.">
              <TextInput name="note" maxLength={1000} />
            </Labelled>
          </KForm>
          <KForm action={setMerchantStatus} submit="Record merchant status" pending="Saving…" label={`Merchant status for ${p.name}`} variant="secondary">
            <input type="hidden" name="provider" value={p.provider} />
            <div className="grid gap-4 sm:grid-cols-2">
              <Labelled label="Status">
                <SelectInput name="merchant_status" defaultValue={st.merchant_status}>
                  {Object.entries(MERCHANT).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </SelectInput>
              </Labelled>
              <Labelled label="Reference" hint="Application or account reference, not a secret.">
                <TextInput name="merchant_reference" defaultValue={st.merchant_reference} maxLength={200} />
              </Labelled>
            </div>
          </KForm>
        </div>
      ) : null}

      {activate && p.registered ? (
        <div className="mt-6 border-t border-border pt-6">
          <KForm
            action={setProviderMode}
            submit={st.mode === "live" ? "Return to test mode" : "Approve live payments"}
            pending="Saving…"
            label={`Mode for ${p.name}`}
            variant="secondary"
          >
            <input type="hidden" name="provider" value={p.provider} />
            <input type="hidden" name="mode" value={st.mode === "live" ? "test" : "live"} />
            {st.mode === "live" ? null : (
              <p className="type-body-sm text-muted">
                Refused unless live credentials are in the secret store, the merchant account is verified, and you give the written approval reference.
              </p>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              {st.mode === "live" ? null : (
                <Labelled label="Approval reference">
                  <TextInput name="reference" minLength={3} maxLength={300} />
                </Labelled>
              )}
              <Labelled label="Note">
                <TextArea name="note" rows={2} maxLength={1000} />
              </Labelled>
            </div>
          </KForm>
        </div>
      ) : null}

      <details className="mt-6 border-t border-border pt-4">
        <summary className="type-body-sm cursor-pointer font-semibold">Recent changes ({p.recent_events.length})</summary>
        {p.recent_events.length ? (
          <ul className="type-body-sm mt-3 grid gap-2">
            {p.recent_events.map((e, i) => (
              <li key={i}>
                <span className="text-muted">{formatDateTime(e.created_at)}</span> · {humanize(e.action)}: {e.from || "—"} → {e.to || "—"}
                {e.note ? <span className="block text-muted">{e.note}</span> : null}
              </li>
            ))}
          </ul>
        ) : (
          <p className="type-body-sm mt-3 text-muted">No changes recorded.</p>
        )}
      </details>
    </Panel>
  );
}
