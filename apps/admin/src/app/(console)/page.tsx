import { cx, Heading, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { BarList, SignupsChart } from "@/components/charts";
import { Panel, StatTile } from "@/components/panel";
import { humanize, number, packagingLabel, percent } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Dashboard" };

const RANGES = [30, 90, 180];

export default async function DashboardPage({ searchParams }: { searchParams: Promise<{ days?: string }> }) {
  const { token, admin } = await requireAdmin();

  if (!can(admin, "stats:read")) {
    return (
      <>
        <Heading as="h1" size="h2">
          Dashboard
        </Heading>
        <Text className="mt-4 max-w-[50ch]">
          Your role does not include the waitlist statistics. You can still look up individual leads.
        </Text>
        <Link href="/leads" className="mt-4 inline-block font-semibold underline underline-offset-4">
          Go to leads
        </Link>
      </>
    );
  }

  const requested = Number((await searchParams).days);
  const days = RANGES.includes(requested) ? requested : 30;
  const overview = await api.admin.overview(token, days);
  const rangeTotal = overview.signups_by_day.reduce((sum, day) => sum + day.count, 0);

  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading as="h1" size="h2">
          Dashboard
        </Heading>
        <nav aria-label="Time range" className="flex gap-1">
          {RANGES.map((range) => (
            <Link
              key={range}
              href={range === 30 ? "/" : `/?days=${range}`}
              aria-current={range === days ? "true" : undefined}
              className={cx(
                "type-body-sm rounded-md border px-3 py-1.5 font-semibold",
                range === days ? "border-primary bg-primary text-primary-foreground" : "border-border-strong hover:border-foreground",
              )}
            >
              {range} days
            </Link>
          ))}
        </nav>
      </div>

      <div className="mt-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatTile
          label="Total leads"
          value={number.format(overview.total_leads)}
          context={`${number.format(overview.pending_leads)} waiting to confirm their email`}
        />
        <StatTile
          label="Verified leads"
          value={number.format(overview.verified_leads)}
          context={`${percent(overview.verified_leads, overview.total_leads)} of all leads`}
        />
        <StatTile
          label="Referral conversions"
          value={number.format(overview.referral_conversions)}
          context={`${number.format(overview.referred_signups)} signups arrived by invitation`}
        />
        <StatTile
          label="Profiles completed"
          value={number.format(overview.profiles_completed)}
          context={`${number.format(overview.unsubscribed_leads)} unsubscribed`}
        />
      </div>

      <Panel title="Signups over time" note={`${number.format(rangeTotal)} in the last ${days} days (UTC days)`} className="mt-4">
        <SignupsChart days={overview.signups_by_day} />
      </Panel>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Panel title="Top acquisition sources" note="First touch">
          <BarList rows={overview.sources.map((row) => ({ ...row, label: humanize(row.label) }))} total={overview.total_leads} empty="No leads yet." />
        </Panel>
        <Panel title="Locations">
          <BarList rows={overview.locations.map((row) => ({ ...row, label: humanize(row.label) }))} total={overview.total_leads} empty="No leads yet." />
        </Panel>
        <Panel title="Dietary interests" note="A lead can choose several">
          <BarList
            rows={overview.dietary.map((row) => ({ ...row, label: humanize(row.label) }))}
            total={overview.total_leads}
            empty="Nobody has chosen a dietary interest yet."
          />
        </Panel>
        <Panel title="Packaging preference" note="The vote on the waitlist form" className="lg:col-span-2">
          <BarList
            rows={overview.packaging.map((row) => ({ ...row, label: packagingLabel(row.label) }))}
            total={overview.total_leads}
            empty="No leads yet."
          />
        </Panel>
        <Panel title="Household size">
          <BarList
            rows={overview.household_sizes.map((row) => ({ ...row, label: /^\d/.test(row.label) ? `${row.label} ${row.label === "1" ? "person" : "people"}` : row.label }))}
            total={overview.total_leads}
            empty="No leads yet."
          />
        </Panel>
      </div>
    </>
  );
}
