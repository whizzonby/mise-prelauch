import type { LeadFilter } from "@mise/api-client";
import { buttonClasses, Field, Heading, Input, Select, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { formatDate, fullName, humanize, LEAD_STATUSES, number, PACKAGING_LABELS, statusTone } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Leads" };

const FILTER_KEYS = ["q", "status", "location", "source", "packaging", "referral", "from", "to"] as const;
type FilterKey = (typeof FILTER_KEYS)[number];
type Params = Partial<Record<FilterKey | "page" | "erased", string>>;

function queryString(filter: Partial<Record<FilterKey, string>>, extra: Record<string, string | number> = {}): string {
  const params = new URLSearchParams();
  for (const key of FILTER_KEYS) if (filter[key]) params.set(key, filter[key]);
  for (const [key, value] of Object.entries(extra)) params.set(key, String(value));
  const text = params.toString();
  return text ? `?${text}` : "";
}

export default async function LeadsPage({ searchParams }: { searchParams: Promise<Params> }) {
  const { token, admin } = await requireAdmin();
  const params = await searchParams;

  const active: Partial<Record<FilterKey, string>> = {};
  for (const key of FILTER_KEYS) {
    const value = params[key]?.trim();
    if (value) active[key] = value;
  }
  const page = Math.max(1, Number(params.page) || 1);
  const filter: LeadFilter = { ...active, page, page_size: 25 };

  const [leads, overview] = await Promise.all([
    api.admin.listLeads(token, filter),
    // The dashboard breakdowns double as the lists of filter options.
    can(admin, "stats:read") ? api.admin.overview(token, 30) : null,
  ]);
  const pages = Math.max(1, Math.ceil(leads.total / leads.page_size));
  const hasFilters = Object.keys(active).length > 0;

  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading as="h1" size="h2">
          Leads
        </Heading>
        {can(admin, "leads:export") ? (
          // A plain link, not a Next link: it downloads a file.
          <a href={`/leads/export${queryString(active)}`} className={buttonClasses("secondary", "md")}>
            Export {hasFilters ? "these" : "all"} leads as CSV
          </a>
        ) : null}
      </div>

      {params.erased ? (
        <p role="status" className="type-body-sm mt-5 border-l-4 border-success bg-surface py-3 pl-4">
          The lead and everything attached to it has been deleted.
        </p>
      ) : null}

      <form method="get" className="mt-6 grid gap-4 border border-border bg-surface p-5 sm:grid-cols-2 lg:grid-cols-4">
        <Field id="q" label="Name or email">
          <Input id="q" name="q" type="search" defaultValue={active.q} />
        </Field>
        <Field id="status" label="Status">
          <Select id="status" name="status" defaultValue={active.status ?? ""}>
            <option value="">Any status</option>
            {LEAD_STATUSES.map((status) => (
              <option key={status} value={status}>
                {humanize(status.toLowerCase())}
              </option>
            ))}
          </Select>
        </Field>
        <Field id="referral" label="Referral activity">
          <Select id="referral" name="referral" defaultValue={active.referral ?? ""}>
            <option value="">Any</option>
            <option value="referrer">Has referred a friend</option>
            <option value="referred">Was invited by someone</option>
          </Select>
        </Field>
        <Field id="location" label="Location">
          {overview ? (
            <Select id="location" name="location" defaultValue={active.location ?? ""}>
              <option value="">Any location</option>
              {overview.locations.map((row) => (
                <option key={row.label} value={row.label}>
                  {humanize(row.label)}
                </option>
              ))}
            </Select>
          ) : (
            <Input id="location" name="location" defaultValue={active.location} />
          )}
        </Field>
        <Field id="source" label="Source">
          {overview ? (
            <Select id="source" name="source" defaultValue={active.source ?? ""}>
              <option value="">Any source</option>
              {overview.sources.map((row) => (
                <option key={row.label} value={row.label}>
                  {humanize(row.label)}
                </option>
              ))}
            </Select>
          ) : (
            <Input id="source" name="source" defaultValue={active.source} />
          )}
        </Field>
        <Field id="packaging" label="Packaging vote">
          <Select id="packaging" name="packaging" defaultValue={active.packaging ?? ""}>
            <option value="">Any</option>
            {Object.entries(PACKAGING_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </Select>
        </Field>
        <Field id="from" label="Signed up from">
          <Input id="from" name="from" type="date" defaultValue={active.from} />
        </Field>
        <Field id="to" label="Signed up to">
          <Input id="to" name="to" type="date" defaultValue={active.to} />
        </Field>
        <div className="flex items-center gap-5 sm:col-span-2 lg:col-span-4">
          <button type="submit" className={buttonClasses("primary", "md")}>
            Apply filters
          </button>
          {hasFilters ? (
            <Link href="/leads" className="type-body-sm underline underline-offset-4">
              Clear filters
            </Link>
          ) : null}
        </div>
      </form>

      <p role="status" className="type-body-sm mt-6 text-muted">
        {number.format(leads.total)} {leads.total === 1 ? "lead" : "leads"}
        {hasFilters ? " match these filters" : ""}
      </p>

      {leads.items.length === 0 ? (
        <Text className="mt-4 border border-border bg-surface p-6">
          {hasFilters ? "No leads match these filters. Clear a filter to widen the search." : "No one has joined the waitlist yet."}
        </Text>
      ) : (
        <div className="mt-3 overflow-x-auto border border-border bg-surface">
          <table className="type-body-sm w-full min-w-[52rem] text-left">
            <thead>
              <tr className="border-b border-foreground">
                {["Name", "Email", "Status", "Location", "Source", "Referrals", "Signed up"].map((heading) => (
                  <th key={heading} scope="col" className="px-4 py-3 font-semibold">
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {leads.items.map((lead) => (
                <tr key={lead.id} className="border-b border-border last:border-0 hover:bg-background">
                  <th scope="row" className="px-4 py-3 font-semibold">
                    <Link href={`/leads/${lead.id}`} className="underline underline-offset-4 hover:decoration-2">
                      {fullName(lead)}
                    </Link>
                  </th>
                  <td className="px-4 py-3">{lead.email}</td>
                  <td className="px-4 py-3">
                    <Tag tone={statusTone(lead.status)}>{humanize(lead.status.toLowerCase())}</Tag>
                  </td>
                  <td className="px-4 py-3">{humanize(lead.location)}</td>
                  <td className="px-4 py-3">{humanize(lead.source)}</td>
                  <td className="type-figures px-4 py-3">
                    {lead.referrals_converted}
                    {lead.was_referred ? <span className="text-muted"> (invited)</span> : null}
                  </td>
                  <td className="type-figures whitespace-nowrap px-4 py-3">{formatDate(lead.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {pages > 1 ? (
        <nav aria-label="Pages" className="mt-5 flex items-center justify-between gap-4">
          {page > 1 ? (
            <Link href={`/leads${queryString(active, { page: page - 1 })}`} className={buttonClasses("secondary", "md")}>
              Previous page
            </Link>
          ) : (
            <span />
          )}
          <p className="type-body-sm type-figures text-muted">
            Page {page} of {pages}
          </p>
          {page < pages ? (
            <Link href={`/leads${queryString(active, { page: page + 1 })}`} className={buttonClasses("secondary", "md")}>
              Next page
            </Link>
          ) : (
            <span />
          )}
        </nav>
      ) : null}
    </>
  );
}
