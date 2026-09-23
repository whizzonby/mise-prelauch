import type { Metadata } from "next";

import { PageShell } from "@/components/page-shell";
import { VerifyView } from "@/components/verify-view";

export const metadata: Metadata = {
  title: "Confirm your email",
  robots: { index: false, follow: false },
  alternates: { canonical: "/verify" },
};

export default function VerifyPage() {
  return (
    <PageShell>
      <VerifyView />
    </PageShell>
  );
}
