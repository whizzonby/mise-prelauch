import { urls } from "../playwright.config";

/** Fails fast, with a useful message, when part of the stack is not running. */
export default async function globalSetup() {
  const checks: [string, string][] = [
    ["API", `${urls.api}/api/v1/ready`],
    ["marketing site", `${urls.site}/robots.txt`],
    ["admin app", `${urls.admin}/login`],
    ["Mailpit", `${urls.mailpit}/api/v1/info`],
  ];
  const down: string[] = [];
  await Promise.all(
    checks.map(async ([name, url]) => {
      try {
        const res = await fetch(url, { signal: AbortSignal.timeout(30_000) });
        if (!res.ok) down.push(`${name} answered ${res.status} at ${url}`);
      } catch {
        down.push(`${name} is not reachable at ${url}`);
      }
    }),
  );
  if (down.length > 0) {
    throw new Error(`The end-to-end tests need the whole stack running:\n  - ${down.join("\n  - ")}\nSee "End-to-end tests" in the README.`);
  }
}
