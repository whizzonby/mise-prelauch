import { ApiError } from "@mise/api-client";
import { Heading, Tag } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { Labelled, Panel, TextInput } from "@/components/commerce-ui";
import { MenuFields } from "@/components/menu-fields";
import { commerceTone, formatDateTime, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { publishMenu, updateMenu } from "../../commerce-actions";

export const metadata: Metadata = { title: "Weekly menu" };

export default async function MenuPage({ params }: { params: Promise<{ id: string }> }) {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "menus.manage")) notFound();
  const { id } = await params;
  const menu = await api.admin.menu(token, id).catch((e) => {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  });
  const [meals, plans] = await Promise.all([
    api.admin.catalogMeals(token, {}),
    can(admin, "subscriptions.read") ? api.admin.plans(token) : Promise.resolve({ items: [] }),
  ]);
  const onMenu = new Set(menu.meals.map((m) => m.meal_id));
  const selectable = meals.items.filter((m) => m.status === "published" || onMenu.has(m.id));
  const selected = new Map(menu.meals.map((m) => [m.meal_id, { max: m.max_per_customer, chosen: m.selected }]));
  const editable = menu.status !== "closed" && menu.status !== "archived";
  const published = menu.status === "published";

  return (
    <>
      <Link href="/menus" className="type-body-sm underline underline-offset-4">
        All menus
      </Link>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Heading as="h1" size="h2">
          {menu.week_label}
        </Heading>
        <Tag tone={commerceTone(menu.status)}>{humanize(menu.status)}</Tag>
      </div>
      <p className="type-body-sm mt-2 text-muted">
        Selection closes {formatDateTime(menu.cutoff_at)} (Trinidad and Tobago time).
        {menu.publish_at ? ` Publishes ${formatDateTime(menu.publish_at)}.` : ""}
      </p>

      {editable && can(admin, "menus.publish") ? (
        <Panel title={published ? "Published" : "Publish"} className="mt-8 max-w-[40rem]">
          <CatalogForm
            action={publishMenu}
            submit={published ? "Unpublish" : "Publish"}
            pending="Updating…"
            label="Publish menu"
          >
            <input type="hidden" name="id" value={menu.id} />
            <input type="hidden" name="action" value={published ? "unpublish" : "publish"} />
            {published ? (
              <p className="type-body-sm text-muted">Unpublishing is refused once customers have chosen meals from this menu.</p>
            ) : (
              <Labelled label="Publish at" hint="Empty: publish now. Every meal must be published in the catalogue.">
                <TextInput name="publish_at" type="datetime-local" />
              </Labelled>
            )}
          </CatalogForm>
        </Panel>
      ) : null}

      <Panel title="Menu" className="mt-8">
        {editable ? (
          <CatalogForm action={updateMenu} submit="Save menu" pending="Saving…" label="Menu">
            <input type="hidden" name="id" value={menu.id} />
            <input type="hidden" name="version" value={menu.version} />
            <input type="hidden" name="week" value={menu.week} />
            <MenuFields
              meals={selectable}
              plans={plans.items}
              selected={selected}
              value={{ week: menu.week, title: menu.title, note: menu.note, cutoff_at: menu.cutoff_at, eligible_plan_ids: menu.eligible_plan_ids }}
              locked={published}
            />
          </CatalogForm>
        ) : (
          <ul className="type-body-sm grid gap-1">
            {menu.meals.map((m) => (
              <li key={m.meal_id}>{m.name}</li>
            ))}
          </ul>
        )}
      </Panel>
    </>
  );
}
