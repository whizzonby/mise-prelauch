import type { ReactNode } from "react";

import { SiteShell } from "@/components/site-shell";
import { requireCustomer } from "@/lib/session";

/** The signed-in customer app: one header, the page, a quiet footer. */
export default async function AppLayout({ children }: { children: ReactNode }) {
  const { customer } = await requireCustomer();
  return <SiteShell customer={customer}>{children}</SiteShell>;
}
