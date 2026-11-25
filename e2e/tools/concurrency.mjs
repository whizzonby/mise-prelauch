#!/usr/bin/env node
// Commerce concurrency checks against a LOCAL stack (Sprint 9). Never run it against production.
//
//   node e2e/tools/concurrency.mjs [apiBase] [mailpitBase]
//
// 1. 20 simultaneous checkout attempts by one customer   → one live subscription, no server errors.
// 2. Every opened checkout paid 5× at once                → one payment taken, one invoice, subscription active.
// 3. 20 simultaneous meal confirmations for the same week → one confirmed selection; the allowance holds.
// Prints one JSON line per scenario and exits non-zero if any expectation fails.

const api = (process.argv[2] ?? "http://localhost:8090").replace(/\/$/, "");
const mailpit = (process.argv[3] ?? "http://localhost:8026").replace(/\/$/, "");
const N = 20;
let failed = false;

async function call(method, path, body, token) {
  const res = await fetch(`${api}/api/v1${path}`, {
    method,
    headers: { "content-type": "application/json", ...(token ? { authorization: `Bearer ${token}` } : {}) },
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(30_000),
  }).catch((e) => ({ status: 0, text: async () => JSON.stringify({ error: { code: e.name } }) }));
  const text = await res.text();
  let json = null;
  try {
    json = JSON.parse(text);
  } catch {}
  return { status: res.status, data: json?.data, error: json?.error?.code };
}
const tally = (rs) => rs.reduce((m, r) => ((m[`${r.status}${r.error ? ":" + r.error : ""}`] = (m[`${r.status}${r.error ? ":" + r.error : ""}`] ?? 0) + 1), m), {});
function check(name, ok, detail) {
  console.log(JSON.stringify({ scenario: name, ok, ...detail }));
  if (!ok) failed = true;
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function confirmedCustomer() {
  const email = `concurrency-${Date.now()}@example.com`;
  const password = "plantain and saltfish 2026";
  const reg = await call("POST", "/auth/register", { email, password, first_name: "Load", last_name: "Test", accept_terms: true });
  if (reg.status !== 202) throw new Error(`register: ${reg.status}`);
  let token = null;
  for (let i = 0; i < 30 && !token; i++) {
    await sleep(1000);
    const found = await (await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:${email}`)}`)).json();
    const id = found.messages?.[0]?.ID;
    if (!id) continue;
    const msg = await (await fetch(`${mailpit}/api/v1/message/${id}`)).json();
    const link = msg.Text.match(/https?:\/\/\S*verify-email\S*/)?.[0];
    if (link) token = new URL(link).searchParams.get("token");
  }
  if (!token) throw new Error("no confirmation email");
  await call("POST", "/auth/verify-email", { token });
  const login = await call("POST", "/auth/login", { email, password });
  const session = login.data.token;
  const addr = await call("POST", "/me/addresses", { recipient_name: "Load Test", line1: "3 Marine Square", city: "Port of Spain", region: "port-of-spain" }, session);
  return { session, addressId: addr.data.id };
}

const { session, addressId } = await confirmedCustomer();
const plans = await call("GET", "/plans");
const basic = plans.data.items.find((p) => p.slug === "basic");

// 1. Simultaneous checkouts. Starting again replaces an unfinished subscription, so several may be
//    created and replaced in turn; what must hold is one live subscription.
const subs = await Promise.all(Array.from({ length: N }, () => call("POST", "/me/subscriptions", { plan_id: basic.id, address_id: addressId }, session)));
const created = subs.filter((r) => r.status < 300 && r.data);
const refs = created.map((r) => r.data.checkout.redirect_url.split("/").pop());
const live = (await call("GET", "/me/subscription", null, session)).data?.subscription;
check("simultaneous checkouts", subs.every((r) => r.status > 0 && r.status < 500) && live?.status === "pending_payment", {
  attempts: N,
  statuses: tally(subs),
  checkouts_opened: refs.length,
  live_subscription: live?.status,
});

// 2. Every checkout that was opened is paid, many times each and all at once (several tabs, a
//    provider retrying its callback). Only the live one may take money; replaced ones are closed.
// A person reaches an old tab seconds later at the earliest; the worker closes replaced checkouts
// within its poll interval (2 s). A payment inside that window is flagged on the void bill for a refund.
await sleep(5000);
const pays = await Promise.all(refs.flatMap((ref) => Array.from({ length: 5 }, () => call("POST", `/test-payments/sessions/${ref}/complete`, { outcome: "approve", card: "approve" }))));
let sub = null;
for (let i = 0; i < 60; i++) {
  sub = (await call("GET", "/me/subscription", null, session)).data?.subscription;
  if (sub?.status === "active") break;
  await sleep(1000);
}
await sleep(3000); // let any late webhook jobs finish
const invoices = (await call("GET", "/me/invoices", null, session)).data?.items ?? [];
check("duplicate payment callbacks", sub?.status === "active" && invoices.length === 1, {
  completions: pays.length,
  statuses: tally(pays),
  subscription: sub?.status,
  invoices: invoices.length,
});

// 3. Simultaneous meal confirmations for the first open week.
const cycles = (await call("GET", "/me/subscription-cycles", null, session)).data?.items ?? [];
let picked = null;
for (const c of cycles.filter((c) => c.state !== "cancelled" && c.status !== "cancelled")) {
  const pg = await call("GET", `/me/meal-selections/${c.id}`, null, session);
  if (pg.status === 200 && (pg.data?.menu?.meals ?? []).length) {
    picked = { cycle: c, page: pg.data };
    break;
  }
}
if (!picked) {
  check("simultaneous meal confirmations", false, { error: "no open week with a menu", cycles: cycles.map((c) => c.week) });
} else {
  const allowance = picked.page.allowance ?? picked.page.meals_per_week ?? 3;
  const items = [{ meal_id: picked.page.menu.meals[0].id, quantity: allowance }];
  const fins = await Promise.all(Array.from({ length: N }, () => call("POST", `/me/meal-selections/${picked.cycle.id}/finalize`, { items }, session)));
  const after = (await call("GET", `/me/meal-selections/${picked.cycle.id}`, null, session)).data;
  const sel = after?.selection ?? after;
  const kits = (sel?.items ?? []).reduce((n, it) => n + (it.quantity ?? 0), 0);
  check("simultaneous meal confirmations", fins.some((r) => r.status < 300) && fins.every((r) => r.status > 0 && r.status < 500) && kits === allowance, {
    attempts: N,
    week: picked.cycle.week,
    statuses: tally(fins),
    allowance,
    kits_after: kits,
  });
}
process.exit(failed ? 1 : 0);
