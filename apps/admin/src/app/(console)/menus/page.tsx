import { Heading, Tag, Text } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { CatalogForm } from "@/components/catalog-form";
import { DataTable, Empty, Panel } from "@/components/commerce-ui";
import { MenuFields } from "@/components/menu-fields";
import { commerceTone, formatDateTime, formatDay, humanize } from "@/lib/format";
import { api, can, requireAdmin } from "@/lib/session";

import { createMenu } from "../commerce-actions";

export const metadata: Metadata = { title: "Weekly menus" };

/** What customers can choose from each week, and when choosing closes. */
export default async function MenusPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "menus.manage")) notFound();
  const [menus, meals, plans] = await Promise.all([
    api.admin.menus(token),
    api.admin.catalogMeals(token, { status: "published" }),
    can(admin, "subscriptions.read") ? api.admin.plans(token) : Promise.resolve({ items: [] }),
  ]);
  return (
    <>
      <Heading as="h1" size="h2">
        Weekly menus
      </Heading>
      <Text className="mt-3 max-w-[48rem]" tone="muted">
        Customers see a menu once it is published, and choose until its cutoff (Trinidad and Tobago time). The next week customers are
        choosing for is the week of {formatDay(menus.next_open_week)}.
      </Text>
      <div className="mt-6">
        {menus.items.length ? (
          <DataTable head={["Week", "Status", "Meals", "Selection closes"]}>
            {menus.items.map((m) => (
              <tr key={m.id}>
                <td className="p-3">
                  <Link href={`/menus/${m.id}`} className="font-semibold underline underline-offset-4">
                    {m.week_label}
                  </Link>
                  {m.title ? <span className="type-caption block text-muted">{m.title}</span> : null}
                </td>
                <td className="p-3">
                  <Tag tone={commerceTone(m.status)}>{humanize(m.status)}</Tag>
                </td>
                <td className="p-3">{m.meal_count}</td>
                <td className="p-3">{formatDateTime(m.cutoff_at)}</td>
              </tr>
            ))}
          </DataTable>
        ) : (
          <Empty>No menus yet.</Empty>
        )}
      </div>
      <Panel title="New menu" className="mt-10">
        {meals.items.length ? (
          <CatalogForm action={createMenu} submit="Create draft menu" pending="Creating…" label="New menu">
            <MenuFields meals={meals.items} plans={plans.items} selected={new Map()} />
          </CatalogForm>
        ) : (
          <p className="type-body-sm">Publish meals in the catalogue first.</p>
        )}
      </Panel>
    </>
  );
}
