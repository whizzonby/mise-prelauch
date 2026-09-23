# Mise design system

The system lives in `packages/ui`. Tokens are in `src/styles.css`; components are in
`src/*.tsx`. Both apps import the same package, so the marketing site and the admin are
visibly the same brand at different densities.

## What Mise looks like, and what it never looks like

Mise should read as a premium Caribbean food brand: a cookbook or a food magazine, not a
software company. Photography and type do the work.

Never: gradients, glass or blur, glow, neon, floating shapes, rows of identical rounded
cards, pill-shaped everything, decoration that moves for its own sake. The default
Tailwind palette, radii and shadows are removed from the theme so none of that is
reachable by accident, and a test fails the build if a gradient appears anywhere in the
site or the design system.

## Colour

Named for what is on a Trinidadian kitchen counter, exposed as semantic tokens.

| Token | Value | Named for | Use |
|---|---|---|---|
| `background` | `#f3ecdd` | cassava | The page. |
| `surface` | `#fcfaf4` | coconut | Forms, panels, anything that sits on the page. |
| `foreground` | `#22211d` | coal-pot charcoal | Text. The footer ground. |
| `muted` | `#5f5b50` | | Secondary text. 5.7:1 on `background`. |
| `primary` | `#1e3a2b` | dasheen leaf | Brand green: headlines, primary buttons, dark sections. |
| `secondary` | `#4a6a28` | chadon beni | Supporting green. |
| `accent` | `#b4411c` | roucou (annatto) | Used sparingly: step numerals, the wordmark's full stop, markers. |
| `border` | `#d9cfba` | | Hairlines and dividers. |
| `border-strong` | `#8a8372` | | Form control outlines. 3:1 on `background`. |
| `success` / `warning` / `error` | `#2f6b3f` / `#8a5a00` / `#9b1c2e` | | State only. `error` is a sorrel red, kept apart from the accent. |

Light mode only for now. Sections with a dark ground carry the class `on-dark`, which
flips button, muted-text and focus-ring colours so contrast holds.

## Type

- **Young Serif** for display and headings. One weight; hierarchy comes from size and space.
- **Instrument Sans** for everything else.
- Sentence case throughout. No all-caps labels.

| Class | Use | Size |
|---|---|---|
| `type-display` | The hero headline | 3.25rem → 9.5rem, fluid |
| `type-h1` | Page titles, story stage names | 2.75rem → 5.75rem |
| `type-h2` | Section titles | 2.125rem → 3.75rem |
| `type-h3` | Meals, plans, steps | 1.5rem → 2.125rem |
| `type-h4` | Small headings (sans, semibold) | 1.1875rem |
| `type-body-lg` | Lead paragraphs | 1.125rem → 1.375rem |
| `type-body` | Body | 1.0625rem |
| `type-body-sm` | Supporting text, form options | 0.9375rem |
| `type-label` | Form labels | 0.9375rem semibold |
| `type-caption` | Captions, hints | 0.8125rem |
| `type-figures` | Tabular numerals for times and counts | modifier |

`Heading` takes `as` (the semantic level, chosen for the document outline) and `size`
(the look) separately, so the outline is never bent to get a size.

## Shape, borders, elevation

- Radii: `sm` 4px (tags, checkboxes), `md` 8px (buttons, form controls), `lg` 12px
  (photographs), `xl` 20px (a frame around a photograph). Softened, never pill-shaped.
  Changed after the 1 October 2026 review, where the team asked for rounded images and
  buttons; the brand toolkit may tune these four values, and nothing else needs to change.
- Borders: 1px hairlines. A section's top rule is `foreground`; internal dividers are `border`.
- Shadow: one token, `shadow-overlay`, for things that float (the chart tooltip). Nothing else casts a shadow.
- Spacing: Tailwind's 0.25rem scale. Sections use fluid vertical padding (`Section`), the page gutter is `--page-gutter`.
- Breakpoints: Tailwind defaults plus `3xl` (120rem) for large desktop.

## Motion

Tokens (CSS custom properties, mirrored in `motion.ts` for JavaScript):

| Token | Value | Use |
|---|---|---|
| `--duration-fast` | 150ms | Direct feedback: hover, press, focus. |
| `--duration-standard` | 300ms | State changes: open, close. |
| `--duration-editorial` | 900ms | Storytelling: image reveals, the hero settle. |
| `ease-settle` | `cubic-bezier(0.22, 1, 0.36, 1)` | Entrances. |
| `ease-interaction` | `cubic-bezier(0.4, 0, 0.2, 1)` | Hover, press, toggle. |

Where motion is used, and why:

1. **The hero counter** (`apps/marketing`, CSS). Ingredient tiles start a few pixels out
   of true and settle into the grid; the headline lines rise. This is the one orchestrated
   moment, and it is the brand idea, *everything in its place*, shown rather than said.
   It is CSS, so it runs at first paint without waiting for JavaScript.
2. **The supply-chain story** (GSAP ScrollTrigger). On wide screens the section pins and
   the scroll position uncovers each photograph over the last. GSAP is used here, and only
   here, because the sequence needs a scrubbed timeline on a pinned element. It is loaded
   when the section approaches the viewport.
3. **`ImageReveal`**. A photograph below the fold is uncovered by a moving mask the first
   time it scrolls into view. Text never animates in.
4. **Responses to an action**: the FAQ disclosure, the mobile menu, button and field states.

**Reduced motion.** With `prefers-reduced-motion: reduce` the hero renders settled, the
story is an ordinary vertical list, images are simply present, and the FAQ opens without
animating. Phones get the unpinned story layout by design, not as a fallback.
Images are visible in the server-rendered HTML; masks are applied only after hydration
and only to images still below the fold, so nothing is hidden without JavaScript.

The Motion library was in the original stack list. It is not installed: every animation
it would have handled is done in CSS, which is lighter and has no hydration cost.

## Components

Primitives in `packages/ui` (no data fetching, no copy):

| Component | Purpose |
|---|---|
| `Container`, `Section` | Page grid width, vertical rhythm, section tone (`background`, `surface`, `primary`). |
| `Heading`, `Text` | The type scale, with semantic level separate from size. |
| `MiseButton`, `buttonClasses` | `primary`, `secondary`, `quiet`. Renders a link when given `href`. |
| `MediaFrame` | Fixed-ratio, square-cornered frame for a photograph, with optional caption. |
| `Field`, `Input`, `Select`, `Checkbox`, `Choice`, `ChoiceGroup`, `FieldError` | Form controls wired for labels, hints and errors. |
| `FAQList`, `FAQItem` | Accessible disclosure (Radix Accordion). |
| `ProcessStep` | One step of a real sequence. |
| `ImageReveal` | The scroll reveal described above. |
| `Tag`, `Wordmark` | Status marker; the logotype. |

Feature components in `apps/marketing/src/components` compose them with content:
`Hero`, `SupplyStory`, `HowItWorks`, `MealShowcase` / `MealPreview`, `Plans`,
`ChefFeature`, `IngredientStory`, `FAQ`, `WaitlistForm`, `ReferralPanel`, `PreferencesForm`.

## Accessibility baseline

WCAG 2.2 AA is the target. Built in: visible focus rings on every interactive element
(flipped on dark sections), a skip link, real form controls under every custom-looking
one, labels and `aria-describedby` wiring for hints and errors, errors announced with
`role="alert"`, 44px minimum touch targets, and colour never used as the only signal.
Automated axe scans run in the end-to-end suite; they do not replace a manual check with
a screen reader (see `docs/status.md`).

## Charts (admin)

Single-series only, so one colour (`primary`) and no legend. Thin bars with a rounded data
end, hairline grid, values in text colours rather than the mark colour, a hover tooltip on
the column chart and a "Show as a table" view under it. Breakdown lists are real tables.
