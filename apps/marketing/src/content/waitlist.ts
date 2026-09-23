/*
 * Options offered by the waitlist and profiling forms. `value` is stored by
 * the API (lower-case letters, digits and hyphens only); `label` is what
 * people see. Adding an option here needs no API change.
 */

export interface Option {
  value: string;
  label: string;
}

export const locations: Option[] = [
  { value: "port-of-spain", label: "Port of Spain" },
  { value: "diego-martin-west", label: "Diego Martin and the West" },
  { value: "san-juan-east-west-corridor", label: "San Juan and the East-West Corridor" },
  { value: "arima-east", label: "Arima and the East" },
  { value: "chaguanas-central", label: "Chaguanas and Central" },
  { value: "san-fernando-south", label: "San Fernando and the South" },
  { value: "tobago", label: "Tobago" },
  { value: "elsewhere", label: "Somewhere else" },
];

export const dietaryInterests: Option[] = [
  { value: "vegetarian", label: "Vegetarian" },
  { value: "vegan", label: "Vegan" },
  { value: "pescatarian", label: "Pescatarian" },
  { value: "halal", label: "Halal" },
  { value: "no-pork", label: "No pork" },
  { value: "gluten-free", label: "Gluten-free" },
  { value: "dairy-free", label: "Dairy-free" },
  { value: "high-protein", label: "High protein" },
];

export const householdSizes: Option[] = [
  { value: "1", label: "Just me" },
  { value: "2", label: "2 people" },
  { value: "3", label: "3 people" },
  { value: "4", label: "4 people" },
  { value: "5", label: "5 people" },
  { value: "6", label: "6 or more" },
];

export const waitlistForm = {
  submit: "Join the waitlist",
  submitting: "Joining…",
  consent: "Email me about the Mise launch, menus and offers. I can unsubscribe at any time.",
  privacyLead: "We use your details to plan the launch and never sell them.",
  phoneHint: "Only if you would like a text when your area opens.",
  dietaryHint: "Choose any that apply. This shapes the first menus.",
  alreadyOnList: {
    title: "You are already on the list",
    body: "That email address has a place saved. We have sent it an email with your personal link.",
  },
  errors: {
    network: "We could not reach Mise. Check your connection and try again.",
    rateLimited: "Too many attempts from this connection. Wait a few minutes and try again.",
    generic: "Something went wrong on our side. Try again in a moment.",
  },
} as const;

/** Optional questions asked after signup. Every one can be skipped. */
export const profiling = {
  title: "What would make Mise perfect for you?",
  body: "Five quick questions. Answer any, skip any. It helps us plan the first menus around the people on the list.",
  submit: "Save my answers",
  skip: "Skip for now",
  saved: "Answers saved. Thank you.",
  questions: {
    householdType: {
      label: "Who are you cooking for?",
      options: [
        { value: "individual", label: "Myself" },
        { value: "couple", label: "Two of us" },
        { value: "family", label: "A family" },
        { value: "shared", label: "A shared house" },
      ],
    },
    mealsPerWeek: {
      label: "How many Mise dinners would you want each week?",
      options: [
        { value: "2", label: "2" },
        { value: "3", label: "3" },
        { value: "4", label: "4" },
        { value: "5", label: "5 or more" },
      ],
    },
    cookingFrequency: {
      label: "How often do you cook at home now?",
      options: [
        { value: "most-days", label: "Most days" },
        { value: "few-times-a-week", label: "A few times a week" },
        { value: "weekends", label: "Mostly weekends" },
        { value: "rarely", label: "Rarely" },
      ],
    },
    mealInterests: {
      label: "Which menus interest you?",
      options: [
        { value: "family", label: "Family" },
        { value: "fitness", label: "Fitness" },
        { value: "caribbean-classics", label: "Caribbean Classics" },
        { value: "chef-series", label: "Chef Series" },
        { value: "school-lunch", label: "School lunch" },
      ],
    },
    usage: {
      label: "What would you use Mise for most?",
      options: [
        { value: "weeknight-dinners", label: "Weeknight dinners" },
        { value: "weekend-cooking", label: "Weekend cooking" },
        { value: "healthy-eating", label: "Eating to a plan" },
        { value: "entertaining", label: "Having people over" },
      ],
    },
  },
} as const;

export const welcome = {
  verified: {
    title: "You're on the Mise list",
    body: "Your place is saved. We will email you when Mise opens in your area.",
  },
  pending: {
    title: "One step left",
    body: "We have sent you an email. Open it and confirm your address to take your place on the list.",
  },
  referral: {
    title: "Bring someone to the table",
    body: "Share your link. Each friend who joins and confirms their email is counted here.",
    copy: "Copy link",
    copied: "Link copied",
    shareText: "I've joined the waitlist for Mise, a Caribbean meal kit launching in Trinidad & Tobago. Join with my link:",
    perks: "We may offer early-access perks to people who invite friends. Nothing is promised yet.",
  },
  noSession: {
    title: "Open your Mise page from your email",
    body: "Your personal page opens from the link in the email we sent you. Not on the list yet?",
    cta: "Join the waitlist",
  },
} as const;
