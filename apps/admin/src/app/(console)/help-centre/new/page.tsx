import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Panel } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { can, requireAdmin } from "@/lib/session";

import { createArticle } from "../actions";
import { ArticleFields } from "../article-fields";

export const metadata: Metadata = { title: "New help article" };

export default async function NewArticlePage() {
  const { admin } = await requireAdmin();
  if (!can(admin, "help_centre.edit")) notFound();
  return (
    <>
      <PageHeader title="New help article" back={{ href: "/help-centre", label: "Help centre" }} description="It starts as a draft. Nothing is public until a publisher approves it." />
      <Panel>
        <KForm action={createArticle} submit="Create draft" pending="Creating…" label="New help article">
          <ArticleFields />
        </KForm>
      </Panel>
    </>
  );
}
