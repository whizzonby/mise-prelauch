# Accessibility and responsive results (Sprint 9)

Target: WCAG 2.2 AA for customer-facing workflows (brief §24). This page records what was **tested**. It is not a compliance claim:
automated scans find only part of WCAG failures, and no screen-reader or real-device audit has been done.

Run on 25 November 2026 against local production builds (`next build` + `next start`) of the marketing site, customer app and admin,
with the Go API and worker in Docker (`-p mise-s1`), Chromium from Playwright 1.63, `@axe-core/playwright` 4.13.

## Automated checks (axe-core)

Tags `wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa` unless noted. Every listed check reports **0 violations**.

| Area | Pages | Spec |
|---|---|---|
| Public storefront | `/meals`, a meal page, `/plans`, a plan page (customer app); `/`, `/how-it-works`, `/faq`, `/about`, `/contact` (marketing) | `launch.spec.ts` |
| Marketing and invitation | home, `/join/…`, `/privacy`, admin sign-in | `experience.spec.ts` |
| Account | sign-in, register, password reset, email confirmation pages | `accounts.spec.ts` |
| Customer area | onboarding, dashboard, meals, favourites, profile and its five sub-pages | `customer-platform.spec.ts` |
| Commerce | plans, plan page, subscription, billing, orders, the test provider's checkout page, meal selection at 390 px (`wcag2a/2aa/21aa`) | `subscription-commerce.spec.ts` |
| Checkout start, subscription management, dashboard (after subscribing) | `/subscribe?plan=…`, `/subscription/manage`, `/dashboard` | `responsive.spec.ts` (new) |
| Admin (staff, `wcag2a/2aa`) | CRM, production, supply chain, delivery, intelligence, launch and system pages | sprint specs |

## Keyboard

- **Checkout by keyboard alone** (new, `responsive.spec.ts`): from `/plans`, Tab-only focus to *Choose Basic* → *Review and pay* →
  *Continue to payment* → *Approve payment*, each activated with Enter, ending at "You're subscribed." Each focused control is
  checked for a visible focus indicator (outline or ring). **Pass.**
- Catalogue by keyboard alone (`launch.spec.ts`), waitlist by keyboard alone (`experience.spec.ts`), mobile menu focus move and
  Escape returning focus to the opener (`experience.spec.ts`). **Pass.**

## Reduced motion

- Marketing home under `prefers-reduced-motion: reduce`: hero settled, story not pinned, no masked images (`experience.spec.ts`). **Pass.**
- Customer `/meals`, `/plans` and marketing home: no infinitely running animations with reduced motion (`responsive.spec.ts`). **Pass.**

## Responsive widths

Horizontal page overflow (`scrollWidth − clientWidth`) measured at 320, 375, 390, 430, 768, 1024 and 1440 px:

| Pages | Result |
|---|---|
| Public storefront and marketing pages (9 pages × 7 widths, `launch.spec.ts`) | Pass |
| Checkout: plan step, review, test provider page; then meal selection, dashboard, subscription, manage, billing, orders, favourites, profile + 4 sub-pages (15 pages × 7 widths, `responsive.spec.ts`) | Pass after fix 1 |
| Admin: every static console route (≈90) at 390 and 768 px (one-off sweep); 16 representative routes at 390/768/1024 kept in `responsive.spec.ts` | Pass after fixes 2–3 |

### Defects found and fixed in Stage 5

1. **Customer profile pages scrolled sideways on phones and tablets** (up to 745 px at 320 px). The account layout's grid track sized
   to the horizontally scrolling section navigation. Fixed with `minmax(0, 1fr)` tracks in `apps/web/src/app/(app)/profile/layout.tsx`.
2. **Admin dashboards (`/intelligence/*`) scrolled sideways at 390 px** (314 px): a 40 rem data table widened the grid instead of
   scrolling in its frame. Fixed in `apps/admin/src/components/intelligence-ui.tsx`.
3. **Admin `/exports` at 390 px**: same cause; fixed in `apps/admin/src/app/(console)/exports/page.tsx`.

## Not verified (gaps)

- Screen-reader testing (NVDA, VoiceOver, TalkBack): **not done**.
- Real mobile devices and real Safari: **not done** (see cross-browser results in the Sprint 9 report).
- Manual colour-contrast review beyond axe's computed checks, 200% zoom/reflow, and text-spacing overrides: **not done**.
- Admin pages were checked against WCAG A/AA tags only; the admin is a staff tool and was not held to the full 2.2 tag set.
- Detail pages (`/…/[id]`) in the admin were not part of the width sweep.
