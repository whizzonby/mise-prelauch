/*
 * DRAFT LEGAL TEXT. These documents describe what the platform actually does
 * with data today, but they have not been reviewed by a lawyer. Have them
 * reviewed against the Trinidad & Tobago Data Protection Act before launch,
 * and replace the placeholder company details.
 */

export interface LegalSection {
  heading: string;
  paragraphs: string[];
}

export interface LegalDocument {
  title: string;
  updated: string;
  intro: string;
  sections: LegalSection[];
}

export const privacy: LegalDocument = {
  title: "Privacy policy",
  updated: "September 2026",
  intro:
    "This policy explains what Mise collects when you join the waitlist, why, and what you can do about it. It is written to be read.",
  sections: [
    {
      heading: "What we collect",
      paragraphs: [
        "When you join the waitlist: your first name, your email address, the area where you would like delivery, and the date you agreed to hear from us. If you choose to give them: your phone number, household size, dietary interests and your answers to the optional questions about how you cook.",
        "How you found us: the campaign details in the link you followed (for example utm_source), the page you landed on, the site that referred you, and the invitation code if a friend invited you.",
        "How the site is used: a small set of events such as a page being viewed or a question being opened, tied to a random identifier stored in your browser. These events never contain what you type into a form. If your browser sends Global Privacy Control or Do Not Track, we do not record them at all.",
      ],
    },
    {
      heading: "What we do not collect",
      paragraphs: [
        "We do not use advertising trackers or third-party analytics, and we set no cookies. We do not store your IP address; we keep only a one-way keyed hash of it for a signup, which lets us spot abuse of invitation links without knowing the address.",
      ],
    },
    {
      heading: "Why we collect it",
      paragraphs: [
        "To tell you when Mise launches in your area, to plan menus, plans and delivery areas around the people on the list, to run the invitation programme fairly, and to understand which of our own efforts bring people to the site.",
        "We send marketing email only because you ticked the box asking us to. That consent, and the date you gave it, is recorded.",
      ],
    },
    {
      heading: "Who sees it",
      paragraphs: [
        "The Mise team, and the companies that host our systems and deliver our email on our behalf. We do not sell or rent your details to anyone.",
        "A friend who invited you can see that someone joined with their link, and how many. They cannot see who.",
      ],
    },
    {
      heading: "How long we keep it",
      paragraphs: [
        "Until you ask us to delete it, or until two years after the waitlist closes, whichever comes first. If you become a Mise customer, your waitlist details become part of your customer account and that account's own policy applies.",
      ],
    },
    {
      heading: "Your choices",
      paragraphs: [
        "Unsubscribe: every email has an unsubscribe link, and it works immediately.",
        "See or delete your details: write to hello@mise.tt from the address you joined with. We will send you a copy of what we hold, or delete it, within 30 days.",
        "Correct your details: reply to any Mise email and tell us what to change.",
      ],
    },
    {
      heading: "Contact",
      paragraphs: ["Questions about this policy: hello@mise.tt."],
    },
  ],
};

export const terms: LegalDocument = {
  title: "Waitlist terms",
  updated: "September 2026",
  intro: "These terms cover the Mise waitlist and invitation links. Terms for ordering will be published before Mise opens for orders.",
  sections: [
    {
      heading: "What joining means",
      paragraphs: [
        "Joining the waitlist is free. It does not commit you to buy anything, and it does not commit Mise to a launch date, a delivery area, a menu or a price. Everything described on this site about meals and plans is a preview and may change.",
      ],
    },
    {
      heading: "Invitation links",
      paragraphs: [
        "Your invitation link is for sharing with people you know. A friend counts when they join with your link and confirm their own email address.",
        "Invitations do not count if they are made with addresses you control, with automated signups, or by other means meant to inflate the number. We may remove such signups and the accounts behind them.",
        "No reward is promised for inviting friends. If we introduce one, its terms will be published before it applies.",
      ],
    },
    {
      heading: "Your details",
      paragraphs: ["How we handle your details is set out in the privacy policy."],
    },
    {
      heading: "Changes",
      paragraphs: ["If these terms change in a way that matters, we will email everyone on the list before the change takes effect."],
    },
  ],
};
