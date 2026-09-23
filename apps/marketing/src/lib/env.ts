/** Public configuration, inlined at build time. */
export const env = {
  siteUrl: (process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:3100").replace(/\/$/, ""),
  apiUrl: (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8090").replace(/\/$/, ""),
} as const;
