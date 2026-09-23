import { expect, type APIRequestContext, type Page } from "@playwright/test";

import { urls } from "../playwright.config";

/** A unique @example.com address, so runs never collide with each other. */
export function uniqueEmail(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}@example.com`;
}

/** Fills the required waitlist fields the way a person would. */
export async function fillWaitlist(page: Page, { name, email }: { name: string; email: string }) {
  const form = page.getByRole("form", { name: "Join the Mise waitlist" });
  await form.scrollIntoViewIfNeeded();
  await form.getByLabel("First name").fill(name);
  await form.getByLabel("Email", { exact: true }).fill(email);
  await form.getByLabel("Where would you like delivery?").selectOption("port-of-spain");
  // Dietary options are label-tags over visually hidden checkboxes: click the tag, as a person does.
  await form.getByText("Vegetarian", { exact: true }).click();
  await expect(form.getByLabel("Vegetarian")).toBeChecked();
  await form.getByRole("checkbox", { name: /Email me about the Mise launch/ }).check();
  // The API rejects forms submitted faster than a person can type (a bot check).
  await page.waitForTimeout(1700);
  return form;
}

export async function submitWaitlist(page: Page, details: { name: string; email: string }) {
  const form = await fillWaitlist(page, details);
  await form.getByRole("button", { name: "Join the waitlist" }).click();
}

interface MailpitMessage {
  ID: string;
  Subject: string;
}

/** Waits for an email to arrive in Mailpit and returns its plain-text body. */
export async function waitForEmail(request: APIRequestContext, to: string, subject: string): Promise<string> {
  let body = "";
  await expect(async () => {
    const res = await request.get(`${urls.mailpit}/api/v1/search`, { params: { query: `to:${to}` } });
    expect(res.ok()).toBe(true);
    const { messages } = (await res.json()) as { messages: MailpitMessage[] };
    const match = messages.find((message) => message.Subject === subject);
    expect(match, `an email "${subject}" to ${to}`).toBeTruthy();
    const detail = await request.get(`${urls.mailpit}/api/v1/message/${match!.ID}`);
    body = ((await detail.json()) as { Text: string }).Text;
  }).toPass({ timeout: 30_000, intervals: [500, 1000, 2000] });
  return body;
}

/** Pulls the first link containing `path` out of an email body. */
export function linkIn(body: string, path: string): string {
  const match = body.match(new RegExp(`https?://\\S*${path}\\S*`));
  if (!match) throw new Error(`No ${path} link in the email:\n${body}`);
  return match[0];
}

/** Opens the verification link from the email, as the person would. */
export async function verifyByEmail(page: Page, request: APIRequestContext, email: string) {
  const body = await waitForEmail(request, email, "Confirm your place on the Mise list");
  const link = new URL(linkIn(body, "/verify"));
  // Follow the link on the site under test, whatever origin the API was told to print.
  await page.goto(`${urls.site}${link.pathname}${link.search}`);
  await expect(page.getByRole("heading", { level: 1 })).toContainText("You're on the Mise list");
}

interface SignupResponse {
  data: { outcome: string; profile_token: string; lead: { referral_code: string; referral_url: string } };
}

/**
 * Creates a lead straight through the API from a given network address. The
 * referral test needs the referrer and the friend on different addresses,
 * because same-network invitations are flagged and not counted.
 */
export async function createLeadViaApi(request: APIRequestContext, { name, email, ip }: { name: string; email: string; ip: string }) {
  const res = await request.post(`${urls.api}/api/v1/leads`, {
    headers: { "X-Forwarded-For": ip },
    data: { first_name: name, email, location: "port-of-spain", dietary_interests: [], consent: true, website: "", elapsed_ms: 9000 },
  });
  expect(res.status(), await res.text()).toBe(201);
  return ((await res.json()) as SignupResponse).data;
}

export async function verifyViaApi(request: APIRequestContext, email: string) {
  const body = await waitForEmail(request, email, "Confirm your place on the Mise list");
  const token = new URL(linkIn(body, "/verify")).searchParams.get("token");
  const res = await request.post(`${urls.api}/api/v1/leads/verify`, { data: { token } });
  expect(res.status(), await res.text()).toBe(200);
}

export async function signInToAdmin(page: Page) {
  const email = process.env.E2E_ADMIN_EMAIL;
  const password = process.env.E2E_ADMIN_PASSWORD;
  if (!email || !password) throw new Error("Set E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD (see .env.example).");
  await page.goto(`${urls.admin}/login`);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard" })).toBeVisible();
}
