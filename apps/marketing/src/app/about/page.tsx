import { Container, Section, Text } from "@mise/ui";
import type { Metadata } from "next";

import { IngredientStory } from "@/components/ingredient-story";
import { SiteFooter } from "@/components/site-footer";
import { SiteHeader } from "@/components/site-header";
import { SupplyStory } from "@/components/supply-story";
import { site } from "@/content/site";

export const metadata: Metadata = {
  title: "About Mise",
  description: "Mise is a Caribbean meal-kit company in Trinidad & Tobago: local ingredients, prepped and portioned, for you to cook at home.",
  alternates: { canonical: "/about" },
};

/*
 * Only what Mise has already said about itself. No team biographies, no
 * founding story, no addresses or certifications: those are added here when
 * the business supplies and approves them (docs/launch/GO_LIVE_CHECKLIST.md,
 * content items).
 */
export default function AboutPage() {
  return (
    <>
      <SiteHeader />
      <main id="main">
        <Section aria-labelledby="page-title" className="pb-0">
          <Container>
            <h1 id="page-title" className="type-display max-w-[14ch] text-primary">
              About Mise
            </h1>
            <Text size="lg" className="mt-8 max-w-[44ch]">
              {site.name} is a Caribbean meal kit from {site.origin}. We season, chop and portion fresh ingredients in our kitchen, and you
              do the cooking at home.
            </Text>
          </Container>
        </Section>
        <SupplyStory />
        <IngredientStory />
      </main>
      <SiteFooter />
    </>
  );
}
