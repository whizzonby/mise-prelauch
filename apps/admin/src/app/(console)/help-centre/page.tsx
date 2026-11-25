import { buttonClasses } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { EmptyState, PageHeader, StatusTag } from "@/components/admin-ui";
import { DataTable } from "@/components/commerce-ui";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

export const metadata: Metadata = { title: "Help centre" };

const STATUSES = ["", "draft", "in_review", "published", "archived"];

/** Help articles in every state. Only published ones are public. */
export default async function HelpCentrePage({ searchParams }: { searchParams: Promise<{ status?: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "help_centre.edit")) notFound();
  const status = STATUSES.includes((await searchParams).status ?? "") ? ((await searchParams).status ?? "") : "";
  const { items } = await api.admin.helpArticles(token, status || undefined);
  return (
    <>
      <PageHeader
        title="Help centre"
        description="Draft, review, publish. Write plain text: blank lines separate paragraphs. Do not invent policies, prices, nutrition or delivery promises: write only what the business has approved."
        actions={
          <Link href="/help-centre/new" className={buttonClasses("primary")}>
            New article
          </Link>
        }
      />
      <nav aria-label="Filter by status" className="mb-4 flex flex-wrap gap-2">
        {STATUSES.map((s) => (
          <Link key={s || "all"} href={s ? `/help-centre?status=${s}` : "/help-centre"} aria-current={status === s ? "page" : undefined}
            className={buttonClasses(status === s ? "primary" : "secondary")}>
            {s ? humanize(s) : "All"}
          </Link>
        ))}
      </nav>
      {items.length ? (
        <DataTable head={["Article", "Category", "Status", "Version", "Updated"]} label="Help articles">
          {items.map((a) => (
            <tr key={a.id}>
              <td className="p-3">
                <Link href={`/help-centre/${a.id}`} className="font-semibold underline underline-offset-4">
                  {a.title}
                </Link>
                <span className="type-caption block text-muted">/help/{a.slug}</span>
              </td>
              <td className="p-3">{humanize(a.category)}</td>
              <td className="p-3">
                <StatusTag status={a.status === "published" ? "active" : a.status === "in_review" ? "pending" : a.status} label={humanize(a.status)} />
              </td>
              <td className="p-3">{a.version}</td>
              <td className="p-3">{formatDateTime(a.updated_at)}</td>
            </tr>
          ))}
        </DataTable>
      ) : (
        <EmptyState>No articles{status ? ` ${humanize(status).toLowerCase()}` : ""} yet.</EmptyState>
      )}
    </>
  );
}
