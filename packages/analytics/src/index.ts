/**
 * First-party analytics for Mise. The taxonomy below is the whole list: the
 * API rejects anything else (services/api/internal/events/events.go).
 *
 * Privacy rules, enforced here and by the API:
 *  - no cookies; ids live in web storage and never leave the Mise API
 *  - metadata is a small flat object of identifiers, never form values
 *  - nothing is recorded when the browser sends Global Privacy Control or Do Not Track
 */

export interface EventMetadata {
  page_view: { path: string };
  section_viewed: { section: string };
  hero_cta_clicked: { cta: "primary" | "secondary" };
  waitlist_started: { placement: string };
  waitlist_completed: { placement: string; outcome: "created" | "already_on_list"; referred: boolean };
  preferences_started: Record<string, never>;
  preferences_completed: { answered: number };
  referral_link_copied: Record<string, never>;
  referral_shared: { channel: "whatsapp" | "email" | "native" };
  faq_opened: { question: string };
  meal_viewed: { meal: string; category: string };
  plan_viewed: { plan: string };
}

export type EventType = keyof EventMetadata;

export const EVENT_TYPES = [
  "page_view",
  "section_viewed",
  "hero_cta_clicked",
  "waitlist_started",
  "waitlist_completed",
  "preferences_started",
  "preferences_completed",
  "referral_link_copied",
  "referral_shared",
  "faq_opened",
  "meal_viewed",
  "plan_viewed",
] as const satisfies readonly EventType[];

interface QueuedEvent {
  type: EventType;
  metadata: Record<string, string | number | boolean>;
}

export interface TrackerOptions {
  /** Sends a batch to the API. Injected so this package has no network code. */
  send: (batch: { anonymous_id: string; session_id: string; events: QueuedEvent[] }) => Promise<unknown>;
  flushIntervalMs?: number;
}

export interface Tracker {
  track<T extends EventType>(type: T, metadata: EventMetadata[T]): void;
  flush(): void;
  anonymousId(): string;
  enabled: boolean;
}

const ANON_KEY = "mise.anonymous_id";
const SESSION_KEY = "mise.session_id";
const MAX_BATCH = 20;

function randomId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 14)}`;
}

function storedId(storage: () => Storage, key: string): string {
  try {
    const store = storage();
    const existing = store.getItem(key);
    if (existing) return existing;
    const id = randomId();
    store.setItem(key, id);
    return id;
  } catch {
    // Storage is blocked (private mode, strict settings): use an id for this page only.
    return randomId();
  }
}

/** True when the visitor has asked not to be tracked. */
export function trackingDeclined(): boolean {
  if (typeof navigator === "undefined") return true;
  const nav = navigator as Navigator & { globalPrivacyControl?: boolean };
  return nav.globalPrivacyControl === true || nav.doNotTrack === "1";
}

const noopTracker: Tracker = {
  track() {},
  flush() {},
  anonymousId: () => "",
  enabled: false,
};

export function createTracker({ send, flushIntervalMs = 4000 }: TrackerOptions): Tracker {
  if (typeof window === "undefined" || trackingDeclined()) return noopTracker;

  const anonymous = storedId(() => window.localStorage, ANON_KEY);
  const session = storedId(() => window.sessionStorage, SESSION_KEY);
  let queue: QueuedEvent[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;

  function flush() {
    if (timer) {
      clearTimeout(timer);
      timer = undefined;
    }
    while (queue.length > 0) {
      const events = queue.slice(0, MAX_BATCH);
      queue = queue.slice(MAX_BATCH);
      // Analytics must never surface an error to the visitor or block the page.
      send({ anonymous_id: anonymous, session_id: session, events }).catch(() => {});
    }
  }

  window.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") flush();
  });
  window.addEventListener("pagehide", flush);

  return {
    enabled: true,
    anonymousId: () => anonymous,
    flush,
    track(type, metadata) {
      queue.push({ type, metadata: metadata as Record<string, string | number | boolean> });
      if (queue.length >= MAX_BATCH) flush();
      else timer ??= setTimeout(flush, flushIntervalMs);
    },
  };
}

// ---------- attribution ----------

export interface Touch {
  utm_source?: string;
  utm_medium?: string;
  utm_campaign?: string;
  utm_content?: string;
  utm_term?: string;
  landing_page?: string;
  referrer_url?: string;
}

const FIRST_TOUCH_KEY = "mise.first_touch";
const LATEST_TOUCH_KEY = "mise.latest_touch";
const REFERRAL_KEY = "mise.referral_code";
const UTM_KEYS = ["utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term"] as const;

/** Builds a touch from a landing URL and the document referrer. */
export function touchFromLocation(href: string, referrer: string): Touch {
  const url = new URL(href);
  const touch: Touch = { landing_page: url.pathname };
  for (const key of UTM_KEYS) {
    const value = url.searchParams.get(key)?.trim();
    if (value) touch[key] = value.slice(0, 120);
  }
  if (referrer) {
    try {
      const ref = new URL(referrer);
      // Keep where they came from, drop the query string: it can hold search
      // terms or identifiers that are none of our business.
      if (ref.origin !== url.origin) touch.referrer_url = ref.origin + ref.pathname;
    } catch {
      // Not a URL; ignore.
    }
  }
  return touch;
}

function hasSource(touch: Touch): boolean {
  return Boolean(touch.utm_source || touch.referrer_url);
}

function readJSON<T>(key: string): T | undefined {
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : undefined;
  } catch {
    return undefined;
  }
}

function writeJSON(key: string, value: unknown) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Storage unavailable; attribution is best-effort.
  }
}

/**
 * Records how this visit arrived. The first touch is written once and never
 * changed. The latest touch is replaced whenever a visit arrives with a
 * source (a campaign link or an external referrer).
 */
export function captureAttribution(href = window.location.href, referrer = document.referrer): void {
  if (trackingDeclined()) return;
  const touch = touchFromLocation(href, referrer);
  if (!readJSON<Touch>(FIRST_TOUCH_KEY)) writeJSON(FIRST_TOUCH_KEY, touch);
  if (hasSource(touch) || !readJSON<Touch>(LATEST_TOUCH_KEY)) writeJSON(LATEST_TOUCH_KEY, touch);
}

export function readAttribution(): { first?: Touch; latest?: Touch } {
  if (typeof window === "undefined") return {};
  return { first: readJSON<Touch>(FIRST_TOUCH_KEY), latest: readJSON<Touch>(LATEST_TOUCH_KEY) };
}

/**
 * The referral code is functional, not tracking: the visitor followed a
 * friend's link expecting it to count, so it is kept even when tracking is
 * declined.
 */
export function rememberReferralCode(code: string): void {
  try {
    window.localStorage.setItem(REFERRAL_KEY, code);
  } catch {
    // Storage unavailable.
  }
}

export function readReferralCode(): string | undefined {
  try {
    return window.localStorage.getItem(REFERRAL_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}
