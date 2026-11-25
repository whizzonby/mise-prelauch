/** Public configuration, inlined at build time. */
export const env = {
  siteUrl: (process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:3100").replace(/\/$/, ""),
  apiUrl: (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8090").replace(/\/$/, ""),
  /** The customer app: meal kits, plans, sign-in and checkout. */
  appUrl: (process.env.NEXT_PUBLIC_APP_URL ?? "http://localhost:3102").replace(/\/$/, ""),
  /**
   * "waitlist" (the default) keeps the pre-launch site: join the list, no
   * ordering. "launch" points every call to action at the meal kits and plans.
   * Switching is a go-live decision (docs/launch/GO_LIVE_CHECKLIST.md), so it
   * is a build setting, not something a visitor or a stray request can change.
   */
  siteMode: process.env.NEXT_PUBLIC_SITE_MODE === "launch" ? ("launch" as const) : ("waitlist" as const),
} as const;
