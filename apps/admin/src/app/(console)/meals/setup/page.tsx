import { Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { Panel } from "@/components/panel";
import { api, can, requireAdmin } from "@/lib/session";

import { createCategory, saveChef } from "../actions";

export const metadata: Metadata = { title: "Categories and chefs" };

const input = "min-h-11 w-full rounded-md border border-border-strong bg-surface px-3";

/** Categories group meals; chef profiles say who wrote them. Publish a profile only with the chef's approval. */
export default async function SetupPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "catalog.read")) notFound();
  const [categories, chefs] = await Promise.all([api.admin.catalogCategories(token), api.admin.catalogChefs(token)]);
  const manage = can(admin, "catalog.manage");
  const publish = can(admin, "catalog.publish");

  return (
    <>
      <Link href="/meals" className="type-body-sm underline underline-offset-4">
        All meals
      </Link>
      <Heading as="h1" size="h2" className="mt-3">
        Categories and chefs
      </Heading>

      <div className="mt-8 grid gap-8 xl:grid-cols-2">
        <Panel title="Categories">
          <ul className="type-body-sm divide-y divide-border border-y border-border">
            {categories.items.map((c) => (
              <li key={c.id} className="flex flex-wrap items-center gap-2 py-2.5">
                <span className="font-semibold">{c.name}</span>
                <span className="text-muted">/{c.slug}</span>
                {c.is_demo ? <Tag tone="warning">Demo</Tag> : null}
              </li>
            ))}
            {categories.items.length === 0 ? <li className="py-2.5 text-muted">No categories yet.</li> : null}
          </ul>
          {manage ? (
            <div className="mt-6">
              <CatalogForm action={createCategory} label="Add a category" submit="Add category" pending="Adding…">
                <label className="grid gap-1.5">
                  <span className="type-label">Name</span>
                  <input name="name" required maxLength={60} className={input} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Description</span>
                  <input name="description" maxLength={300} className={input} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Order (lower comes first)</span>
                  <input name="position" type="number" min={0} max={1000} defaultValue={10} className={input} />
                </label>
                <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                  <input type="checkbox" name="is_demo" className="size-5" /> Demo content
                </label>
              </CatalogForm>
            </div>
          ) : null}
        </Panel>

        <Panel title="Chef profiles" note={publish ? undefined : "Publishing a profile needs catalog.publish"}>
          <ul className="type-body-sm divide-y divide-border border-y border-border">
            {chefs.items.map((c) => (
              <li key={c.id} className="flex flex-wrap items-center gap-2 py-2.5">
                <span className="font-semibold">{c.display_name}</span>
                <Tag tone={c.status === "published" ? "success" : "neutral"}>{c.status}</Tag>
                {c.is_demo ? <Tag tone="warning">Demo</Tag> : null}
              </li>
            ))}
            {chefs.items.length === 0 ? <li className="py-2.5 text-muted">No chef profiles yet.</li> : null}
          </ul>
          {manage ? (
            <div className="mt-6">
              <CatalogForm action={saveChef} label="Add a chef profile" submit="Save profile" pending="Saving…">
                <label className="grid gap-1.5">
                  <span className="type-label">Name as shown to customers</span>
                  <input name="display_name" required maxLength={80} className={input} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Title</span>
                  <input name="role_title" maxLength={80} className={input} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Biography (approved by the chef)</span>
                  <textarea name="bio" rows={4} maxLength={2000} className={`${input} py-2`} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Photograph</span>
                  <input name="image_src" placeholder="/catalog/chefs/name.jpg" className={input} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Photograph description</span>
                  <input name="image_alt" maxLength={200} className={input} />
                </label>
                <label className="grid gap-1.5">
                  <span className="type-label">Status</span>
                  <select name="status" defaultValue="draft" className={input}>
                    <option value="draft">Draft</option>
                    {publish ? <option value="published">Published</option> : null}
                  </select>
                </label>
                <label className="type-body-sm inline-flex min-h-11 items-center gap-2">
                  <input type="checkbox" name="is_demo" className="size-5" /> Demo content
                </label>
              </CatalogForm>
            </div>
          ) : null}
        </Panel>
      </div>
    </>
  );
}
