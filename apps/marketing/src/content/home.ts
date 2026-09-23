import type { StaticImageData } from "next/image";

import chefDetail from "@/assets/images/chef-detail.jpg";
import chefMain from "@/assets/images/chef-main.jpg";
import coconut from "@/assets/images/ingredient-coconut.jpg";
import garlic from "@/assets/images/ingredient-garlic.jpg";
import lime from "@/assets/images/ingredient-lime.jpg";
import okra from "@/assets/images/ingredient-okra.jpg";
import pepper from "@/assets/images/ingredient-pepper.jpg";
import marketPeppers from "@/assets/images/market-peppers.jpg";
import marketStall from "@/assets/images/market-stall.jpg";
import spices from "@/assets/images/spices.jpg";
import storyDinner from "@/assets/images/story-dinner.jpg";
import storyFarm from "@/assets/images/story-farm.jpg";
import storyHome from "@/assets/images/story-home.jpg";
import storyKit from "@/assets/images/story-kit.jpg";
import storyKitchen from "@/assets/images/story-kitchen.jpg";

/*
 * PLACEHOLDER PHOTOGRAPHY. Every image in this file is a stock photograph
 * from Pexels (credits: docs/content/image-credits.md).
 * Replace them with Mise's own photography before launch; alt text describes
 * what the current image shows and must be rewritten with each replacement.
 */

export interface Photo {
  src: StaticImageData;
  alt: string;
}

export const hero = {
  /** One entry per line of the headline. */
  headline: ["Everything", "in its place."],
  body: "Mise is a Caribbean meal kit. We season, chop and portion fresh local ingredients in our Trinidad kitchen, then deliver them ready for you to cook.",
  primaryCta: "Join the waitlist",
  secondaryCta: { label: "See how it works", href: "#how-it-works" },
  note: "Launching in Trinidad & Tobago. People on the waitlist hear first.",
  /**
   * The counter: ingredients laid out the way a cook sets up before service.
   * `prep` is what the Mise kitchen has already done to it.
   */
  counter: [
    { id: "coconut", name: "Coconut", prep: "cracked", src: coconut, alt: "Two whole coconuts and one cracked open, showing pieces of white flesh" },
    { id: "lime", name: "Lime", prep: "halved", src: lime, alt: "A whole lime with lime halves and wedges on a white counter" },
    { id: "garlic", name: "Garlic", prep: "peeled", src: garlic, alt: "Garlic bulbs cut across, with loose peeled cloves" },
    { id: "ochro", name: "Ochro", prep: "sliced", src: okra, alt: "An ochro pod with five neat slices cut from it" },
    { id: "pepper", name: "Bird pepper", prep: "whole", src: pepper, alt: "Three small red hot peppers in a row" },
  ],
} as const;

export const story = {
  id: "our-story",
  title: "How a Mise dinner gets made",
  stages: [
    {
      id: "farm",
      name: "Farm",
      body: "It starts with growers in Trinidad & Tobago. Menus are planned around what local farms are harvesting, including produce that is good to eat but hard to sell.",
      src: storyFarm,
      alt: "Banana plants on a green hillside, with forest stretching to the horizon",
    },
    {
      id: "kitchen",
      name: "Mise kitchen",
      body: "Our cooks wash, peel, chop, season and portion. The slow part of dinner is finished before the kit leaves us.",
      src: storyKitchen,
      alt: "Hands mincing garlic on a wooden board beside red onions",
    },
    {
      id: "kit",
      name: "Your meal kit",
      body: "Everything the recipe needs, measured and labelled, with a recipe card and a QR code that opens a cook-along video.",
      src: storyKit,
      alt: "A box packed with squash, sweet potato, ginger, broccoli and tomatoes",
    },
    {
      id: "home",
      name: "Your kitchen",
      body: "You do the good part. Most recipes take one pot or one pan and are built for a weeknight.",
      src: storyHome,
      alt: "A hand stirring a pot of tomato stew with a spoon",
    },
    {
      id: "dinner",
      name: "Dinner",
      body: "A home-cooked Caribbean meal on the table, without the trip to the market or the pile of peelings.",
      src: storyDinner,
      alt: "A girl and her father serving themselves at a dinner table",
    },
  ],
} as const;

export const howItWorks = {
  id: "how-it-works",
  title: "Four steps from menu to plate",
  steps: [
    { title: "Choose", body: "Pick from a weekly menu of Caribbean dishes: family pots, lighter plates and a guest chef's recipe." },
    { title: "We prep", body: "We source the ingredients, then season, chop and portion them in our kitchen." },
    { title: "We deliver", body: "Your kit arrives chilled and organised by recipe, in packaging made to be reused." },
    { title: "You cook", body: "Follow the card or the video. Dinner is ready without the prep." },
  ],
} as const;

export const chefs = {
  id: "chefs",
  title: "A different Caribbean chef in your kitchen every month",
  body: "The Caribbean is not one cuisine. Each month a guest chef from a different island or tradition writes recipes for Mise and tells the story behind them: where the dish comes from, who taught it to them, and how they make it their own.",
  points: [
    { title: "Rotating recipes", body: "A new chef's dishes join the menu each month, alongside the Mise kitchen's own." },
    { title: "Regional flavours", body: "Trinidad curry, Tobago crab and dumpling, Jamaican jerk, Bajan cou-cou, Guyanese pepperpot." },
    { title: "Told by the cook", body: "Every chef recipe comes with its story and a video of the chef cooking it." },
  ],
  note: "The first guest chefs will be announced before launch.",
  photos: {
    main: { src: chefMain, alt: "A chef in a black jacket chopping fresh herbs on a wooden board" },
    detail: { src: chefDetail, alt: "A cook dropping chopped herbs onto a board in a dark kitchen" },
  },
} as const;

export const sustainability = {
  id: "sustainability",
  title: "Less waste, by design",
  body: "A meal kit only makes sense if nothing is thrown away. We plan to buy directly from local farmers, including surplus produce that would otherwise go unsold, portion exactly what each recipe needs, and deliver in packaging that can be reused.",
  /** A real sequence: each link feeds the next. */
  chain: [
    { title: "Local farmers", body: "Bought direct, close to where it is cooked." },
    { title: "Fresh and surplus produce", body: "Including good food that is hard to sell." },
    { title: "Mise kitchen", body: "Prepped and portioned to the recipe." },
    { title: "Meal kits", body: "Only what the dish needs." },
    { title: "Less waste", body: "On the farm, in our kitchen and in yours." },
  ],
  note: "This is how we intend to operate. Once we are delivering, we will publish what we actually achieve.",
  photos: {
    main: { src: marketStall, alt: "A market stall piled with bananas, cocoa pods, pineapples and limes" },
    detail: { src: marketPeppers, alt: "A basket heaped with red, yellow and green seasoning peppers" },
  },
} as const;

export const faq = {
  id: "faq",
  title: "Questions people ask",
  items: [
    {
      id: "launch-date",
      question: "When does Mise launch?",
      answer: "We are finishing the kitchen and the first menus. People on the waitlist will be the first to get a launch date and the first to be able to order.",
    },
    {
      id: "delivery-area",
      question: "Where will you deliver?",
      answer: "We will start in Trinidad and open area by area. The location you choose when you join helps us decide where to go first, and we will email you when your area opens.",
    },
    {
      id: "kit-contents",
      question: "What comes in a kit?",
      answer: "Pre-portioned ingredients for each recipe, already washed, chopped and seasoned where the dish calls for it, plus a recipe card with nutritional information and a QR code that opens a cooking video.",
    },
    {
      id: "pricing",
      question: "How much will it cost?",
      answer: "Prices will be announced before launch. Joining the waitlist is free and does not commit you to anything.",
    },
    {
      id: "diets",
      question: "Can you cater for my diet?",
      answer: "Tell us your dietary interests when you join; it shapes the first menus. Every recipe will list its ingredients and allergens. We cannot promise to cover every diet from day one.",
    },
    {
      id: "referrals",
      question: "How does the invitation link work?",
      answer: "After you join you get a personal link. When a friend signs up with it and confirms their email, it is counted on your Mise page. We may offer early-access perks to people who invite friends, but nothing is promised yet.",
    },
    {
      id: "privacy",
      question: "What do you do with my details?",
      answer: "We use them to tell you about the launch and to plan menus and delivery areas. We do not sell them. You can unsubscribe from any email, and ask us to delete your details at any time.",
    },
  ],
} as const;

export const waitlistSection = {
  id: "waitlist",
  title: "Save your place",
  body: "Join the list and we will email you when Mise opens in your area. It takes less than a minute.",
  assurances: ["Free, with nothing to pay or commit to", "First to hear the launch date", "Unsubscribe whenever you like"],
  photo: { src: spices, alt: "Small metal dishes of ground spices, salt and dried herbs" },
} as const;
