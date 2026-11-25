import type { MetadataRoute } from "next";

const appUrl = (process.env.CUSTOMER_APP_URL ?? "http://localhost:3102").replace(/\/$/, "");

/** Only the public catalogue is crawlable; accounts, checkout and sign-in are not. */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: [{ userAgent: "*", allow: ["/meals", "/chefs", "/plans"], disallow: ["/"] }],
    sitemap: `${appUrl}/sitemap.xml`,
  };
}
