// Dev helper: screenshots a page at a given viewport.
// node tools/shot.mjs <url> <out.png> [width] [height] [full|view] [scrollY] [reduce]
import { chromium } from "@playwright/test";
const [url, out, w = "1440", h = "900", mode = "view", scrollY = "0", motion = "no-preference"] = process.argv.slice(2);
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: +w, height: +h }, reducedMotion: motion === "reduce" ? "reduce" : "no-preference", deviceScaleFactor: 1 });
const page = await ctx.newPage();
const errors = [];
page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
await page.goto(url, { waitUntil: "networkidle" });
if (mode === "full") {
  // Scroll through so lazy images and reveals fire, then capture.
  const total = await page.evaluate(() => document.documentElement.scrollHeight);
  for (let y = 0; y < total; y += 500) { await page.evaluate((v) => window.scrollTo(0, v), y); await page.waitForTimeout(120); }
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForTimeout(1200);
  await page.screenshot({ path: out, fullPage: true });
} else {
  await page.evaluate((v) => window.scrollTo(0, v), +scrollY);
  await page.waitForTimeout(1800);
  await page.screenshot({ path: out });
}
console.log(out, errors.length ? "\nCONSOLE ERRORS:\n" + errors.join("\n") : "(no console errors)");
await browser.close();
