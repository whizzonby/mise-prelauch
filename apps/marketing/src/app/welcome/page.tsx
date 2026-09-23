import type { Metadata } from "next";

import { PageShell } from "@/components/page-shell";
import { WelcomeView } from "@/components/welcome-view";

export const metadata: Metadata = {
  title: "Your Mise page",
  robots: { index: false, follow: false },
  alternates: { canonical: "/welcome" },
};

export default function WelcomePage() {
  return (
    <PageShell>
      <WelcomeView />
    </PageShell>
  );
}
