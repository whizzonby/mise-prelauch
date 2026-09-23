import { ApiError, type LeadDetail, type ReferralEntry } from "@mise/api-client";
import { Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EraseLeadForm, StatusForm } from "@/components/lead-actions";
import { DetailList, Panel } from "@/components/panel";
import { formatDateTime, humanize, statusTone } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Lead" };

function list(values: string[] | undefined): string | null {
  return values && values.length > 0 ? values.map(humanize).join(", ") : null;
}

function ReferralRow({ entry }: { entry: ReferralEntry }) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-b border-border py-2.5 last:border-0">
      <Link href={`/leads/${entry.lead_id}`} className="type-body-sm font-semibold underline underline-offset-4">
        {entry.first_name}
      </Link>
      <span className="flex items-center gap-3">
        <Tag tone={statusTone(entry.status)}>{humanize(entry.status)}</Tag>
        <span className="type-caption type-figures text-muted">{formatDateTime(entry.converted_at ?? entry.created_at)}</span>
      </span>
    </li>
  );
}

/** A plain-language line for one timeline entry. */
function describe(entry: LeadDetail["timeline"][number]): string {
  const meta = entry.metadata as Record<string, string | number | boolean | undefined>;
  if (entry.kind === "audit") {
    if (entry.type === "lead.status_changed") return `Status changed from ${meta.from} to ${meta.to} by an admin`;
    return humanize(entry.type.replace(".", " "));
  }
  const detail = meta.path ?? meta.section ?? meta.question ?? meta.meal ?? meta.plan ?? meta.channel ?? meta.cta;
  return detail === undefined ? humanize(entry.type) : `${humanize(entry.type)}: ${detail}`;
}

export default async function LeadPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  const { id } = await params;

  let lead: LeadDetail;
  try {
    lead = await api.admin.getLead(token, id);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) notFound();
    throw error;
  }
  const prefs = lead.preferences;

  return (
    <>
      <Link href="/leads" className="type-body-sm underline underline-offset-4">
        Back to all leads
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
        <Heading as="h1" size="h2">
          {lead.first_name}
        </Heading>
        <Tag tone={statusTone(lead.status)}>{humanize(lead.status.toLowerCase())}</Tag>
      </div>
      <Text tone="muted" className="mt-1">
        Joined {formatDateTime(lead.created_at)}
      </Text>

      <div className="mt-6 grid gap-4 lg:grid-cols-2">
        <Panel title="Profile">
          <DetailList
            items={[
              ["Email", <a key="e" href={`mailto:${lead.email}`} className="underline underline-offset-4">{lead.email}</a>],
              ["Phone", lead.phone],
              ["Location", humanize(lead.location)],
              ["Email confirmed", lead.email_verified_at ? formatDateTime(lead.email_verified_at) : "Not yet"],
              ["Consent given", `${formatDateTime(lead.consent_at)} (wording ${lead.consent_version})`],
              ["Invitation code", lead.referral_code],
            ]}
          />
        </Panel>

        <Panel title="Preferences" note={prefs ? `Updated ${formatDateTime(prefs.updated_at)}` : undefined}>
          {prefs ? (
            <DetailList
              items={[
                ["Household size", prefs.household_size],
                ["Dietary interests", list(prefs.dietary_preferences)],
                ["Menus of interest", list(prefs.meal_interests)],
                ["Meals per week", prefs.meals_per_week],
                ["Cooks at home", prefs.cooking_frequency ? humanize(prefs.cooking_frequency) : null],
                ...Object.entries(prefs.metadata ?? {}).map(([key, value]): [string, string] => [humanize(key), humanize(String(value))]),
              ]}
            />
          ) : (
            <Text size="sm" tone="muted">
              No preferences recorded.
            </Text>
          )}
        </Panel>

        <Panel title="Attribution">
          {lead.attribution.length === 0 ? (
            <Text size="sm" tone="muted">
              No attribution was captured. The visitor arrived directly, or their browser asked not to be tracked.
            </Text>
          ) : (
            <div className="grid gap-5">
              {lead.attribution.map((touch) => (
                <div key={touch.touch}>
                  <p className="type-label mb-2">{touch.touch === "first" ? "First touch" : "Latest touch"}</p>
                  <DetailList
                    items={[
                      ["Source", touch.utm_source],
                      ["Medium", touch.utm_medium],
                      ["Campaign", touch.utm_campaign],
                      ["Content", touch.utm_content],
                      ["Term", touch.utm_term],
                      ["Landing page", touch.landing_page],
                      ["Referring site", touch.referrer_url],
                    ]}
                  />
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Referral history">
          <p className="type-label mb-1">Invited by</p>
          {lead.referred_by ? (
            <ul>
              <ReferralRow entry={lead.referred_by} />
            </ul>
          ) : (
            <Text size="sm" tone="muted">
              Nobody. This lead found Mise on their own.
            </Text>
          )}
          <p className="type-label mb-1 mt-5">Friends invited ({lead.referrals.length})</p>
          {lead.referrals.length > 0 ? (
            <ul>
              {lead.referrals.map((entry) => (
                <ReferralRow key={entry.lead_id} entry={entry} />
              ))}
            </ul>
          ) : (
            <Text size="sm" tone="muted">
              None yet.
            </Text>
          )}
        </Panel>

        <Panel title="Activity" note="Most recent first" className="lg:col-span-2">
          {lead.timeline.length === 0 ? (
            <Text size="sm" tone="muted">
              No activity recorded.
            </Text>
          ) : (
            <ol className="max-h-96 overflow-y-auto">
              {lead.timeline.map((entry, index) => (
                <li key={index} className="flex flex-wrap justify-between gap-x-6 gap-y-0.5 border-b border-border py-2 last:border-0">
                  <span className="type-body-sm">{describe(entry)}</span>
                  <span className="type-caption type-figures text-muted">{formatDateTime(entry.created_at)}</span>
                </li>
              ))}
            </ol>
          )}
        </Panel>

        {can(admin, "leads:write") ? (
          <Panel title="Change status">
            <StatusForm id={lead.id} current={lead.status} />
          </Panel>
        ) : null}

        {can(admin, "leads:erase") ? (
          <Panel title="Delete this lead">
            <EraseLeadForm id={lead.id} name={lead.first_name} />
          </Panel>
        ) : null}
      </div>
    </>
  );
}
