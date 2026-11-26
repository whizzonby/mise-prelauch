/**
 * Customer-app behaviour events, contract v1 (docs/analytics/BEHAVIOR_EVENTS.md).
 * Properties are short identifiers only (slugs, a week date, a count):
 * never form contents, names, emails, addresses or card details. Nothing is
 * sent when the browser signals Global Privacy Control or Do Not Track.
 * No cookies: a random visitor id lives in localStorage.
 */
export interface BehaviorEvents {
  catalogue_viewed: { category?: string; filters?: number };
  meal_viewed: { meal: string };
  plan_viewed: { plan: string };
  plan_selected: { plan: string };
  meal_selected: { meal: string; week: string };
  checkout_started: { kind: "subscription" | "week"; plan?: string };
}
export type BehaviorName = keyof BehaviorEvents;

/** True when the browser asks not to be tracked (Global Privacy Control or Do Not Track). */
export function trackingDeclined(): boolean {
  if (typeof navigator === "undefined") return true;
  const nav = navigator as Navigator & { globalPrivacyControl?: boolean };
  return nav.globalPrivacyControl === true || nav.doNotTrack === "1";
}

const ANON_KEY = "mise_bx_anon";
const SESSION_KEY = "mise_bx_session";
type Queued = { event_id: string; name: BehaviorName; version: 1; properties: Record<string, string | number> };
const queue: Queued[] = [];
let timer: ReturnType<typeof setTimeout> | undefined;
let listening = false;

function randomId(): string {
  return crypto.randomUUID().replace(/-/g, "");
}

function stored(storage: () => Storage, key: string): string {
  try {
    const s = storage();
    let v = s.getItem(key);
    if (!v || !/^[A-Za-z0-9_-]{8,64}$/.test(v)) {
      v = randomId();
      s.setItem(key, v);
    }
    return v;
  } catch {
    return randomId();
  }
}

export function flushBehavior(): void {
  if (timer) clearTimeout(timer);
  timer = undefined;
  if (queue.length === 0) return;
  const events = queue.splice(0, 20);
  const body = JSON.stringify({
    anonymous_id: stored(() => window.localStorage, ANON_KEY),
    session_id: stored(() => window.sessionStorage, SESSION_KEY),
    events,
  });
  // keepalive lets the last events of a page leave as the visitor navigates.
  fetch("/api/behavior", { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {});
  if (queue.length > 0) flushBehavior();
}

export function track<N extends BehaviorName>(name: N, properties: BehaviorEvents[N]): void {
  if (typeof window === "undefined" || trackingDeclined()) return;
  if (!listening) {
    listening = true;
    window.addEventListener("pagehide", flushBehavior);
    document.addEventListener("visibilitychange", () => document.visibilityState === "hidden" && flushBehavior());
  }
  queue.push({ event_id: crypto.randomUUID(), name, version: 1, properties: properties as Record<string, string | number> });
  if (queue.length >= 20) flushBehavior();
  else timer ??= setTimeout(flushBehavior, 1500);
}
