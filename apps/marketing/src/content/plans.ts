/*
 * Subscription plan previews. Checkout does not exist yet, so these describe
 * what is planned and nothing more.
 *
 * `price` is null until pricing is decided. Set it here (for example
 * "From TT$… per week") and it appears on the site; never hard-code a price
 * in a component.
 */

export interface Plan {
  id: string;
  name: string;
  summary: string;
  audience: string;
  price: string | null;
}

export const plans: Plan[] = [
  {
    id: "basic",
    name: "Basic",
    summary: "Everyday Caribbean dinners from the weekly menu.",
    audience: "One or two people, or a household",
    price: null,
  },
  {
    id: "fitness",
    name: "Fitness",
    summary: "Meals planned with a nutritionist, with calories and protein on every card.",
    audience: "People training or eating to a plan",
    price: null,
  },
  {
    id: "school-lunch",
    name: "School Lunch",
    summary: "Lunch kits made to be packed the night before.",
    audience: "Parents and children",
    price: null,
  },
  {
    id: "premium-chef",
    name: "Premium Chef",
    summary: "Each month's guest chef recipes, with harder-to-find ingredients.",
    audience: "People who cook for the pleasure of it",
    price: null,
  },
  {
    id: "corporate-wellness",
    name: "Corporate Wellness",
    summary: "Meal kits for teams, delivered to the workplace.",
    audience: "Employers",
    price: null,
  },
];

export const plansSection = {
  id: "plans",
  title: "Five ways to get Mise",
  body: "Plans are still being finalised. Join the waitlist and you will see them, with prices, before anyone else.",
  pricePending: "Price announced before launch",
  cta: { label: "Get early access", href: "#waitlist" },
} as const;
