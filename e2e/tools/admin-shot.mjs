// Dev helper: signs in to the admin and screenshots a page. node tools/admin-shot.mjs <path> <out.png> [width] [height]
import { chromium } from "@playwright/test";
import { existsSync } from "node:fs";
if (existsSync("../.env")) process.loadEnvFile("../.env");
const [path, out, w = "1440", h = "900"] = process.argv.slice(2);
const browser = await chromium.launch();
const page = await (await browser.newContext({ viewport: { width: +w, height: +h } })).newPage();
await page.goto("http://localhost:3101/login");
await page.getByLabel("Email", { exact: true }).fill(process.env.E2E_ADMIN_EMAIL);
await page.getByLabel("Password").fill(process.env.E2E_ADMIN_PASSWORD);
await page.getByRole("button", { name: "Sign in" }).click();
await page.waitForURL("http://localhost:3101/");
await page.goto("http://localhost:3101" + path, { waitUntil: "networkidle" });
await page.screenshot({ path: out, fullPage: true });
await browser.close();
