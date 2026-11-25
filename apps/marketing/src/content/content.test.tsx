import { EVENT_TYPES } from "@mise/analytics";
import { render, screen } from "@testing-library/react";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

import type { MealCard } from "@mise/api-client";

import { MealPreview } from "@/components/meal-showcase";
import { launchFaq } from "@/content/home";
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

  it("keeps no sample meals or draft plans in the site's own content", () => {
    // Meals and plans come from the API. A copy here would drift from what customers can order.
    const dir = join(process.cwd(), "src/content");
    expect(readdirSync(dir).filter((name) => /^(meals|plans)\.ts$/.test(name))).toEqual([]);
  });

  it("states no price, delivery area or delivery time in the launch FAQ", () => {
    const text = launchFaq({ cutoffRule: "Choose by Thursday.", maxPauseWeeks: 4 }).map((item) => item.answer).join(" ");
    expect(text).not.toMatch(/TT\$|\$\d|\bwithin \d+ (hours|days)\b/i);
    expect(text).toContain("Choose by Thursday.");
    expect(text).toContain("up to 4 weeks");
  });

  it("keeps the analytics taxonomy in step with the API allow-list", () => {
    const source = readFileSync(join(process.cwd(), "../../services/api/internal/events/events.go"), "utf8");
    const allowed = [...source.matchAll(/^\t"([a-z_]+)":\s+true,$/gm)].map((match) => match[1]);
    expect([...EVENT_TYPES].sort()).toEqual(allowed.sort());
  });
});

describe("MealPreview", () => {
  const base: MealCard = {
    id: "00000000-0000-0000-0000-000000000001",
    slug: "pelau",
    name: "Pelau",
    summary: "Rice, pigeon peas and browned chicken.",
    category: { slug: "classics", name: "Caribbean classics" },
    chef: null,
    prep_minutes: 15,
    cook_minutes: 30,
    total_minutes: 45,
    servings: 2,
    protein: null,
    dietary: [],
    allergens: { status: "unconfirmed", contains: [], notice: "" },
    image: null,
    nutrition: null,
    is_demo: false,
  } as unknown as MealCard;

  it("shows no nutrition figures until they are verified", () => {
    render(<MealPreview meal={base} />);
    expect(screen.queryByText("Per serving")).not.toBeInTheDocument();
    expect(screen.getByText("45 minutes")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pelau" })).toHaveAttribute("href", "http://localhost:3102/meals/pelau");
  });

  it("shows calories and protein when the meal has them", () => {
    render(<MealPreview meal={{ ...base, nutrition: { calories: 520, protein_g: 34 } } as unknown as MealCard} />);
    expect(screen.getByText("520 kcal, 34 g protein")).toBeInTheDocument();
  });

  it("labels a demo meal", () => {
    render(<MealPreview meal={{ ...base, is_demo: true }} />);
    expect(screen.getByText("Demo meal")).toBeInTheDocument();
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
