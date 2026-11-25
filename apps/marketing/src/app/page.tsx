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
import { site } from "@/content/site";
import { env } from "@/lib/env";
import { faqItems } from "@/lib/faq";
import { featuredMeals, livePlans } from "@/lib/storefront";
import { FinalCta } from "@/components/final-cta";

/** Structured data: who Mise is, and the FAQ, in a form search engines can read. */
function structuredData(questions: { question: string; answer: string }[]) {
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
        mainEntity: questions.map((item) => ({
          "@type": "Question",
          name: item.question,
          acceptedAnswer: { "@type": "Answer", text: item.answer },
        })),
      },
    ],
  };
}

/** Meals and plans are read from the API and refreshed every five minutes. */
export const revalidate = 300;

export default async function HomePage() {
  const [meals, plans, questions] = await Promise.all([featuredMeals(), livePlans(), faqItems()]);
  return (
    <>
      <script
        type="application/ld+json"
        // The content is our own static copy; "<" is escaped so it cannot close the script element.
        dangerouslySetInnerHTML={{ __html: JSON.stringify(structuredData(questions)).replace(/</g, "\\u003c") }}
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
          <MealShowcase meals={meals} />
        </SectionView>
        <SectionView section="plans">
          <Plans plans={plans} />
        </SectionView>
        <SectionView section="chefs">
          <ChefFeature />
        </SectionView>
        <SectionView section="sustainability">
          <IngredientStory />
        </SectionView>
        <SectionView section="faq">
          <FAQ items={questions} />
        </SectionView>
        {env.siteMode === "launch" ? (
          <FinalCta />
        ) : (
          <SectionView section="waitlist">
            <WaitlistSection />
          </SectionView>
        )}
      </main>
      <SiteFooter />
    </>
  );
}
