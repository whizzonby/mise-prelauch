// Sprint 9.2 screenshots: signs in to the admin once and captures the command
// center tabs and finance pages at desktop and phone widths.
// node tools/shots-9-2.mjs [outDir]   (run from e2e/, with the admin on :3101)
import { chromium } from "@playwright/test";
import { existsSync, mkdirSync } from "node:fs";
if (existsSync("../.env")) process.loadEnvFile("../.env");
const out = process.argv[2] ?? "../docs/screenshots/sprint-9-2";
mkdirSync(out, { recursive: true });
const base = "http://localhost:3101";
const pages = [
  ["overview", "/intelligence/command-center"],
  ["briefing", "/intelligence/command-center?tab=briefing"],
  ["finance", "/intelligence/command-center?tab=finance"],
  ["outlook", "/intelligence/command-center?tab=outlook"],
  ["customers", "/intelligence/command-center?tab=customers"],
  ["meals", "/intelligence/command-center?tab=meals"],
  ["operations", "/intelligence/command-center?tab=operations"],
  ["risks", "/intelligence/command-center?tab=risks"],
  ["opportunities", "/intelligence/command-center?tab=opportunities"],
  ["recommendations", "/intelligence/command-center?tab=recommendations"],
  ["ask", "/intelligence/command-center?tab=ask"],
  ["saved", "/intelligence/command-center?tab=saved"],
  ["history", "/intelligence/command-center?tab=history"],
  ["readiness", "/intelligence/command-center?tab=readiness"],
  ["settings", "/intelligence/command-center?tab=settings"],
  ["expenses", "/finance/expenses"],
  ["expenses-recurring", "/finance/expenses?tab=recurring"],
  ["expenses-coverage", "/finance/expenses?tab=coverage"],
  ["expenses-cash", "/finance/expenses?tab=cash"],
  ...(process.env.EXTRA_PATHS ?? "").split(",").filter(Boolean).map((p, i) => [`extra-${i + 1}`, p]),
];
const only = process.env.ONLY ? new Set(process.env.ONLY.split(",")) : null;
const browser = await chromium.launch();
const problems = [];
for (const [label, w, h] of [["desk", 1440, 900], ["phone", 390, 844]]) {
  const ctx = await browser.newContext({ viewport: { width: w, height: h } });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => problems.push(`${label}: page error ${e.message}`));
  await page.goto(base + "/login");
  await page.getByLabel("Email", { exact: true }).fill(process.env.E2E_ADMIN_EMAIL);
  await page.getByLabel("Password").fill(process.env.E2E_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForURL(base + "/");
  for (const [name, path] of pages) {
    if (only && !only.has(name)) continue;
    const res = await page.goto(base + path, { waitUntil: "networkidle" });
    const status = res?.status() ?? 0;
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
    if (status >= 400) problems.push(`${label} ${name}: HTTP ${status}`);
    if (overflow) problems.push(`${label} ${name}: horizontal page scroll`);
    const heading = await page.locator("h1").first().textContent().catch(() => "");
    if (/error|not found/i.test(heading ?? "")) problems.push(`${label} ${name}: heading "${heading}"`);
    await page.screenshot({ path: `${out}/${name}-${label}.png`, fullPage: true });
  }
  await ctx.close();
}
await browser.close();
console.log(problems.length ? "PROBLEMS\n" + problems.join("\n") : "all pages rendered without errors or horizontal scroll");
