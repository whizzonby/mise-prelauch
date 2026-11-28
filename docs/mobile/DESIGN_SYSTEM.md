# Mise app: design system

The app uses the web brand (`docs/architecture/design-system.md`) at phone density. Code: `apps/mobile/lib/design_system`.

## Tokens

| Token | Value | Use |
|---|---|---|
| background (cassava) | `#F3ECDD` | Screens |
| surface (coconut) | `#FCFAF4` | Panels, sheets, bars, fields |
| foreground (charcoal) | `#22211D` | Text |
| muted | `#5F5B50` | Secondary text (5.7:1 on background) |
| primary (dasheen) | `#1E3A2B` | Primary buttons, headlines, selected states |
| secondary (chadon beni) | `#4A6A28` | Supporting green |
| accent (roucou) | `#B4411C` | Sparingly: step numerals, the wordmark's full stop, unread dots |
| border / border-strong | `#D9CFBA` / `#8A8372` | Hairlines / control outlines (3:1) |
| success / warning / error | `#2F6B3F` / `#8A5A00` / `#9B1C2E` | State only |

Spacing 4/8/12/16/24/32/48/64, page gutter 20. Radii 4/8/12/20 (never pills). Motion 150/300/900 ms with the web's "settle" and
"interaction" curves. One shadow at most (none used); no gradients, glass, blur or glow. Light theme only.

**Type.** Young Serif (display, one weight) and Instrument Sans (variable; weight set through the `wght` axis), both SIL OFL, bundled
with their licences in `assets/fonts`. Sentence case. Tabular figures for prices, times and counts.

## Components

`MiseButton` (primary, secondary, quiet, danger; busy state keeps size and blocks double submits), `MiseTextField` (label above,
error below, password reveal, autofill hints), `MiseSearchField`, `MealCard` (photograph first, no box), `PlanCard`, `PriceText`,
`QuantitySelector` (announced value, increase/decrease actions), `FilterChips`, `showMiseSheet`, `confirmAction` (states the
consequence), `BottomActionBar`, `NavigationBar` theme, `MiseLoading`, `Skeleton` (stops under reduced motion), `EmptyState`,
`ErrorState` (retry, never internals), `NoticeBanner`, `MiseTag`, `CountBadge`, `MilestoneTimeline` (real milestones), `SelectionProgress`,
`ChatBubble` (pending, failed with retry, seen), `RecipeStep` (large numerals and text), `FactRow`, `SectionHeader` (hairline rule,
the web's editorial device), `Wordmark`, `MiseNetworkImage` (decoded at display size).

## Brand assets

The launcher icon ("M" with the roucou full stop on dasheen green), adaptive icon, splash wordmark and notification icon in
`assets/branding` were rendered from the wordmark and the bundled font. **They are proposals**: the brand toolkit has no app icon, so
the owner must approve them (or supply official ones) before any store submission. Onboarding photographs are the licensed Pexels
stock already used on the website (`docs/branding/IMAGE_SOURCES.md`), each captioned "Stock photograph, not a Mise kit."

## Accessibility built in

48 dp minimum touch targets; text scale honoured up to 200 %; screen-reader labels on cards, steps, milestones, prices, chat bubbles,
quantity controls and badges; headers marked; errors announced (live regions); reduced motion respected (onboarding, cooking steps,
skeletons); contrast pairs from the web palette (all body text ≥ 4.5:1).
