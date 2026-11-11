/**
 * Typed client for the Mise API. Used by the marketing site (browser), and by
 * the admin and customer apps (on their servers only). The contract is
 * docs/api/openapi.yaml; `schema.d.ts` is generated from it (`pnpm --filter
 * @mise/api-client generate`) and `contract-check.ts` fails the type check if
 * the hand-written types below drift from it.
 */

import type { components } from "./schema";

export type { components, paths } from "./schema";

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
  last_name: string;
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

// ---------- accounts (types generated from the contract) ----------

export type Customer = components["schemas"]["Customer"];
export type RegisterInput = components["schemas"]["RegisterInput"];
export type SessionInfo = components["schemas"]["SessionInfo"];
export type MFAState = components["schemas"]["MFAState"];
export type AdminPrincipal = components["schemas"]["AdminPrincipal"];
export type Permission = AdminPrincipal["permissions"][number];

export interface CustomerLoginResult {
  token: string;
  expires_in: number;
  user: Customer;
}

export interface AdminLoginResult {
  token: string;
  expires_in: number;
  admin: AdminPrincipal;
  /** Anything but "verified" or "not_enrolled" means the second step is still due. */
  mfa: MFAState;
}

export interface MFASetup {
  secret: string;
  otpauth_url: string;
  qr_png_base64: string;
}

/** Whether an admin session may use the console, or must finish MFA first. */
export function mfaComplete(state: MFAState): boolean {
  return state === "verified" || state === "not_enrolled" || state === "not_applicable";
}

// ---------- Sprint 2: the customer's records and the catalogue (generated types) ----------

type S = components["schemas"];
export type Term = S["Term"];
export type Profile = S["Profile"];
export type ProfileInput = S["ProfileInput"];
export type Household = S["Household"];
export type HouseholdInput = S["HouseholdInput"];
export type Preferences = S["Preferences"];
export type PreferencesUpdate = S["PreferencesUpdate"];
export type Onboarding = S["Onboarding"];
export type OnboardingStep = Onboarding["step"];
export type NotificationPreferences = S["NotificationPreferences"];
export type Address = S["Address"];
export type AddressInput = S["AddressInput"];
export type DeliveryAvailability = S["DeliveryAvailability"];
export type MealCard = S["MealCard"];
export type MealDetail = S["MealDetail"];
export type MealPage = S["MealPage"];
export type MealCategory = S["MealCategory"];
export type ChefRef = S["Ref"];
export type ChefProfile = S["ChefProfile"];
export type Recommendations = S["Recommendations"];
export type Reference = S["Reference"];
export type FavoriteList = S["FavoriteList"];
export type AdminMeal = S["AdminMeal"];
export type AdminMealPage = S["AdminMealPage"];
export type MealInput = S["MealInput"];
export type NutritionInput = S["NutritionInput"];
export type AdminCategory = S["AdminCategory"];
export type CategoryInput = S["CategoryInput"];
export type AdminChef = S["AdminChef"];
export type ChefInput = S["ChefInput"];

export interface MealFilter {
  category?: string;
  /** Dietary keys; a meal must suit every one. */
  diet?: string[];
  max_minutes?: number;
  protein?: string;
  chef?: string;
  q?: string;
  exclude_allergens?: string[];
  sort?: "newest" | "quickest" | "name";
  page?: number;
  page_size?: number;
}

/** Query parameters for GET /meals: lists become comma-separated values. */
export function mealQuery(filter: MealFilter): Record<string, string | number | undefined> {
  return {
    ...filter,
    diet: filter.diet?.length ? filter.diet.join(",") : undefined,
    exclude_allergens: filter.exclude_allergens?.length ? filter.exclude_allergens.join(",") : undefined,
  };
}

// ---------- admin ----------

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
  /** Empty for leads who joined before the form asked for it. */
  last_name: string;
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
  last_name: string;
  status: "pending" | "converted" | "flagged";
  created_at: string;
  converted_at: string | null;
}

export interface LeadDetail {
  id: string;
  first_name: string;
  /** Empty for leads who joined before the form asked for it. */
  last_name: string;
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
  /** The customer account this lead became, once linked. */
  account: { customer_id: string; linked_at: string } | null;
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
  /**
   * Extra headers for every request, resolved per call. The Next.js servers
   * use it to pass on the end user's address and browser with the BFF key
   * (docs/security/AUTHENTICATION.md, "Client address").
   */
  headers?: () => Record<string, string> | Promise<Record<string, string>>;
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  token?: string;
  query?: object;
  keepalive?: boolean;
  /** Passed through to Next.js fetch on the server. */
  cache?: RequestCache;
  /** Next.js data cache options (revalidation), on the server. */
  next?: { revalidate?: number };
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

export function createClient({ baseUrl, fetch: fetchImpl = fetch, headers: extraHeaders }: ClientOptions) {
  const base = baseUrl.replace(/\/$/, "");

  async function buildRequest(path: string, opts: RequestOptions): Promise<[string, RequestInit]> {
    const headers: Record<string, string> = { ...(extraHeaders ? await extraHeaders() : {}), Accept: "application/json" };
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
        ...(opts.next ? { next: opts.next } : {}),
      } as RequestInit,
    ];
  }

  async function send(path: string, opts: RequestOptions): Promise<Response> {
    const [url, init] = await buildRequest(path, opts);
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

    /** Customer accounts. Server-side only: tokens never reach the browser. */
    auth: {
      register: (input: RegisterInput) =>
        request<{ status: "check_email" }>("/auth/register", { method: "POST", body: input, cache: "no-store" }),
      verifyEmail: (token: string) =>
        request<{ verified: boolean }>("/auth/verify-email", { method: "POST", body: { token }, cache: "no-store" }),
      login: (email: string, password: string) =>
        request<CustomerLoginResult>("/auth/login", { method: "POST", body: { email, password }, cache: "no-store" }),
      logout: (token: string) =>
        request<{ signed_out: boolean }>("/auth/logout", { method: "POST", token, cache: "no-store" }),
      forgotPassword: (email: string) =>
        request<{ status: "check_email" }>("/auth/password/forgot", { method: "POST", body: { email }, cache: "no-store" }),
      resetPassword: (token: string, password: string) =>
        request<{ password_reset: boolean }>("/auth/password/reset", {
          method: "POST",
          body: { token, password },
          cache: "no-store",
        }),
      me: (token: string) => request<Customer>("/me", { token, cache: "no-store" }),
      sessions: (token: string) => request<{ items: SessionInfo[] }>("/me/sessions", { token, cache: "no-store" }),
      revokeSession: (token: string, id: string) =>
        request<{ revoked: boolean }>(`/me/sessions/${encodeURIComponent(id)}`, { method: "DELETE", token, cache: "no-store" }),
      revokeOtherSessions: (token: string) =>
        request<{ revoked: number }>("/me/sessions", { method: "DELETE", token, cache: "no-store" }),
    },

    /** The signed-in customer's own records. Server-side only. */
    account: {
      profile: (token: string) => request<Profile>("/me/profile", { token, cache: "no-store" }),
      updateProfile: (token: string, input: ProfileInput) =>
        request<Profile>("/me/profile", { method: "PATCH", body: input, token, cache: "no-store" }),
      changePassword: (token: string, current_password: string, new_password: string) =>
        request<{ password_changed: boolean }>("/me/password", {
          method: "PUT",
          body: { current_password, new_password },
          token,
          cache: "no-store",
        }),
      requestEmailChange: (token: string, new_email: string, current_password: string) =>
        request<{ status: "check_email" }>("/me/email", {
          method: "POST",
          body: { new_email, current_password },
          token,
          cache: "no-store",
        }),
      confirmEmailChange: (emailToken: string) =>
        request<{ email_changed: boolean }>("/auth/confirm-email-change", {
          method: "POST",
          body: { token: emailToken },
          cache: "no-store",
        }),
      household: (token: string) => request<{ household: Household | null }>("/me/household", { token, cache: "no-store" }),
      putHousehold: (token: string, input: HouseholdInput) =>
        request<{ household: Household }>("/me/household", { method: "PUT", body: input, token, cache: "no-store" }),
      preferences: (token: string) => request<Preferences>("/me/preferences", { token, cache: "no-store" }),
      putPreferences: (token: string, input: PreferencesUpdate) =>
        request<Preferences>("/me/preferences", { method: "PUT", body: input, token, cache: "no-store" }),
      onboarding: (token: string) => request<Onboarding>("/me/onboarding", { token, cache: "no-store" }),
      putOnboarding: (token: string, step: OnboardingStep, skipped: string[]) =>
        request<Onboarding>("/me/onboarding", { method: "PUT", body: { step, skipped }, token, cache: "no-store" }),
      notifications: (token: string) =>
        request<NotificationPreferences>("/me/notification-preferences", { token, cache: "no-store" }),
      putNotifications: (token: string, topics: Record<string, boolean>) =>
        request<NotificationPreferences>("/me/notification-preferences", {
          method: "PUT",
          body: { topics },
          token,
          cache: "no-store",
        }),
      addresses: (token: string) => request<{ items: Address[] }>("/me/addresses", { token, cache: "no-store" }),
      createAddress: (token: string, input: AddressInput) =>
        request<Address>("/me/addresses", { method: "POST", body: input, token, cache: "no-store" }),
      updateAddress: (token: string, id: string, input: AddressInput) =>
        request<Address>(`/me/addresses/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      deleteAddress: (token: string, id: string) =>
        request<{ removed: boolean }>(`/me/addresses/${encodeURIComponent(id)}`, { method: "DELETE", token, cache: "no-store" }),
      favorites: (token: string) => request<FavoriteList>("/me/favorites", { token, cache: "no-store" }),
      addFavorite: (token: string, mealId: string) =>
        request<{ meal_id: string; saved: boolean }>("/me/favorites", {
          method: "POST",
          body: { meal_id: mealId },
          token,
          cache: "no-store",
        }),
      removeFavorite: (token: string, mealId: string) =>
        request<{ meal_id: string; saved: boolean }>(`/me/favorites/${encodeURIComponent(mealId)}`, {
          method: "DELETE",
          token,
          cache: "no-store",
        }),
      recommendations: (token: string, limit = 6) =>
        request<Recommendations>("/me/recommendations", { token, query: { limit }, cache: "no-store" }),
    },

    /** The public catalogue. Holds no personal data. */
    catalog: {
      meals: (filter: MealFilter = {}) => request<MealPage>("/meals", { query: mealQuery(filter), cache: "no-store" }),
      meal: (slug: string) => request<MealDetail>(`/meals/${encodeURIComponent(slug)}`, { cache: "no-store" }),
      categories: () => request<{ items: MealCategory[] }>("/meal-categories", { cache: "no-store" }),
      chefs: () => request<{ items: ChefRef[] }>("/chefs", { cache: "no-store" }),
      chef: (slug: string) => request<ChefProfile>(`/chefs/${encodeURIComponent(slug)}`, { cache: "no-store" }),
      reference: () => request<Reference>("/reference", { next: { revalidate: 3600 } }),
    },

    admin: {
      login: (email: string, password: string) =>
        request<AdminLoginResult>("/admin/auth/login", { method: "POST", body: { email, password }, cache: "no-store" }),
      logout: (token: string) =>
        request<{ signed_out: boolean }>("/admin/auth/logout", { method: "POST", token, cache: "no-store" }),
      me: (token: string) => request<AdminPrincipal>("/admin/auth/me", { token, cache: "no-store" }),
      mfaVerify: (token: string, code: string) =>
        request<AdminLoginResult>("/admin/auth/mfa/verify", { method: "POST", body: { code }, token, cache: "no-store" }),
      mfaSetup: (token: string) =>
        request<MFASetup>("/admin/auth/mfa/setup", { method: "POST", token, cache: "no-store" }),
      mfaConfirm: (token: string, code: string) =>
        request<AdminLoginResult>("/admin/auth/mfa/confirm", { method: "POST", body: { code }, token, cache: "no-store" }),
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
      catalogMeals: (token: string, query: { status?: string; q?: string; page?: number } = {}) =>
        request<AdminMealPage>("/admin/catalog/meals", { token, query, cache: "no-store" }),
      catalogMeal: (token: string, id: string) =>
        request<AdminMeal>(`/admin/catalog/meals/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createMeal: (token: string, input: MealInput) =>
        request<AdminMeal>("/admin/catalog/meals", { method: "POST", body: input, token, cache: "no-store" }),
      updateMeal: (token: string, id: string, input: MealInput) =>
        request<AdminMeal>(`/admin/catalog/meals/${encodeURIComponent(id)}`, { method: "PUT", body: input, token, cache: "no-store" }),
      setMealStatus: (token: string, id: string, action: "publish" | "unpublish") =>
        request<AdminMeal>(`/admin/catalog/meals/${encodeURIComponent(id)}/${action}`, { method: "POST", token, cache: "no-store" }),
      setNutrition: (token: string, id: string, input: NutritionInput) =>
        request<AdminMeal>(`/admin/catalog/meals/${encodeURIComponent(id)}/nutrition`, {
          method: "PUT",
          body: input,
          token,
          cache: "no-store",
        }),
      catalogCategories: (token: string) =>
        request<{ items: AdminCategory[] }>("/admin/catalog/categories", { token, cache: "no-store" }),
      createCategory: (token: string, input: CategoryInput) =>
        request<AdminCategory>("/admin/catalog/categories", { method: "POST", body: input, token, cache: "no-store" }),
      catalogChefs: (token: string) => request<{ items: AdminChef[] }>("/admin/catalog/chefs", { token, cache: "no-store" }),
      saveChef: (token: string, input: ChefInput, id?: string) =>
        request<AdminChef>(id ? `/admin/catalog/chefs/${encodeURIComponent(id)}` : "/admin/catalog/chefs", {
          method: id ? "PUT" : "POST",
          body: input,
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
