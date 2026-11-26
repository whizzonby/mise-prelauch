import AxeBuilder from "@axe-core/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";

import { urls } from "../playwright.config";
import { linkIn, signInToAdmin, uniqueEmail, waitForEmail } from "./helpers";

// Sprint 9.1: Mise Chat (guest and customer widget, live delivery, agent
// inbox, internal notes) and the help centre. Needs the stack running and
// E2E_ADMIN_EMAIL / E2E_ADMIN_PASSWORD for a staff account holding the
// support_chat and help_centre permissions (the admin role has them).

const password = "plantain and saltfish 2026";
const stamp = () => Math.random().toString(36).slice(2, 9);

async function staffToken(request: APIRequestContext): Promise<string> {
  const res = await request.post(`${urls.api}/api/v1/admin/auth/login`, {
    data: { email: process.env.E2E_ADMIN_EMAIL, password: process.env.E2E_ADMIN_PASSWORD },
  });
  expect(res.status(), await res.text()).toBe(200);
  return (await res.json()).data.token;
}

async function agentPage(browser: Browser): Promise<Page> {
  const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
  await signInToAdmin(page);
  return page;
}

async function openWidget(page: Page) {
  await page.getByRole("button", { name: /Chat with Mise/ }).click();
  await expect(page.getByRole("dialog", { name: "Mise help" })).toBeVisible();
}

test("a guest chats from the marketing site and an agent replies live", async ({ page, browser }) => {
  const s = stamp();
  await page.goto(urls.site);
  await openWidget(page);
  await expect(page.getByText("We'll reply as soon as we can.")).toBeVisible();
  const axe = await new AxeBuilder({ page }).include('[role="dialog"]').analyze();
  expect(axe.violations.map((v) => v.id)).toEqual([]);

  // Everything inside the chat dialog (the waitlist form on the same page has its own Email field).
  const chat = page.getByRole("dialog", { name: "Mise help" });
  await chat.getByRole("button", { name: "Send us a message" }).click();
  // Required fields are checked by the API and shown next to the field.
  await chat.getByRole("button", { name: "Send", exact: true }).click();
  await expect(chat.getByText("Tell us your name.")).toBeVisible();
  await chat.getByLabel("Your name").fill(`Renee ${s}`);
  await chat.getByLabel(/^Email/).fill(uniqueEmail("chatguest"));
  await chat.getByLabel("Message").fill(`Do you deliver to San Fernando? ${s}`);
  await chat.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByText(`San Fernando? ${s}`)).toBeVisible();

  // The guest token is an httpOnly cookie: page script cannot read it.
  const cookie = (await page.context().cookies()).find((c) => c.name === "mise_chat_guest");
  expect(cookie?.httpOnly).toBe(true);
  expect(await page.evaluate(() => document.cookie)).not.toContain("mise_chat_guest");

  const agent = await agentPage(browser);
  await agent.goto(`${urls.admin}/support/chat?view=all&q=${s}`);
  await agent.getByRole("link", { name: new RegExp(`Renee ${s}`) }).click();
  await expect(agent.getByText(`San Fernando? ${s}`).first()).toBeVisible();
  // The conversation view opens its own live connection; wait for it (typing is never stored, so a
  // signal sent before the connection is open is simply not seen).
  await expect(agent).toHaveURL(/[?&]c=[0-9a-f-]{36}/);
  await expect(agent.getByText("Live", { exact: true })).toBeVisible();

  // Typing travels both ways without being stored.
  await page.getByLabel("Your message").fill("Also");
  await expect(agent.getByText("The customer is typing…")).toBeVisible();
  await page.getByLabel("Your message").fill("");

  await agent.getByLabel("Reply to the customer").fill(`Yes, every Saturday. ${s}`);
  await agent.getByRole("button", { name: "Send reply" }).click();
  // Live: no reload on the guest's side.
  await expect(page.getByText(`Yes, every Saturday. ${s}`)).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText("The Mise team is typing…")).toHaveCount(0);
  // Replying assigned the conversation to the agent; after the live refresh the form shows it
  // (a stale "Nobody" here would unassign the conversation if the agent pressed Transfer).
  await expect(agent.getByText(/^Assigned to /)).toBeVisible();
  await expect(agent.getByRole("combobox", { name: /^Agent/ })).not.toHaveValue("");
  // The guest has read it: the agent sees "Seen" after the inbox refreshes.
  await expect(agent.getByText("· Seen").first()).toBeVisible({ timeout: 10_000 });

  // An internal note never reaches the guest, live or on reload.
  await agent.getByRole("radio", { name: "Internal note" }).click();
  await agent.getByLabel(/Internal note/).fill(`NOTE ${s}`);
  await agent.getByRole("button", { name: "Add note" }).click();
  await expect(agent.getByText(`NOTE ${s}`).first()).toBeVisible();
  await page.waitForTimeout(2000);
  await expect(page.getByText(`NOTE ${s}`)).toHaveCount(0);
  await page.reload();
  await openWidget(page);
  await page.getByRole("button", { name: /Continue conversation/ }).click();
  await expect(page.getByText(`Yes, every Saturday. ${s}`)).toBeVisible();
  await expect(page.getByText(`NOTE ${s}`)).toHaveCount(0);

  // Resolve: the guest is offered a rating.
  await agent.getByRole("button", { name: "Resolve" }).click();
  await expect(page.getByText("How did we do? (optional)")).toBeVisible({ timeout: 10_000 });
  await page.getByRole("button", { name: "5 out of 5" }).click();
  await expect(page.getByText("Thanks for telling us.")).toBeVisible();
  await agent.context().close();
});

test("the chat widget works with the keyboard alone", async ({ page }) => {
  await page.goto(urls.site);
  const launcher = page.getByRole("button", { name: /Chat with Mise/ });
  await launcher.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Mise help" });
  await expect(dialog).toBeVisible();
  await expect(page.getByRole("heading", { name: "Mise help" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Close chat" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(launcher).toBeFocused();
});

async function newCustomer(page: Page, request: APIRequestContext) {
  const email = uniqueEmail("chatcust");
  const res = await request.post(`${urls.api}/api/v1/auth/register`, {
    data: { email, password, first_name: "Kavita", last_name: "Ramdass", accept_terms: true },
  });
  expect(res.status(), await res.text()).toBe(202);
  const body = await waitForEmail(request, email, "Confirm your email for your Mise account");
  const token = new URL(linkIn(body, "/verify-email")).searchParams.get("token");
  expect((await request.post(`${urls.api}/api/v1/auth/verify-email`, { data: { token } })).status()).toBe(200);
  await page.goto(`${urls.web}/sign-in`);
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/onboarding$/);
  return email;
}

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("a signed-in customer chats full screen, clear of the tab bar and of checkout", async ({ page, request }) => {
    const s = stamp();
    await newCustomer(page, request);
    await page.goto(`${urls.web}/dashboard`);
    const launcher = page.getByRole("button", { name: /Chat with Mise/ });
    const tabBar = page.getByRole("navigation", { name: "Main" }).last();
    const lb = (await launcher.boundingBox())!;
    const tb = (await tabBar.boundingBox())!;
    expect(lb.y + lb.height).toBeLessThanOrEqual(tb.y);

    await launcher.click();
    const dialog = page.getByRole("dialog", { name: "Mise help" });
    const db = (await dialog.boundingBox())!;
    expect(Math.round(db.width)).toBe(390);
    await page.getByRole("button", { name: "Send us a message" }).click();
    await expect(page.getByLabel("Your name")).toHaveCount(0); // signed in: no guest details asked
    await page.getByLabel("Message").fill(`Can I change my address? ${s}`);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(page.getByText(`change my address? ${s}`)).toBeVisible();
    await expect(page.getByText(/Delivered/)).toBeVisible();

    // An agent replies through the API; it arrives live.
    const staff = await staffToken(request);
    const inbox = await request.get(`${urls.api}/api/v1/admin/chat/conversations?view=all&q=${s}`, { headers: { Authorization: `Bearer ${staff}` } });
    const conv = (await inbox.json()).data.items[0];
    expect(conv.owner_kind).toBe("customer");
    const reply = await request.post(`${urls.api}/api/v1/admin/chat/conversations/${conv.id}/messages`, {
      headers: { Authorization: `Bearer ${staff}` },
      data: { body: `Yes, from Subscription, then Manage. ${s}` },
    });
    expect(reply.status()).toBe(201);
    await expect(page.getByText(`from Subscription, then Manage. ${s}`)).toBeVisible({ timeout: 10_000 });

    // The reply email's page shows the same conversation, only to its owner.
    await page.goto(`${urls.web}/support/chat/${conv.id}`);
    await expect(page.getByText(`from Subscription, then Manage. ${s}`)).toBeVisible();
    await expect(page.getByRole("button", { name: /Chat with Mise/ })).toHaveCount(0);

    // Not over payment steps.
    const plans = (await (await request.get(`${urls.api}/api/v1/plans`)).json()).data.items as { slug: string }[];
    await page.goto(`${urls.web}/subscribe?plan=${plans[0]!.slug}`);
    await expect(page).toHaveURL(/\/subscribe\?plan=/);
    await expect(page.getByRole("button", { name: /Chat with Mise/ })).toHaveCount(0);
  });
});

/** Not public: the page says so and tells search engines not to index it. (Streamed pages answer 200 with
 *  noindex, as the existing catalogue does; see the Sprint 9.1 report.) */
async function expectNotPublic(page: Page, url: string) {
  await page.goto(url);
  await expect(page.getByText("Page not found")).toBeVisible();
  expect(await page.locator('meta[name="robots"][content="noindex"]').count()).toBeGreaterThan(0);
}

test("help articles go from draft to review to published, and can be found", async ({ page, browser }) => {
  const s = stamp();
  const slug = `e2e-article-${s}`;
  await signInToAdmin(page);
  await page.goto(`${urls.admin}/help-centre/new`);
  await page.getByLabel("Title").fill(`Pausing your plan ${s}`);
  await page.getByLabel(/^Address/).fill(slug);
  await page.getByLabel(/^Summary/).fill("How to pause deliveries for a week or more.");
  await page.getByLabel(/^Article/).fill(`First paragraph ${s}.\n\nSecond paragraph.`);
  await page.getByRole("button", { name: "Create draft" }).click();
  await expect(page).toHaveURL(/\/help-centre\/[0-9a-f-]{36}$/);

  const visitor = await (await browser.newContext()).newPage();
  await expectNotPublic(visitor, `${urls.web}/help/${slug}`);

  await page.getByRole("button", { name: "Send for review" }).click();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByRole("button", { name: /Archive/ })).toBeVisible();

  await visitor.goto(`${urls.web}/help?q=${s}`);
  await visitor.getByRole("link", { name: `Pausing your plan ${s}` }).click();
  await expect(visitor.getByRole("heading", { level: 1, name: `Pausing your plan ${s}` })).toBeVisible();
  await expect(visitor.getByText(`First paragraph ${s}.`)).toBeVisible();

  // The widget's help search finds it too.
  await visitor.goto(urls.site);
  await openWidget(visitor);
  await visitor.getByRole("button", { name: "Help", exact: true }).click();
  await visitor.getByLabel("Search help articles").fill(s);
  await visitor.getByRole("button", { name: new RegExp(`Pausing your plan ${s}`) }).click();
  await expect(visitor.getByText("Second paragraph.")).toBeVisible();

  // Revising takes it off the site.
  await page.getByRole("button", { name: /Revise/ }).click();
  await expect(page.getByRole("button", { name: "Send for review" })).toBeVisible();
  await expectNotPublic(visitor, `${urls.web}/help/${slug}`);
  await visitor.context().close();
});
