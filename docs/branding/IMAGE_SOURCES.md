# Image sources and licences

The single record of every photograph used on the Mise websites: where it came from, who made it, the licence, and where it is
used. `/credits` on the marketing site is generated from `apps/marketing/src/content/image-credits.ts`, which must match this
table.

## Rules

- **Every photograph today is stock**, from [Pexels](https://www.pexels.com), under the [Pexels License](https://www.pexels.com/license/)
  (read 25 November 2026): free to use and modify, including commercially and on websites; attribution not required (given anyway).
  Not allowed: selling unaltered copies, implying endorsement by people or brands shown, showing identifiable people in a bad light,
  redistributing on stock platforms, using a photo as a trademark.
- **Stock is never presented as Mise.** The hero photograph carries the caption "Serving suggestion. Stock photograph, not a Mise
  kit."; the supply-chain story says its photographs are stand-ins, not Mise's farms, kitchen or kits; the footer says the site's
  photographs are stock until Mise's own are ready.
- **No hot-linking.** Files are downloaded once, resized and re-encoded, and served from the site through `next/image`
  (AVIF/WebP, responsive sizes).
- **Replacing a photograph:** delete its row here and in `image-credits.ts`, delete the file, and rewrite its alt text. Real Mise
  photography (meal-kit, kitchen, team) needs its own source and licence record here (photographer, date, rights holder).

## The landing hero (Sprint 9.1)

| | |
|---|---|
| File | `apps/marketing/src/assets/images/hero-caribbean-plate.jpg` (2000 × 1335, JPEG q84, 449,604 bytes, SHA-256 prefix `4cdd2756ca5686b7`) |
| Original | https://www.pexels.com/photo/traditional-caribbean-cuisine-27556985/ (downloaded at 2400 × 1602 from `images.pexels.com`, 25 Nov 2026) |
| Photographer | Snappr (Pexels account; the name shown on Pexels should be re-checked on the photo page before launch) |
| Licence | Pexels License |
| Subject | Jerk chicken, rice and peas with coriander, fried plantain, on a white plate on dark wood. No people. |
| Why this one | One plated Caribbean meal, natural light, no faces or text; the strongest of the candidates reviewed (the oxtail alternative, Pexels 30746554, showed a person in the background). |
| Rendering | `next/image` with `preload` (the page's LCP element), blur placeholder, fixed aspect ratio (no layout shift), `sizes` 100vw on phones / 52vw / max 760 px. |

## All photographs

| File | Used in | Original | Photographer |
|---|---|---|---|
| `hero-caribbean-plate.jpg` | **Landing hero** (the one dominant photograph, Sprint 9.1) | [Traditional caribbean cuisine](https://www.pexels.com/photo/traditional-caribbean-cuisine-27556985/) | Snappr |
| `story-farm.jpg` | Supply-chain story: farm | [View of hills](https://www.pexels.com/photo/view-of-hills-17316626/) | a Pexels contributor |
| `story-kitchen.jpg` | Supply-chain story: Mise kitchen | [A person chopping garlic](https://www.pexels.com/photo/a-person-chopping-garlic-7601452/) | Los Muertos Crew |
| `story-kit.jpg` | Supply-chain story: meal kit | [Fresh fruits and vegetables in a box](https://www.pexels.com/photo/fresh-fruits-and-vegetables-in-a-box-7657072/) | CUP OF COUPLE |
| `story-home.jpg` | Supply-chain story: your kitchen | [Photograph of a red dish](https://www.pexels.com/photo/photograph-of-a-red-dish-4871236/) | Karolina Grabowska |
| `story-dinner.jpg` | Supply-chain story: dinner | [Crop black father serving food to daughter during christmas holiday](https://www.pexels.com/photo/crop-black-father-serving-food-to-daughter-during-christmas-holiday-5728168/) | Any Lane |
| `meal-curry-chicken.jpg` | Meals | [Authentic caribbean food](https://www.pexels.com/photo/authentic-caribbean-food-27568542/) | Snappr |
| `meal-jerk-chicken.jpg` | Meals | [Traditional caribbean cuisine](https://www.pexels.com/photo/traditional-caribbean-cuisine-27556985/) | Snappr |
| `meal-oxtail.jpg` | Meals | [Traditional caribbean cuisine](https://www.pexels.com/photo/traditional-caribbean-cuisine-27556975/) | Snappr |
| `meal-channa.jpg` | Meals | [Meal with rice on plate](https://www.pexels.com/photo/meal-with-rice-on-plate-8996219/) | I Own My Food Art |
| `meal-pumpkin-soup.jpg` | Meals | [Warm and hearty lentil soup in terra cotta bowl](https://www.pexels.com/photo/warm-and-hearty-lentil-soup-in-terra-cotta-bowl-29850843/) | Snappr |
| `meal-plantain.jpg` | Meals | [Traditional caribbean cuisine](https://www.pexels.com/photo/traditional-caribbean-cuisine-27556971/) | a Pexels contributor |
| `chef-main.jpg` | Chef story (waitlist mode only) | [Chef chopping fresh herbs on wooden board](https://www.pexels.com/photo/chef-chopping-fresh-herbs-on-wooden-board-29145745/) | EDUARDO FIN FOTOGRAFIA |
| `chef-detail.jpg` | Chef story (waitlist mode only) | [A person in white uniform holding green vegetables](https://www.pexels.com/photo/a-person-in-white-uniform-holding-green-vegetables-8629083/) | Kampus Production |
| `market-stall.jpg` | Sustainability | [Colorful tropical fruit market display](https://www.pexels.com/photo/colorful-tropical-fruit-market-display-30521864/) | a Pexels contributor |
| `market-peppers.jpg` | Sustainability | [Vibrant habanero peppers in market display](https://www.pexels.com/photo/vibrant-habanero-peppers-in-market-display-37649821/) | Ana Gonzalez |
| `spices.jpg` | Waitlist | [Assorted spices and grains in metal bowls](https://www.pexels.com/photo/assorted-spices-and-grains-in-metal-bowls-36884865/) | Muhammed Salih Karabağ |

Files live in `apps/marketing/src/assets/images/`. The five ingredient photographs used by the old hero (coconut, lime, garlic,
okra, pepper) were removed in Sprint 9.1 together with that composition.

Meal photographs in the customer app's demo catalogue are part of the demo data (`misectl catalog seed-demo`) and are never shown
in production (`CATALOG_DEMO_ENABLED=false`); real meal photography is a launch gate (`meal_photography`).
