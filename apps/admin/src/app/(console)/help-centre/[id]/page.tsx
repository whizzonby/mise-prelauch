import { ApiError } from "@mise/api-client";
import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader, StatusTag } from "@/components/admin-ui";
import { Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { transitionArticle, updateArticle } from "../actions";
import { ArticleFields } from "../article-fields";

export const metadata: Metadata = { title: "Help article" };

const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? "http://localhost:3102";

/** Edit a draft, move it through review, publish, archive or revise it. */
export default async function ArticlePage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "help_centre.edit")) notFound();
  const { id } = await params;
  const a = await api.admin.helpArticle(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const publisher = can(admin, "help_centre.publish");
  const steps: { action: string; label: string; show: boolean; primary?: boolean }[] = [
    { action: "submit", label: "Send for review", show: a.status === "draft", primary: true },
    { action: "publish", label: "Publish", show: a.status === "in_review" && publisher, primary: true },
    { action: "return", label: "Return to draft", show: a.status === "in_review" && publisher },
    { action: "archive", label: "Archive (take off the site)", show: a.status === "published" && publisher },
    { action: "revise", label: "Revise (takes it off the site)", show: a.status === "published" || a.status === "archived" },
  ];

  return (
    <>
      <PageHeader
        title={a.title}
        back={{ href: "/help-centre", label: "Help centre" }}
        description={
          <>
            <StatusTag status={a.status === "published" ? "active" : a.status === "in_review" ? "pending" : a.status} label={humanize(a.status)} /> Version {a.version}
            {a.status === "published" ? (
              <>
                {" · "}
                <a href={`${appUrl}/help/${a.slug}`} className="underline underline-offset-2">
                  View on the site
                </a>
              </>
            ) : null}
          </>
        }
      />
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <Panel title={a.status === "draft" ? "Edit draft" : "Article"} note={a.status === "draft" ? undefined : "Only drafts can be edited. Use Revise to make changes."}>
          <KForm action={updateArticle} submit="Save draft" pending="Saving…" label="Edit help article">
            <input type="hidden" name="id" value={a.id} />
            <ArticleFields a={a} disabled={a.status !== "draft"} />
          </KForm>
        </Panel>
        <div className="grid content-start gap-6">
          <Panel title="Workflow" note={publisher ? undefined : "A help centre publisher reviews and publishes."}>
            <div className="grid gap-3">
              {steps
                .filter((s) => s.show)
                .map((s) => (
                  <KForm key={s.action} action={transitionArticle} submit={s.label} pending="Saving…" label={s.label} variant={s.primary ? "primary" : "secondary"}>
                    <input type="hidden" name="id" value={a.id} />
                    <input type="hidden" name="action" value={s.action} />
                    {s.action !== "submit" ? (
                      <Labelled label="Note (optional)">
                        <TextInput name="note" maxLength={1000} />
                      </Labelled>
                    ) : null}
                  </KForm>
                ))}
            </div>
          </Panel>
          <Panel title="History">
            <ol className="type-caption grid gap-1.5 text-muted">
              {a.history.map((e, i) => (
                <li key={i}>
                  {formatDateTime(e.created_at)} · {humanize(e.action)} (v{e.version}){e.actor_name ? ` by ${e.actor_name}` : ""}
                  {e.note ? <span className="block">{e.note}</span> : null}
                </li>
              ))}
            </ol>
          </Panel>
        </div>
      </div>
    </>
  );
}
