/**
 * Site-wide content. Everything a visitor reads lives in src/content so copy
 * can change without touching components, and so a CMS can replace these
 * files later by returning the same shapes.
 */
export const site = {
  name: "Mise",
  title: "Mise: Caribbean meal kits, prepped and ready to cook",
  description:
    "Mise delivers fresh Caribbean meal kits in Trinidad & Tobago: local ingredients, seasoned, chopped and portioned in our kitchen, ready for you to cook. Join the waitlist.",
  locale: "en_TT",
  contactEmail: "hello@mise.tt",
  /** Shown in the footer and in structured data. */
  origin: "Trinidad & Tobago",
} as const;

export const navigation = [
  { label: "How it works", href: "/#how-it-works" },
  { label: "Meals", href: "/#meals" },
  { label: "Our story", href: "/#our-story" },
  { label: "Sustainability", href: "/#sustainability" },
  { label: "FAQ", href: "/#faq" },
] as const;

export const primaryCta = { label: "Join the waitlist", href: "/#waitlist" } as const;

export const footer = {
  line: "Caribbean meal kits, prepped in Trinidad & Tobago.",
  legal: [
    { label: "Privacy", href: "/privacy" },
    { label: "Terms", href: "/terms" },
    { label: "Photo credits", href: "/credits" },
  ],
} as const;
