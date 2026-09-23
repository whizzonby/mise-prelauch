import { Container } from "@mise/ui";
import type { ReactNode } from "react";

import { SiteFooter } from "./site-footer";
import { SiteHeader } from "./site-header";

/** Header, a main region and footer, for every page other than the home page. */
export function PageShell({ children, width = "wide" }: { children: ReactNode; width?: "wide" | "prose" }) {
  return (
    <>
      <SiteHeader />
      <main id="main" className="min-h-[70svh] py-[clamp(3rem,7vw,6.5rem)]">
        <Container width={width}>{children}</Container>
      </main>
      <SiteFooter />
    </>
  );
}
