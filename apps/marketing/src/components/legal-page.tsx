import { Heading, Text } from "@mise/ui";

import type { LegalDocument } from "@/content/legal";

import { PageShell } from "./page-shell";

export function LegalPage({ document }: { document: LegalDocument }) {
  return (
    <PageShell width="prose">
      <article>
        <Heading as="h1" size="h2" className="text-primary">
          {document.title}
        </Heading>
        <Text size="sm" tone="muted" className="mt-3">
          Last updated {document.updated}
        </Text>
        <Text size="lg" className="mt-8">
          {document.intro}
        </Text>
        {document.sections.map((section) => (
          <section key={section.heading} className="mt-12">
            <Heading as="h2" size="h3">
              {section.heading}
            </Heading>
            {section.paragraphs.map((paragraph) => (
              <Text key={paragraph} className="mt-4">
                {paragraph}
              </Text>
            ))}
          </section>
        ))}
      </article>
    </PageShell>
  );
}
