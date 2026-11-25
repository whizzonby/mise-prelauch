// Quick visual check during development: node e2e/tools/look.mjs <url> <outPrefix> [fullPage]
import { chromium } from "@playwright/test";
const [url, out, full] = process.argv.slice(2);
const browser = await chromium.launch();
for (const [name, width, height] of [["desk", 1440, 900], ["phone", 390, 844]]) {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(url, { waitUntil: "networkidle" });
  await page.waitForTimeout(1200);
  await page.screenshot({ path: `${out}-${name}.png`, fullPage: Boolean(full) });
  await page.close();
}
await browser.close();
