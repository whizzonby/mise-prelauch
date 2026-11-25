import { env } from "@/lib/env";

/**
 * Site-wide content. Everything a visitor reads lives in src/content so copy
 * can change without touching components, and so a CMS can replace these
 * files later by returning the same shapes.
 */
export const site = {
  name: "Mise",
  title: "Mise: Caribbean meal kits, prepped and ready to cook",
  description:
    "Mise delivers fresh Caribbean meal kits in Trinidad & Tobago: local ingredients, seasoned, chopped and portioned in our kitchen, ready for you to cook. " + (env.siteMode === "launch" ? "Browse the meal kits and choose a plan." : "Join the waitlist."),
  locale: "en_TT",
  contactEmail: "hello@mise.tt",
  /** Shown in the footer and in structured data. */
  origin: "Trinidad & Tobago",
} as const;

const launch = env.siteMode === "launch";

/** In launch mode meal kits and plans live in the customer app (one catalogue, one checkout). */
export const navigation = launch
  ? [
      { label: "Meal kits", href: `${env.appUrl}/meals` },
      { label: "Plans", href: `${env.appUrl}/plans` },
      { label: "How it works", href: "/how-it-works" },
      { label: "About", href: "/about" },
      { label: "FAQ", href: "/faq" },
    ]
  : [
      { label: "How it works", href: "/how-it-works" },
      { label: "Meals", href: "/#meals" },
      { label: "Our story", href: "/#our-story" },
      { label: "Sustainability", href: "/#sustainability" },
      { label: "FAQ", href: "/faq" },
    ];

export const primaryCta = launch
  ? ({ label: "Browse meal kits", href: `${env.appUrl}/meals` } as const)
  : ({ label: "Join the waitlist", href: "/#waitlist" } as const);

/** Sign in, shown beside the primary call to action once ordering is open. */
export const signIn = launch ? { label: "Sign in", href: `${env.appUrl}/sign-in` } : null;

export const footer = {
  line: "Caribbean meal kits, prepped in Trinidad & Tobago.",
  photoNote: "Photographs on this site are stock images until Mise's own are ready.",
  legal: [
    { label: "Privacy", href: "/privacy" },
    { label: "Terms", href: "/terms" },
    { label: "Contact", href: "/contact" },
    { label: "Help centre", href: `${env.appUrl}/help` },
    { label: "Photo credits", href: "/credits" },
  ],
} as const;
