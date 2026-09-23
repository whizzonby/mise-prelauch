import { Heading, Text } from "@mise/ui";
import type { Metadata } from "next";

import { PageShell } from "@/components/page-shell";
import { imageCredits } from "@/content/image-credits";

export const metadata: Metadata = {
  title: "Photo credits",
  description: "Credits and licences for the photographs on this site.",
  alternates: { canonical: "/credits" },
  robots: { index: false, follow: true },
};

export default function CreditsPage() {
  return (
    <PageShell width="prose">
      <Heading as="h1" size="h2" className="text-primary">
        Photo credits
      </Heading>
      <Text size="lg" className="mt-6">
        The photographs on this site are stand-ins until Mise&apos;s own are ready. They are stock photographs from Pexels,
        used under the Pexels License. They do not show Mise&apos;s cooking, kitchen or team.
      </Text>
      <ul className="mt-10 border-t border-foreground">
        {imageCredits.map((credit) => (
          <li key={credit.file} className="type-body-sm border-b border-border py-3">
            <a href={credit.source} className="font-semibold underline underline-offset-4" rel="noopener noreferrer">
              {credit.title}
            </a>{" "}
            by {credit.author},{" "}
            {credit.licenseUrl ? (
              <a href={credit.licenseUrl} className="underline underline-offset-4" rel="noopener noreferrer license">
                {credit.license}
              </a>
            ) : (
              credit.license
            )}
          </li>
        ))}
      </ul>
    </PageShell>
  );
}
