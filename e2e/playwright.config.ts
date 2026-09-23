import { existsSync } from "node:fs";
import { join } from "node:path";

import { defineConfig, devices } from "@playwright/test";

// The tests run against an already-running stack (see README: "End-to-end
// tests"). Locally that is the dev servers; in CI it is `docker compose up`.
const rootEnv = join(import.meta.dirname, "..", ".env");
if (existsSync(rootEnv)) process.loadEnvFile(rootEnv);

export const urls = {
  site: process.env.E2E_SITE_URL ?? "http://localhost:3100",
  admin: process.env.E2E_ADMIN_URL ?? "http://localhost:3101",
  api: process.env.E2E_API_URL ?? "http://localhost:8090",
  mailpit: process.env.MAILPIT_URL ?? "http://localhost:8026",
};

export default defineConfig({
  testDir: "./tests",
  globalSetup: "./tests/global-setup.ts",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : [["list"]],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: urls.site,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1366, height: 850 } } }],
});
