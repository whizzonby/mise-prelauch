import type { Metadata } from "next";

import { LegalPage } from "@/components/legal-page";
import { terms } from "@/content/legal";

export const metadata: Metadata = {
  title: terms.title,
  description: "The terms that apply to the Mise waitlist and invitation links.",
  alternates: { canonical: "/terms" },
};

export default function TermsPage() {
  return <LegalPage document={terms} />;
}
