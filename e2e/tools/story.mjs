import { chromium } from "@playwright/test";
const out = process.argv[2];
const browser = await chromium.launch();
const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
const errors = [];
page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
await page.goto("http://localhost:3100/", { waitUntil: "networkidle" });
// Approach the section the way a person does, so the lazy GSAP import fires.
await page.mouse.wheel(0, 500); await page.waitForTimeout(1500);
const top = await page.evaluate(() => document.querySelector("[data-story]").getBoundingClientRect().top + window.scrollY);
const enhanced = await page.evaluate(() => document.querySelector("[data-story]").dataset.enhanced);
console.log("story top", top, "enhanced", enhanced);
let i = 0;
for (const offset of [0, 600, 1300, 2100, 3100]) {
  await page.evaluate((y) => window.scrollTo(0, y), top + offset);
  await page.waitForTimeout(1600);
  await page.screenshot({ path: `${out}/story-${i++}.jpg`, type: "jpeg", quality: 70 });
}
const after = await page.evaluate(() => ({ h: document.documentElement.scrollHeight, overflowX: document.documentElement.scrollWidth > window.innerWidth }));
console.log(after, errors.length ? errors.join("\n") : "no console errors");
await browser.close();
