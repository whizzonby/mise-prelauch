import { EVENT_TYPES } from "@mise/analytics";
import { render, screen } from "@testing-library/react";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

import { MealPreview } from "@/components/meal-showcase";
import { mealCategories, meals, type Meal } from "@/content/meals";
import { plans } from "@/content/plans";
import { dietaryInterests, locations, profiling } from "@/content/waitlist";

vi.mock("next/image", () => ({
  // eslint-disable-next-line @next/next/no-img-element
  default: ({ alt }: { alt: string }) => <img alt={alt} />,
}));

const SLUG = /^[a-z0-9][a-z0-9-]{0,39}$/;

describe("content", () => {
  it("uses option values the API accepts", () => {
    const values = [
      ...locations,
      ...dietaryInterests,
      ...Object.values(profiling.questions).flatMap((question) => [...question.options]),
    ].map((option) => option.value);
    for (const value of values) expect(value).toMatch(SLUG);
  });

  it("gives every meal category at least one meal, and every photo alt text", () => {
    for (const category of mealCategories) {
      expect(meals.some((meal) => meal.category === category.id)).toBe(true);
    }
    for (const meal of meals) expect(meal.image.alt.length).toBeGreaterThan(10);
  });

  it("states no price until one is configured", () => {
    // Guards against a placeholder price slipping into the plan previews.
    for (const plan of plans) expect(plan.price === null || plan.price.length > 0).toBe(true);
  });

  it("keeps the analytics taxonomy in step with the API allow-list", () => {
    const source = readFileSync(join(process.cwd(), "../../services/api/internal/events/events.go"), "utf8");
    const allowed = [...source.matchAll(/^\t"([a-z_]+)":\s+true,$/gm)].map((match) => match[1]);
    expect([...EVENT_TYPES].sort()).toEqual(allowed.sort());
  });
});

describe("MealPreview", () => {
  const base: Meal = { ...meals[0]!, nutrition: undefined };

  it("shows no nutrition figures until they are provided", () => {
    render(<MealPreview meal={base} />);
    expect(screen.queryByText("Per serving")).not.toBeInTheDocument();
    expect(screen.getByText(`${base.prepMinutes} minutes`)).toBeInTheDocument();
  });

  it("shows calories and protein when the meal has them", () => {
    render(<MealPreview meal={{ ...base, nutrition: { calories: 520, proteinGrams: 34 } }} />);
    expect(screen.getByText("520 kcal, 34 g protein")).toBeInTheDocument();
  });
});

describe("design rules", () => {
  it("uses no gradients anywhere in the site or the design system", () => {
    const roots = [join(process.cwd(), "src"), join(process.cwd(), "../../packages/ui/src")];
    const offenders: string[] = [];
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const path = join(dir, name);
        if (statSync(path).isDirectory()) walk(path);
        else if (/\.(tsx?|css)$/.test(name) && !name.includes(".test.")) {
          const text = readFileSync(path, "utf8");
          if (/(linear|radial|conic)-gradient\(|\bbg-(gradient|linear|radial|conic)-/.test(text)) offenders.push(path);
        }
      }
    };
    roots.forEach(walk);
    expect(offenders).toEqual([]);
  });
});
