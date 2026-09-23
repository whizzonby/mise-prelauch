import type { Metadata } from "next";

import { LegalPage } from "@/components/legal-page";
import { privacy } from "@/content/legal";

export const metadata: Metadata = {
  title: privacy.title,
  description: "What Mise collects when you join the waitlist, why, and the choices you have.",
  alternates: { canonical: "/privacy" },
};

export default function PrivacyPage() {
  return <LegalPage document={privacy} />;
}
