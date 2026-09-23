/**
 * Typed client for the Mise API. Used by the marketing site (browser), the
 * admin app (server) and, later, anything else written in TypeScript. The
 * contract is documented in docs/api/README.md.
 */

export type LeadStatus = "PENDING" | "VERIFIED" | "QUALIFIED" | "CONVERTED" | "UNSUBSCRIBED" | "BLOCKED";

export interface ReferralCounts {
  pending: number;
  converted: number;
}

export interface LeadSummary {
  first_name: string;
  status: LeadStatus;
  verification_required: boolean;
  referral_code: string;
  referral_url: string;
  referrals: ReferralCounts;
  profile_completed: boolean;
}

export interface AttributionTouch {
  utm_source?: string;
  utm_medium?: string;
  utm_campaign?: string;
  utm_content?: string;
  utm_term?: string;
  landing_page?: string;
  referrer_url?: string;
}

export interface CreateLeadInput {
  first_name: string;
  email: string;
  phone?: string;
  location: string;
  dietary_interests: string[];
  household_size?: number;
  packaging_preference?: string;
  consent: boolean;
  referral_code?: string;
  attribution?: { first?: AttributionTouch; latest?: AttributionTouch };
  anonymous_id?: string;
  /** Honeypot. Always empty when a person fills the form. */
  website: string;
  /** Milliseconds between the form appearing and being submitted. */
  elapsed_ms: number;
}

export type CreateLeadResult =
  | { outcome: "created"; lead: LeadSummary; profile_token: string }
  | { outcome: "already_on_list" };

export interface VerifyResult {
  lead: LeadSummary;
  profile_token: string;
}

export interface PreferencesInput {
  household_size?: number;
  meals_per_week?: number;
  dietary_preferences?: string[];
  meal_interests?: string[];
  cooking_frequency?: string;
  delivery_area?: string;
  packaging_preference?: string;
  household_type?: string;
  fitness_goal?: string;
  usage?: string;
}

export interface ReferralLookup {
  code: string;
  referrer_first_name: string;
}

export interface EventBatch {
  anonymous_id: string;
  session_id: string;
  events: { type: string; metadata?: Record<string, string | number | boolean> }[];
}

// ---------- admin ----------

export interface AdminPrincipal {
  id: string;
  email: string;
  name: string;
  role: string;
  permissions: string[];
}

export interface AdminLoginResult {
  token: string;
  expires_in: number;
  admin: AdminPrincipal;
}

export interface Bucket {
  label: string;
  count: number;
}

export interface Overview {
  total_leads: number;
  verified_leads: number;
  pending_leads: number;
  unsubscribed_leads: number;
  referral_conversions: number;
  referred_signups: number;
  profiles_completed: number;
  range_days: number;
  signups_by_day: { date: string; count: number }[];
  sources: Bucket[];
  dietary: Bucket[];
  household_sizes: Bucket[];
  /** Packaging votes; "none" counts leads who did not answer. */
  packaging: Bucket[];
  locations: Bucket[];
}

export interface LeadRow {
  id: string;
  first_name: string;
  email: string;
  status: LeadStatus;
  location: string;
  source: string;
  household_size: number | null;
  referrals_converted: number;
  was_referred: boolean;
  created_at: string;
}

export interface LeadPage {
  items: LeadRow[];
  total: number;
  page: number;
  page_size: number;
}

export interface LeadFilter {
  q?: string;
  status?: string;
  location?: string;
  source?: string;
  referral?: string;
  /** A packaging option value, or "none". */
  packaging?: string;
  from?: string;
  to?: string;
  page?: number;
  page_size?: number;
}

export interface ReferralEntry {
  lead_id: string;
  first_name: string;
  status: "pending" | "converted" | "flagged";
  created_at: string;
  converted_at: string | null;
}

export interface LeadDetail {
  id: string;
  first_name: string;
  email: string;
  phone: string | null;
  location: string;
  status: LeadStatus;
  referral_code: string;
  email_verified_at: string | null;
  consent_at: string;
  consent_version: string;
  created_at: string;
  updated_at: string;
  preferences: {
    household_size: number | null;
    meals_per_week: number | null;
    dietary_preferences: string[];
    meal_interests: string[];
    cooking_frequency: string | null;
    delivery_area: string | null;
    packaging_preference: string | null;
    metadata: Record<string, string>;
    updated_at: string;
  } | null;
  attribution: (AttributionTouch & { touch: "first" | "latest"; created_at: string })[];
  referred_by: ReferralEntry | null;
  referrals: ReferralEntry[];
  timeline: { kind: "event" | "audit"; type: string; metadata: Record<string, unknown>; created_at: string }[];
}

// ---------- errors ----------

export interface FieldError {
  field: string;
  code: string;
  message: string;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly fields: FieldError[] = [],
    readonly requestId = "",
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** Thrown when the API cannot be reached at all. */
export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super("The Mise API could not be reached.", { cause });
    this.name = "NetworkError";
  }
}

// ---------- client ----------

export interface ClientOptions {
  baseUrl: string;
  fetch?: typeof fetch;
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  token?: string;
  query?: object;
  keepalive?: boolean;
  /** Passed through to Next.js fetch on the server. */
  cache?: RequestCache;
}

function toQuery(query?: object): string {
  if (!query) return "";
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== "") params.set(key, String(value));
  }
  const text = params.toString();
  return text ? `?${text}` : "";
}

export function createClient({ baseUrl, fetch: fetchImpl = fetch }: ClientOptions) {
  const base = baseUrl.replace(/\/$/, "");

  function buildRequest(path: string, opts: RequestOptions): [string, RequestInit] {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (opts.body !== undefined) headers["Content-Type"] = "application/json";
    if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
    return [
      `${base}/api/v1${path}${toQuery(opts.query)}`,
      {
        method: opts.method ?? "GET",
        headers,
        body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
        keepalive: opts.keepalive,
        cache: opts.cache,
      },
    ];
  }

  async function send(path: string, opts: RequestOptions): Promise<Response> {
    const [url, init] = buildRequest(path, opts);
    let res: Response;
    try {
      res = await fetchImpl(url, init);
    } catch (cause) {
      throw new NetworkError(cause);
    }
    if (res.ok) return res;

    let payload: { error?: { code?: string; message?: string; fields?: FieldError[] }; meta?: { request_id?: string } } = {};
    try {
      payload = await res.json();
    } catch {
      // A proxy or crash produced a non-JSON error; fall through to the generic message.
    }
    throw new ApiError(
      res.status,
      payload.error?.code ?? "unknown_error",
      payload.error?.message ?? "Something went wrong. Try again in a moment.",
      payload.error?.fields ?? [],
      payload.meta?.request_id ?? res.headers.get("X-Request-Id") ?? "",
    );
  }

  async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    const res = await send(path, opts);
    const payload = (await res.json()) as { data: T };
    return payload.data;
  }

  return {
    createLead: (input: CreateLeadInput) => request<CreateLeadResult>("/leads", { method: "POST", body: input }),
    verifyLead: (token: string) => request<VerifyResult>("/leads/verify", { method: "POST", body: { token } }),
    unsubscribe: (token: string) =>
      request<{ unsubscribed: boolean }>("/leads/unsubscribe", { method: "POST", body: { token } }),
    getMe: (token: string) => request<LeadSummary>("/leads/me", { token }),
    updatePreferences: (token: string, input: PreferencesInput) =>
      request<LeadSummary>("/leads/preferences", { method: "PATCH", body: input, token }),
    lookupReferral: (code: string) => request<ReferralLookup>(`/referrals/${encodeURIComponent(code)}`),
    sendEvents: (batch: EventBatch, token?: string) =>
      request<{ accepted: number }>("/events", { method: "POST", body: batch, token, keepalive: true }),

    admin: {
      login: (email: string, password: string) =>
        request<AdminLoginResult>("/admin/auth/login", { method: "POST", body: { email, password }, cache: "no-store" }),
      logout: (token: string) =>
        request<{ signed_out: boolean }>("/admin/auth/logout", { method: "POST", token, cache: "no-store" }),
      me: (token: string) => request<AdminPrincipal>("/admin/auth/me", { token, cache: "no-store" }),
      overview: (token: string, days = 30) =>
        request<Overview>("/admin/stats/overview", { token, query: { days }, cache: "no-store" }),
      listLeads: (token: string, filter: LeadFilter = {}) =>
        request<LeadPage>("/admin/leads", { token, query: filter, cache: "no-store" }),
      getLead: (token: string, id: string) =>
        request<LeadDetail>(`/admin/leads/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      setLeadStatus: (token: string, id: string, status: LeadStatus) =>
        request<LeadDetail>(`/admin/leads/${encodeURIComponent(id)}`, {
          method: "PATCH",
          body: { status },
          token,
          cache: "no-store",
        }),
      eraseLead: (token: string, id: string) =>
        request<{ erased: boolean }>(`/admin/leads/${encodeURIComponent(id)}`, {
          method: "DELETE",
          token,
          cache: "no-store",
        }),
      /** Returns the raw CSV response so it can be streamed to the browser. */
      exportLeads: (token: string, filter: LeadFilter = {}) =>
        send("/admin/leads/export.csv", { token, query: filter, cache: "no-store" }),
    },
  };
}

export type MiseClient = ReturnType<typeof createClient>;
