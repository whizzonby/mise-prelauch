/**
 * Motion tokens for JavaScript-driven animation. They mirror the CSS custom
 * properties in styles.css; change both together.
 */
export const duration = {
  /** Direct feedback: hover, press, focus. */
  fast: 0.15,
  /** State changes: open, close, swap. */
  standard: 0.3,
  /** Storytelling: image reveals, the hero settle. */
  editorial: 0.9,
} as const;

export const ease = {
  /** Entrances: quick start, long soft landing. */
  settle: [0.22, 1, 0.36, 1],
  /** Hover, press, toggle. */
  interaction: [0.4, 0, 0.2, 1],
} as const satisfies Record<string, readonly [number, number, number, number]>;
