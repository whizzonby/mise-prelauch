import type { Metadata } from "next";

import { PageShell } from "@/components/page-shell";
import { UnsubscribeView } from "@/components/unsubscribe-view";

export const metadata: Metadata = {
  title: "Unsubscribe",
  robots: { index: false, follow: false },
  alternates: { canonical: "/unsubscribe" },
};

export default function UnsubscribePage() {
  return (
    <PageShell>
      <UnsubscribeView />
    </PageShell>
  );
}
