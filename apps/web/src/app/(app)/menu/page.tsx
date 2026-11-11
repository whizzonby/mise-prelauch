import { buttonClasses, Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { EmptyState } from "@/components/meal-card";
import { selectHref } from "@/lib/commerce";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "This week's menu" };

/**
 * "Menu" goes where it is useful: to choosing meals for the week that is
 * open, if the customer has one; otherwise to the next published menu.
 */
export default async function MenuIndex() {
  const { token } = await requireCustomer();
  const [{ items: weeks }, { items: menus }] = await Promise.all([api.commerce.weeks(token), api.shop.menus()]);
  const open = weeks
    .filter((w) => w.selection_open)
    .sort((a, b) => a.week.localeCompare(b.week))[0];
  if (open) redirect(selectHref(open.week));
  const next = menus.find((m) => m.is_open) ?? menus[0];
  if (next) redirect(`/menu/${next.week}`);
  return (
    <Container className="py-10 md:py-14">
      <h1 className="type-h1">This week&apos;s menu</h1>
      <div className="mt-8">
        <EmptyState
          title="The next menu isn't out yet."
          action={
            <Link href="/meals" className={buttonClasses("primary")}>
              Explore meals
            </Link>
          }
        >
          We publish each week&apos;s menu ahead of its cutoff. Subscribers get an email when it&apos;s ready.
        </EmptyState>
      </div>
    </Container>
  );
}
