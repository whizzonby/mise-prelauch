import type { StaticImageData } from "next/image";

import callaloo from "@/assets/images/meal-callaloo.jpg";
import curriedEggs from "@/assets/images/meal-curried-eggs.jpg";
import stewChicken from "@/assets/images/meal-stew-chicken.jpg";
import stewedLentils from "@/assets/images/meal-stewed-lentils.jpg";
import sweetPotatoBake from "@/assets/images/meal-sweet-potato-bake.jpg";

/*
 * SAMPLE MENU. These dishes, times and photographs are placeholders that show
 * how the menu will be presented. Replace them with the real launch menu.
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
    id: "callaloo-shrimp",
    name: "Callaloo with shrimp",
    category: "caribbean-classics",
    description: "Dasheen leaves simmered with coconut milk, ochro and pimento, finished with shrimp.",
    chef: "Mise kitchen",
    prepMinutes: 30,
    dietary: ["Pescatarian"],
    image: { src: callaloo, alt: "A bowl of green callaloo topped with shrimp, on a wooden table beside fresh peppers" },
  },
  {
    id: "stew-chicken",
    name: "Stew chicken, rice and peas",
    category: "family",
    description: "Chicken browned in burnt sugar and green seasoning, with coconut rice and pigeon peas.",
    chef: "Mise kitchen",
    prepMinutes: 40,
    dietary: [],
    image: { src: stewChicken, alt: "Stewed chicken with rice and peas and a spoonful of slaw on a white plate" },
  },
  {
    id: "stewed-lentils",
    name: "Stewed lentils with pimento",
    category: "chef-series",
    description: "Slow lentils with pumpkin, thyme and seasoning peppers. A guest chef's version of a weekday staple.",
    chef: "Guest chef, to be announced",
    prepMinutes: 35,
    dietary: ["Vegan"],
    image: { src: stewedLentils, alt: "Two clay dishes of lentil stew, each topped with a green pepper, on rough cloth" },
  },
  {
    id: "curried-eggs",
    name: "Curried eggs",
    category: "fitness",
    description: "Boiled eggs in a tomato and curry sauce with chadon beni, built to go with rice or roti.",
    chef: "Mise kitchen",
    prepMinutes: 25,
    dietary: ["Vegetarian"],
    image: { src: curriedEggs, alt: "Three eggs in a red curry sauce scattered with herbs, in a yellow bowl" },
  },
  {
    id: "sweet-potato-bake",
    name: "Sweet potato bake",
    category: "family",
    description: "Thin-sliced sweet potato layered with coconut milk and nutmeg, baked until the top catches.",
    chef: "Mise kitchen",
    prepMinutes: 45,
    dietary: ["Vegetarian"],
    image: { src: sweetPotatoBake, alt: "A round pan of golden baked sliced potato with a knife resting on it" },
  },
];

export const mealsSection = {
  id: "meals",
  title: "A taste of the first menus",
  note: "These are sample dishes. The launch menu, with nutrition facts and allergens for every recipe, will be published before ordering opens.",
} as const;
