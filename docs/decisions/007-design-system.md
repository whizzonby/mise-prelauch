# ADR-007: The Mise design system

Status: accepted

## Decision

A token-based system in `packages/ui`, built on Tailwind CSS v4, with Radix primitives for
the two components whose accessibility is hard to get right by hand (the FAQ disclosure
and the mobile menu dialog). Full specification in `docs/architecture/design-system.md`.

## Choices worth recording

- **The palette is removed, not extended.** Tailwind's default colours, radii and shadows
  are reset in the theme, so a class like `bg-blue-500` or `rounded-2xl` does not exist.
  Staying on-brand is the path of least resistance.
- **No gradients, enforced.** A test scans the site and the design system and fails on any
  gradient.
- **Radix directly, not the shadcn/ui generator.** shadcn/ui is a way to copy pre-styled
  Radix components into a project. Mise needed two primitives and its own styling, so the
  generator and its default look were skipped.
- **CSS for animation; GSAP once; no Motion library.** The brief listed Motion for ordinary
  UI animation. Every such animation here (hero settle, image reveals, disclosure, menu) is
  expressible in CSS, which runs before hydration and ships no JavaScript. GSAP is used for
  the one scrubbed, pinned sequence and is lazy-loaded.
- **Semantic level and visual size are separate props** on `Heading`, so the document
  outline is never distorted to get a size.
- **One signature moment.** The hero is a literal mise en place that settles into position.
  Everything around it is quiet.

## Consequences

The system is small. New product surfaces (customer portal, ERP screens) will need more
components (tables, dialogs, toasts, date pickers); add them here, on the same tokens,
rather than in the apps.
