// Walks Mise Chat end to end in real browsers and saves screenshots:
// a guest on the marketing site writes; a support agent replies from the
// admin inbox; the reply arrives live in the guest's widget; a signed-in
// customer chats from the customer app on a phone-sized screen.
// Usage: node tools/chat-walkthrough.mjs <out-dir>  (apps on 3100-3102, API on 8090)
import { chromium } from "@playwright/test";

const out = process.argv[2];
const PW = process.env.REVIEW_PASSWORD ?? "MiseReview-2026!";
const stamp = Date.now().toString(36);
const browser = await chromium.launch();
const log = (...a) => console.log(...a);
const errors = [];
const watch = (page, who) => {
  page.on("pageerror", (e) => errors.push(`${who}: ${e.message}`));
  page.on("console", (m) => m.type() === "error" && !m.text().includes("favicon") && errors.push(`${who} console: ${m.text()}`));
};
const shot = (page, name) => page.screenshot({ path: `${out}/${name}.png` });

// ---------------------------------------------------------------- guest on the marketing site
const guest = await (await browser.newContext({ viewport: { width: 1280, height: 860 } })).newPage();
watch(guest, "guest");
await guest.goto("http://localhost:3100/", { waitUntil: "networkidle" });
await guest.getByRole("button", { name: /Chat with Mise/ }).click();
await guest.getByRole("dialog", { name: "Mise help" }).waitFor();
await shot(guest, "01-guest-home");
await guest.getByRole("button", { name: "Send us a message" }).click();
await guest.getByLabel("Your name").fill("Renee Guest");
await guest.getByLabel(/^Email/).fill(`renee.${stamp}@example.com`);
await guest.getByLabel("Message").fill(`Hi, do you deliver to San Fernando? (${stamp})`);
await guest.getByRole("button", { name: "Send", exact: true }).click();
await guest.getByText(`(${stamp})`).waitFor();
await guest.waitForTimeout(800);
await shot(guest, "02-guest-sent");
const cookies = await guest.context().cookies();
log("guest cookie httpOnly:", cookies.find((c) => c.name === "mise_chat_guest")?.httpOnly, "| token visible to script:",
  await guest.evaluate(() => document.cookie.includes("mise_chat_guest")));

// ---------------------------------------------------------------- agent in the admin inbox
const agent = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
watch(agent, "agent");
await agent.goto("http://localhost:3101/login");
await agent.getByLabel("Email", { exact: true }).fill("support@mise.test");
await agent.getByLabel("Password").fill(PW);
await agent.getByRole("button", { name: "Sign in" }).click();
await agent.waitForURL((u) => !u.pathname.startsWith("/login"));
await agent.goto("http://localhost:3101/support/chat?view=unassigned", { waitUntil: "networkidle" });
await agent.getByRole("link", { name: /Renee Guest/ }).first().click();
await agent.getByText(`(${stamp})`).first().waitFor();
await agent.waitForTimeout(1200);
await shot(agent, "03-agent-inbox");

// The guest types: the agent sees it live.
await guest.getByLabel("Your message").fill("Also, what days");
await agent.getByText("The customer is typing…").waitFor({ timeout: 8000 }).then(() => log("agent saw typing: yes"), () => log("agent saw typing: NO"));
await guest.getByLabel("Your message").fill("");

await agent.getByLabel("Reply to the customer").fill(`Yes, we deliver to San Fernando. (${stamp}-reply)`);
await agent.getByRole("button", { name: "Send reply" }).click();
await agent.getByText(`(${stamp}-reply)`).first().waitFor();

// ---------------------------------------------------------------- the reply arrives live for the guest
const t0 = Date.now();
await guest.getByText(`(${stamp}-reply)`).waitFor({ timeout: 10_000 });
log("reply reached the guest live in", Date.now() - t0, "ms");
await guest.waitForTimeout(1500);
log("typing indicator cleared after the reply:", (await guest.getByText("The Mise team is typing…").count()) === 0);
await shot(guest, "04-guest-reply-live");
await agent.waitForTimeout(2500);
await agent.reload({ waitUntil: "networkidle" });
log("agent sees 'Seen' receipt:", await agent.getByText("· Seen").count() > 0);
await shot(agent, "05-agent-after-reply");

// Internal note never reaches the guest.
await agent.getByRole("radio", { name: "Internal note" }).click();
await agent.getByLabel(/Internal note/).fill(`NOTE-${stamp} check zone list`);
await agent.getByRole("button", { name: "Add note" }).click();
await agent.getByText(`NOTE-${stamp}`).first().waitFor();
await guest.waitForTimeout(2500);
log("note visible to guest:", (await guest.content()).includes(`NOTE-${stamp}`));

// ---------------------------------------------------------------- signed-in customer on a phone
const phone = await (await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })).newPage();
watch(phone, "customer");
await phone.goto("http://localhost:3102/sign-in");
await phone.getByLabel("Email").fill("kavita@mise.test");
await phone.getByLabel("Password").fill(PW);
await phone.getByRole("button", { name: "Sign in" }).click();
await phone.waitForURL((u) => !u.pathname.startsWith("/sign-in"));
await phone.goto("http://localhost:3102/dashboard", { waitUntil: "networkidle" });
await shot(phone, "06-customer-phone-launcher");
await phone.getByRole("button", { name: /Chat with Mise/ }).click();
await phone.getByRole("button", { name: "Send us a message" }).click();
await phone.getByLabel("Message").fill(`Can I change my delivery address for next week? (${stamp}-k)`);
await phone.getByRole("button", { name: "Send", exact: true }).click();
await phone.getByText(`(${stamp}-k)`).waitFor();
await phone.waitForTimeout(800);
await shot(phone, "07-customer-phone-chat");

// ---------------------------------------------------------------- help centre and launcher placement
await phone.goto("http://localhost:3102/help", { waitUntil: "networkidle" });
await shot(phone, "08-help-centre-phone");
// Checkout pages hide the launcher (Kavita already subscribes, so use a fresh visitor on /subscribe).
const visitor = await (await browser.newContext()).newPage();
await visitor.goto("http://localhost:3102/plans", { waitUntil: "networkidle" });
const onPlans = await visitor.getByRole("button", { name: /Chat with Mise/ }).count();
await visitor.goto("http://localhost:3102/test-checkout/none", { waitUntil: "networkidle" });
log("launcher on /plans:", onPlans === 1, "| hidden on test checkout:", (await visitor.getByRole("button", { name: /Chat with Mise/ }).count()) === 0);

// ---------------------------------------------------------------- manager: settings and the help centre workflow
const mgr = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
watch(mgr, "admin");
await mgr.goto("http://localhost:3101/login");
await mgr.getByLabel("Email", { exact: true }).fill("admin@mise.test");
await mgr.getByLabel("Password").fill(PW);
await mgr.getByRole("button", { name: "Sign in" }).click();
await mgr.waitForURL((u) => !u.pathname.startsWith("/login"));
await mgr.goto("http://localhost:3101/support/chat/settings", { waitUntil: "networkidle" });
await shot(mgr, "09-chat-settings");
await mgr.goto("http://localhost:3101/help-centre/new", { waitUntil: "networkidle" });
const slug = `walkthrough-${stamp}`;
await mgr.getByLabel("Title").fill(`Walkthrough article ${stamp}`);
await mgr.getByLabel(/^Address/).fill(slug);
await mgr.getByLabel(/^Summary/).fill("A test article created by the walkthrough.");
await mgr.getByLabel(/^Article/).fill("First paragraph.\n\nSecond paragraph.");
await mgr.getByRole("button", { name: "Create draft" }).click();
await mgr.waitForURL(/\/help-centre\/[0-9a-f-]{36}$/);
await mgr.getByRole("button", { name: "Send for review" }).click();
await mgr.getByRole("button", { name: "Publish" }).waitFor();
await mgr.getByRole("button", { name: "Publish" }).click();
await mgr.getByRole("button", { name: /Archive/ }).waitFor(); // the workflow moves on: published
await shot(mgr, "10-help-article-published");
await guest.goto(`http://localhost:3102/help?q=${stamp}`, { waitUntil: "networkidle" });
log("published article found in public help search:", (await guest.getByRole("link", { name: `Walkthrough article ${stamp}` }).count()) === 1);
await guest.goto(`http://localhost:3102/help/${slug}`, { waitUntil: "networkidle" });
await shot(guest, "11-help-article-public");
// Revise takes it off the site again (keeps the dev data tidy).
await mgr.getByRole("button", { name: /Revise/ }).click();
await mgr.getByRole("button", { name: "Send for review" }).waitFor(); // a draft again

log(errors.length ? "ERRORS:\n" + errors.join("\n") : "no page errors");
await browser.close();
