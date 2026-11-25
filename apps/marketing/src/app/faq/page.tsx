import type { Metadata } from "next";
import { connection } from "next/server";

import { FAQ } from "@/components/faq";
import { SiteFooter } from "@/components/site-footer";
import { SiteHeader } from "@/components/site-header";
import { faqItems } from "@/lib/faq";

export const metadata: Metadata = {
  title: "Questions",
  description: "Answers about Mise meal kits: how plans work, pausing and cancelling, what is in a kit, delivery and allergies.",
  alternates: { canonical: "/faq" },
};


export default async function FaqPage() {
  // Rendered per request, not at build: the API is not reachable while images are built, and a
  // page baked then would show "could not be loaded" after every deploy. The reads stay cached for
  // five minutes (lib/storefront.ts).
  await connection();
  const items = await faqItems();
  const data = {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: items.map((item) => ({ "@type": "Question", name: item.question, acceptedAnswer: { "@type": "Answer", text: item.answer } })),
  };
  return (
    <>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data).replace(/</g, "\u003c") }} />
      <SiteHeader />
      <main id="main">
        <FAQ items={items} headingLevel="h1" />
      </main>
      <SiteFooter />
    </>
  );
}
