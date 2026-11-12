import { Tag } from "@mise/ui";
import type { Metadata } from "next";

import { PageHeader, Tabs } from "@/components/admin-ui";
import { CatalogForm } from "@/components/catalog-form";
import { Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { formatDateTime } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { saveSettings } from "../crm-actions";

export const metadata: Metadata = { title: "Settings" };

const STATUS = {
  not_configured: { label: "Not configured", tone: "neutral" },
  test_mode: { label: "Test mode", tone: "warning" },
  configured: { label: "Configured", tone: "accent" },
  working: { label: "Working", tone: "success" },
} as const;

/**
 * Business details staff may edit, the policies the system runs on (shown,
 * not editable here: they change through configuration and review), and
 * the state of each outside service. No secret is ever shown.
 */
export default async function SettingsPage({ searchParams }: { searchParams: Promise<{ tab?: string }> }) {
  const { token, admin } = await requireAdmin();
  const { tab = "business" } = await searchParams;
  const manage = can(admin, "settings.manage");
  const tabs = [
    { key: "business", label: "Business" },
    { key: "policies", label: "Policies" },
    { key: "integrations", label: "Integrations", hidden: !manage },
  ];
  return (
    <>
      <PageHeader title="Settings" />
      <Tabs base="/settings" current={tab} tabs={tabs} />
      <div className="mt-6">
        {tab === "integrations" && manage ? <Integrations token={token} /> : <BusinessAndPolicies token={token} manage={manage} policies={tab === "policies"} />}
      </div>
    </>
  );
}

async function BusinessAndPolicies({ token, manage, policies }: { token: string; manage: boolean; policies: boolean }) {
  const s = await api.admin.settings(token);
  if (policies) {
    return (
      <Panel title="Policies" note="How ordering, billing and accounts behave. These are read-only here: they change through configuration and a reviewed deploy, so customers are never surprised by a click.">
        <dl className="grid gap-4 md:grid-cols-2">
          {s.policies.map((p) => (
            <div key={p.key} className="border-b border-border pb-3">
              <dt className="type-body-sm font-semibold">{p.label}</dt>
              <dd className="type-body-sm">{p.value}</dd>
              <dd className="type-caption text-muted">Set in: {p.owner}</dd>
            </div>
          ))}
        </dl>
      </Panel>
    );
  }
  return (
    <Panel title="Business details" note="Shown to customers in emails and on the website.">
      {manage ? (
        <CatalogForm action={saveSettings} submit="Save settings" pending="Saving…" label="Business details">
          <div className="grid gap-4 md:grid-cols-2">
            {s.editable.map((x) => (
              <Labelled key={x.key} label={x.label} hint={x.updated_at ? `Changed ${formatDateTime(x.updated_at)}` : undefined}>
                <TextInput name={x.key} defaultValue={x.value} maxLength={300} />
              </Labelled>
            ))}
          </div>
        </CatalogForm>
      ) : (
        <dl className="grid gap-3 md:grid-cols-2">
          {s.editable.map((x) => (
            <div key={x.key}>
              <dt className="type-body-sm font-semibold">{x.label}</dt>
              <dd className="type-body-sm">{x.value || <span className="text-muted">Not set</span>}</dd>
            </div>
          ))}
        </dl>
      )}
    </Panel>
  );
}

async function Integrations({ token }: { token: string }) {
  const { items } = await api.admin.integrations(token);
  return (
    <div className="grid gap-4 md:grid-cols-2">
      {items.map((i) => {
        const st = STATUS[i.status];
        return (
          <section key={i.key} className="border border-border bg-surface p-5" aria-labelledby={`int-${i.key}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 id={`int-${i.key}`} className="type-h4">
                {i.name}
              </h2>
              <Tag tone={st.tone}>{st.label}</Tag>
            </div>
            <p className="type-body-sm mt-1 text-muted">{i.purpose}</p>
            <p className="type-body-sm mt-3">{i.detail}</p>
            <p className="type-caption mt-2 text-muted">
              {i.last_success_at ? `Last worked ${formatDateTime(i.last_success_at)}.` : "No recorded success yet."}
              {i.last_error_at ? ` Last error ${formatDateTime(i.last_error_at)}: ${i.last_error ?? ""}` : ""}
            </p>
          </section>
        );
      })}
    </div>
  );
}
