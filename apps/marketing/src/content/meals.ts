import type { StaticImageData } from "next/image";

import channa from "@/assets/images/meal-channa.jpg";
import curryChicken from "@/assets/images/meal-curry-chicken.jpg";
import jerkChicken from "@/assets/images/meal-jerk-chicken.jpg";
import oxtail from "@/assets/images/meal-oxtail.jpg";
import plantain from "@/assets/images/meal-plantain.jpg";
import pumpkinSoup from "@/assets/images/meal-pumpkin-soup.jpg";

/*
 * SAMPLE MENU. These dishes, times and photographs are placeholders that show
 * how the menu will be presented. The photographs are stock images from
 * Pexels, not Mise's cooking. Replace all of it with the real launch menu.
 *
 * `nutrition` is deliberately left out of every sample dish: calories and
 * protein are facts about a real recipe and must come from the nutritionist,
 * not from a placeholder. The component shows the figures as soon as they are
 * provided here.
 */

export const mealCategories = [
  { id: "family", label: "Family" },
  { id: "fitness", label: "Fitness" },
  { id: "caribbean-classics", label: "Caribbean Classics" },
  { id: "chef-series", label: "Chef Series" },
] as const;

export type MealCategoryId = (typeof mealCategories)[number]["id"];

export interface Meal {
  id: string;
  name: string;
  category: MealCategoryId;
  description: string;
  /** Who wrote the recipe. */
  chef: string;
  prepMinutes: number;
  /** Per serving. Omit until confirmed by the nutritionist. */
  nutrition?: { calories: number; proteinGrams: number };
  dietary: string[];
  image: { src: StaticImageData; alt: string };
}

export const meals: Meal[] = [
  {
    id: "curry-chicken",
    name: "Curry chicken and potato",
    category: "caribbean-classics",
    description: "Chicken and potato cooked down in green seasoning and Trinidad curry, made for rice or roti.",
    chef: "Mise kitchen",
    prepMinutes: 40,
    dietary: [],
    image: { src: curryChicken, alt: "Curry chicken with chunks of potato in a white bowl" },
  },
  {
    id: "jerk-chicken",
    name: "Jerk chicken, rice and peas",
    category: "family",
    description: "Chicken in a pimento and scotch bonnet rub, with coconut rice and peas and fried ripe plantain.",
    chef: "Mise kitchen",
    prepMinutes: 45,
    dietary: [],
    image: { src: jerkChicken, alt: "Jerk chicken with rice and peas and fried plantain on a white plate" },
  },
  {
    id: "stewed-oxtail",
    name: "Stewed oxtail with thyme",
    category: "chef-series",
    description: "Oxtail browned in burnt sugar and braised until it gives. A guest chef's Sunday dish.",
    chef: "Guest chef, to be announced",
    prepMinutes: 50,
    dietary: [],
    image: { src: oxtail, alt: "Dark, glossy stewed oxtail topped with a sprig of thyme" },
  },
  {
    id: "curried-channa",
    name: "Curried channa and rice",
    category: "fitness",
    description: "Chickpeas simmered with geera, garlic and tomato, served beside steamed rice.",
    chef: "Mise kitchen",
    prepMinutes: 25,
    dietary: ["Vegan"],
    image: { src: channa, alt: "A white plate, half steamed rice and half curried chickpeas" },
  },
  {
    id: "pumpkin-soup",
    name: "Pumpkin soup",
    category: "family",
    description: "Pumpkin and split peas blended smooth with coconut milk, ginger and a little pepper.",
    chef: "Mise kitchen",
    prepMinutes: 30,
    dietary: ["Vegetarian"],
    image: { src: pumpkinSoup, alt: "A terracotta pot of smooth yellow soup" },
  },
  {
    id: "fried-plantain",
    name: "Fried ripe plantain",
    category: "caribbean-classics",
    description: "Ripe plantain, sliced long and fried until the edges caramelise. The side that goes with everything.",
    chef: "Mise kitchen",
    prepMinutes: 15,
    dietary: ["Vegan"],
    image: { src: plantain, alt: "Slices of fried ripe plantain on a white plate" },
  },
];

export const mealsSection = {
  id: "meals",
  title: "A taste of the first menus",
  note: "These are sample dishes. The launch menu, with nutrition facts and allergens for every recipe, will be published before ordering opens.",
} as const;
