import type { Metadata } from "next";
import type { ReactNode } from "react";

import { SiteShell } from "@/components/site-shell";
import { optionalSession } from "@/lib/session";

/** The public catalogue may be indexed; every other page in this app may not. */
export const metadata: Metadata = {
  title: { default: "Mise meal kits", template: "%s | Mise" },
  robots: { index: true, follow: true },
  openGraph: { siteName: "Mise", locale: "en_TT", type: "website" },
};

/**
 * Meals, chefs and plans: open to visitors. A signed-in customer sees the same
 * pages inside their account navigation, with saving and their plan shown.
 */
export default async function StoreLayout({ children }: { children: ReactNode }) {
  const session = await optionalSession();
  return <SiteShell customer={session?.customer ?? null}>{children}</SiteShell>;
}
