import { ChefFeature } from "@/components/chef-feature";
import { FAQ } from "@/components/faq";
import { Hero } from "@/components/hero";
import { HowItWorks } from "@/components/how-it-works";
import { IngredientStory } from "@/components/ingredient-story";
import { MealShowcase } from "@/components/meal-showcase";
import { Plans } from "@/components/plans";
import { SectionView } from "@/components/providers";
import { SiteFooter } from "@/components/site-footer";
import { SiteHeader } from "@/components/site-header";
import { SupplyStory } from "@/components/supply-story";
import { WaitlistSection } from "@/components/waitlist-section";
import { faq } from "@/content/home";
import { site } from "@/content/site";
import { env } from "@/lib/env";

/** Structured data: who Mise is, and the FAQ, in a form search engines can read. */
function structuredData() {
  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "Organization",
        "@id": `${env.siteUrl}/#organization`,
        name: site.name,
        url: env.siteUrl,
        description: site.description,
        email: site.contactEmail,
        areaServed: { "@type": "Country", name: site.origin },
      },
      {
        "@type": "WebSite",
        "@id": `${env.siteUrl}/#website`,
        url: env.siteUrl,
        name: site.name,
        publisher: { "@id": `${env.siteUrl}/#organization` },
        inLanguage: "en",
      },
      {
        "@type": "FAQPage",
        mainEntity: faq.items.map((item) => ({
          "@type": "Question",
          name: item.question,
          acceptedAnswer: { "@type": "Answer", text: item.answer },
        })),
      },
    ],
  };
}

export default function HomePage() {
  return (
    <>
      <script
        type="application/ld+json"
        // The content is our own static copy; "<" is escaped so it cannot close the script element.
        dangerouslySetInnerHTML={{ __html: JSON.stringify(structuredData()).replace(/</g, "\\u003c") }}
      />
      <SiteHeader />
      <main id="main">
        <Hero />
        <SectionView section="story">
          <SupplyStory />
        </SectionView>
        <SectionView section="how-it-works">
          <HowItWorks />
        </SectionView>
        <SectionView section="meals">
          <MealShowcase />
        </SectionView>
        <SectionView section="plans">
          <Plans />
        </SectionView>
        <SectionView section="chefs">
          <ChefFeature />
        </SectionView>
        <SectionView section="sustainability">
          <IngredientStory />
        </SectionView>
        <SectionView section="faq">
          <FAQ />
        </SectionView>
        <SectionView section="waitlist">
          <WaitlistSection />
        </SectionView>
      </main>
      <SiteFooter />
    </>
  );
}
