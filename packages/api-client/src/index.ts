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

// ---------- Sprint 3: subscription commerce (generated types) ----------

export type Plan = S["Plan"];
export type PlanPrice = S["Price"];
export type AdminPlan = S["AdminPlan"];
export type PlanInput = S["PlanInput"];
export type PriceInput = S["PriceInput"];
export type MenuSummary = S["MenuSummary"];
export type WeeklyMenu = S["WeeklyMenu"];
export type MenuMeal = S["MenuMeal"];
export type AdminMenu = S["AdminMenu"];
export type MenuInput = S["MenuInput"];
export type Amounts = S["Amounts"];
export type Quote = S["Quote"];
export type SubscribeInput = S["SubscribeInput"];
export type SubscribeResult = S["SubscribeResult"];
export type Checkout = S["Checkout"];
export type CheckoutResult = S["CheckoutResult"];
export type Subscription = S["Subscription"];
export type SubscriptionStatus = Subscription["status"];
export type SubscriptionCycle = S["SubscriptionCycle"];
export type SubscriptionInfo = S["SubscriptionInfo"];
export type SelectionItem = S["SelectionItem"];
export type MealSelection = S["MealSelection"];
export type MealNote = S["MealNote"];
export type SelectionPage = S["SelectionPage"];
export type Order = S["Order"];
export type OrderSummary = S["OrderSummary"];
export type BillingCycle = S["BillingCycle"];
export type BillingSummary = S["BillingSummary"];
export type Invoice = S["Invoice"];
export type InvoiceSummary = S["InvoiceSummary"];
export type AdminSubscriptionRow = S["AdminSubscriptionRow"];
export type AdminSubscription = S["AdminSubscription"];
export type Payment = S["Payment"];
export type PaymentDetail = S["PaymentDetail"];
export type RefundInput = S["RefundInput"];
export type StoredWebhook = S["StoredWebhook"];
export type Demand = S["Demand"];
export type Promotion = S["Promotion"];
export type PromotionInput = S["PromotionInput"];
export type FailedJob = S["FailedJob"];
export type TestpaySession = S["TestpaySession"];

/** Formats minor units: 33000 TTD is "TTD 330.00". Display only; amounts come from the API. */
export function formatMoney(minor: number, currency = "TTD"): string {
  const sign = minor < 0 ? "-" : "";
  const abs = Math.abs(minor);
  const whole = Math.floor(abs / 100).toLocaleString("en-US");
  return `${sign}${currency} ${whole}.${String(abs % 100).padStart(2, "0")}`;
}

// ---------- Sprint 4: administration and CRM (generated types) ----------

export type Metric = S["Metric"];
export type Series = S["Series"];
export type Share = S["Share"];
export type AttentionItem = S["AttentionItem"];
export type Dashboard = S["Dashboard"];
export type Report = S["Report"];
export type CustomerRow = S["CustomerRow"];
export type CustomerPage = S["CustomerPage"];
export type Customer360 = S["Customer360"];
export type CustomerBilling = S["CustomerBilling"];
export type CustomerSensitive = S["CustomerSensitive"];
export type ActivityEntry = S["ActivityEntry"];
export type CrmNote = S["CrmNote"];
export type LeadCRM = S["LeadCRM"];
export type SegmentRules = S["SegmentRules"];
export type Segment = S["Segment"];
export type SegmentInput = S["SegmentInput"];
export type Campaign = S["Campaign"];
export type CampaignInput = S["CampaignInput"];
export type Attribution = S["Attribution"];
export type ReferralRow = S["ReferralRow"];
export type RetentionSignal = S["RetentionSignal"];
export type SupportCase = S["SupportCase"];
export type SupportCasePage = S["SupportCasePage"];
export type CaseInput = S["CaseInput"];
export type CaseUpdate = S["CaseUpdate"];
export type StaffActionInput = S["StaffActionInput"];
export type InvoiceRow = S["InvoiceRow"];
export type ExportRequest = S["ExportRequest"];
export type ExportJob = S["Export"];
export type AdminAlert = S["AdminAlert"];
export type Settings = S["Settings"];
export type IntegrationStatus = S["IntegrationStatus"];
export type AuditEntry = S["AuditEntry"];
export type StaffMember = S["StaffMember"];
export type StaffInvitation = S["StaffInvitation"];
export type RoleInfo = S["RoleInfo"];
export type PermissionInfo = S["PermissionInfo"];

export interface CustomerFilter {
  q?: string;
  status?: string;
  subscription_status?: string;
  plan_id?: string;
  region?: string;
  segment_id?: string;
  sort?: string;
  page?: number;
  page_size?: number;
}

export interface CaseFilter {
  status?: string;
  open?: boolean;
  customer_id?: string;
  assigned_to?: string;
  priority?: string;
  page?: number;
}

export interface AuditFilter {
  actor?: string;
  action?: string;
  target_type?: string;
  target_id?: string;
  from?: string;
  to?: string;
  page?: number;
}

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
  /** CRM stage (Sprint 4); converted and unsubscribed come from the lifecycle. */
  stage: LeadStage;
  assigned_to: string | null;
}

export type LeadStage = "new" | "contacted" | "qualified" | "unqualified" | "converted" | "unsubscribed";

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
  stage?: string;
  /** A staff user id, or "none". */
  assigned_to?: string;
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

// ---------- Sprint 5: Chef and Production ERP (generated types) ----------

export type Unit = S["Unit"];
export type UnitConversion = S["UnitConversion"];
export type CostRef = S["CostRef"];
export type Ingredient = S["Ingredient"];
export type IngredientPage = S["IngredientPage"];
export type IngredientInput = S["CatalogIngredientInput"];
export type ConversionInput = S["ConversionInput"];
export type CostInput = S["CostInput"];
export type Chef = S["Chef"];
export type ChefTeamInput = S["ChefTeamInput"];
export type Recipe = S["Recipe"];
export type RecipePage = S["RecipePage"];
export type RecipeInput = S["RecipeInput"];
export type RecipeVersionSummary = S["RecipeVersionSummary"];
export type RecipeVersion = S["RecipeVersion"];
export type RecipeLine = S["RecipeLine"];
export type RecipeLineInput = S["RecipeLineInput"];
export type RecipeNutrition = S["RecipeNutrition"];
export type RecipeReview = S["RecipeReview"];
export type DraftInput = S["DraftInput"];
export type ReviewInput = S["ReviewInput"];
export type QueueItem = S["QueueItem"];
export type Costing = S["Costing"];
export type DemandMeal = S["DemandMeal"];
export type DemandSnapshot = S["DemandSnapshot"];
export type ProductionDemand = S["ProductionDemand"];
export type ProductionPlan = S["ProductionPlan"];
export type PlanItem = S["PlanItem"];
export type Requirement = S["Requirement"];
export type RequirementRow = S["RequirementRow"];
export type Batch = S["Batch"];
export type BatchYield = S["Yield"];
export type BatchPackaging = S["Packaging"];
export type QualityCheck = S["QualityCheck"];
export type ChecklistRow = S["ChecklistRow"];
export type QualityHold = S["QualityHold"];
export type KitchenTask = S["Task"];
export type Waste = S["Waste"];
export type KitLabel = S["KitLabel"];
export type TaskTemplate = S["TaskTemplate"];
export type TaskTemplateInput = S["TaskTemplateInput"];
export type QualityTemplate = S["QualityTemplate"];
export type QualityTemplateInput = S["QualityTemplateInput"];
export type ScheduleSlot = S["ScheduleSlot"];
export type ProductionSettings = S["ProductionSettings"];
export type ProductionSettingsInput = S["ProductionSettingsInput"];
export type ProductionOverview = S["ProductionOverview"];
export type ProductionCalendar = S["ProductionCalendar"];
export type CalendarDay = S["CalendarDay"];
export type WasteInput = S["WasteInput"];
export type PackagingInput = S["PackagingInput"];
export type OutputInput = S["OutputInput"];
export type TaskInput = S["TaskInput"];

// ---------- Sprint 6: supply chain (generated types) ----------

export type Supplier = S["Supplier"];
export type SupplierPage = S["SupplierPage"];
export type SupplierInput = S["SupplierInput"];
export type SupplierActionInput = S["SupplierActionInput"];
export type SupplierReviewInput = S["SupplierReviewInput"];
export type CriterionResult = S["CriterionResult"];
export type SupplierFarm = S["SupplierFarm"];
export type FarmInput = S["FarmInput"];
export type SupplierDocument = S["SupplierDocument"];
export type SupplierDocumentInput = S["SupplierDocumentInput"];
export type SupplierCommunication = S["SupplierCommunication"];
export type CommunicationInput = S["CommunicationInput"];
export type SupplierCriterion = S["SupplierCriterion"];
export type CriterionInput = S["CriterionInput"];
export type SupplierStatusEvent = S["SupplierStatusEvent"];
export type SupplierReview = S["SupplierReview"];
export type SupplierProduct = S["SupplierProduct"];
export type SupplierProductInput = S["SupplierProductInput"];
export type SupplierPrice = S["SupplierPrice"];
export type SupplierPriceInput = S["SupplierPriceInput"];
export type MappingInput = S["MappingInput"];
export type SupplierAvailability = S["SupplierAvailability"];
export type AvailabilityInput = S["AvailabilityInput"];
export type ProcurementSettings = S["ProcurementSettings"];
export type ProcurementSettingsInput = S["ProcurementSettingsInput"];
export type ProcurementRun = S["ProcurementRun"];
export type ProcurementLine = S["ProcurementLine"];
export type RunRequirement = S["RunRequirement"];
export type SupplyOption = S["SupplyOption"];
export type LinePurchaseOrder = S["LinePurchaseOrder"];
export type ProductionPlanRef = S["ProductionPlanRef"];
export type EventDeliveries = S["EventDeliveries"];
export type DecisionInput = S["DecisionInput"];
export type PurchaseOrder = S["PurchaseOrder"];
export type POLine = S["POLine"];
export type POEvent = S["POEvent"];
export type PORevision = S["PORevision"];
export type POInput = S["POInput"];
export type POLineInput = S["POLineInput"];
export type IssueInput = S["IssueInput"];
export type RFQ = S["RFQ"];
export type RFQLine = S["RFQLine"];
export type RFQInvite = S["RFQInvite"];
export type RFQInput = S["RFQInput"];
export type RFQInviteInput = S["RFQInviteInput"];
export type Quotation = S["Quotation"];
export type QuotationLine = S["QuotationLine"];
export type QuotationInput = S["QuotationInput"];
export type StockSummary = S["StockSummary"];
export type StockView = S["StockView"];
export type InventoryLot = S["InventoryLot"];
export type InventoryMovement = S["InventoryMovement"];
export type InventoryReservation = S["InventoryReservation"];
export type BatchLotUse = S["BatchLotUse"];
export type LotStatusInput = S["LotStatusInput"];
export type LotExpiryInput = S["LotExpiryInput"];
export type LotTransferInput = S["LotTransferInput"];
export type LotQuantityInput = S["LotQuantityInput"];
export type AdjustmentInput = S["AdjustmentInput"];
export type ReserveInput = S["ReserveInput"];
export type ReserveResult = S["ReserveResult"];
export type BatchRef = S["BatchRef"];
export type IssueStockInput = S["IssueStockInput"];
export type IssueResult = S["IssueResult"];
export type ReturnStockInput = S["ReturnStockInput"];
export type Reconciliation = S["Reconciliation"];
export type ReconcileLine = S["ReconcileLine"];
export type StockCount = S["StockCount"];
export type CountLine = S["CountLine"];
export type StorageLocation = S["StorageLocation"];
export type LocationInput = S["LocationInput"];
export type InventoryPolicy = S["InventoryPolicy"];
export type InventoryPolicyInput = S["InventoryPolicyInput"];
export type ReceivingChecklistItem = S["ReceivingChecklistItem"];
export type ReceivingChecklistInput = S["ReceivingChecklistInput"];
export type GoodsReceipt = S["GoodsReceipt"];
export type ReceiptLine = S["ReceiptLine"];
export type ReceiptInput = S["ReceiptInput"];
export type InspectionResult = S["InspectionResult"];
export type Trace = S["Trace"];
export type TraceLot = S["TraceLot"];
export type TraceBatch = S["TraceBatch"];
export type LotRef = S["LotRef"];

// ---------- Sprint 7: fulfillment and delivery (generated types) ----------

export type Fulfillment = S["Fulfillment"];
export type FulfillmentPackage = S["FulfillmentPackage"];
export type FulfillmentItem = S["FulfillmentItem"];
export type FulfillmentHistory = S["FulfillmentHistory"];
export type FulfillmentCheck = S["FulfillmentCheck"];
export type VerifyInput = S["VerifyInput"];
export type FulfillmentOutcome = S["FulfillmentOutcome"];
export type ConsumerStatus = S["ConsumerStatus"];
export type PackageLabel = S["PackageLabel"];
export type FulfillmentTrace = S["FulfillmentTrace"];
export type FulfillmentTraceItem = S["FulfillmentTraceItem"];
export type FulfillmentTraceLot = S["FulfillmentTraceLot"];
export type DeliverySettings = S["DeliverySettings"];
export type DeliverySettingsInput = S["DeliverySettingsInput"];
export type DeliveryZone = S["DeliveryZone"];
export type DeliveryZoneInput = S["DeliveryZoneInput"];
export type DeliveryRegion = S["DeliveryRegion"];
export type RegionCoverage = S["RegionCoverage"];
export type ServiceDay = S["ServiceDay"];
export type ServiceDayInput = S["ServiceDayInput"];
export type DeliveryWindow = S["DeliveryWindow"];
export type DeliveryCapacity = S["DeliveryCapacity"];
export type DeliveryCapacityInput = S["DeliveryCapacityInput"];
export type DeliveryPartner = S["DeliveryPartner"];
export type DeliveryPartnerInput = S["DeliveryPartnerInput"];
export type PartnerCriterion = S["PartnerCriterion"];
export type PartnerCriterionInput = S["PartnerCriterionInput"];
export type PartnerStatusInput = S["PartnerStatusInput"];
export type PartnerStatusEvent = S["PartnerStatusEvent"];
export type Courier = S["Courier"];
export type CourierInput = S["CourierInput"];
export type DeliveryCost = S["DeliveryCost"];
export type DeliveryCostInput = S["DeliveryCostInput"];
export type Delivery = S["Delivery"];
export type DeliveryAddress = S["DeliveryAddress"];
export type PackageRef = S["PackageRef"];
export type TrackingEvent = S["TrackingEvent"];
export type DeliveryProof = S["DeliveryProof"];
export type RescheduleRequest = S["RescheduleRequest"];
export type PendingReschedule = S["PendingReschedule"];
export type DeliveryReturn = S["DeliveryReturn"];
export type DeliveryNotification = S["DeliveryNotification"];
export type DeliveryCaseLink = S["DeliveryCaseLink"];
export type ScheduleInput = S["ScheduleInput"];
export type AddressCorrectionInput = S["AddressCorrectionInput"];
export type StatusUpdateInput = S["StatusUpdateInput"];
export type ProofInput = S["ProofInput"];
export type OutcomeInput = S["OutcomeInput"];
export type ReturnInput = S["ReturnInput"];
export type ReturnReviewInput = S["ReturnReviewInput"];
export type CorrectionInput = S["CorrectionInput"];
export type RescheduleRequestInput = S["RescheduleRequestInput"];
export type RescheduleDecisionInput = S["RescheduleDecisionInput"];
export type DeliveryException = S["DeliveryException"];
export type DeliveryExceptionEvent = S["DeliveryExceptionEvent"];
export type DeliveryExceptionInput = S["DeliveryExceptionInput"];
export type DeliveryExceptionUpdate = S["DeliveryExceptionUpdate"];
export type DispatchRun = S["DispatchRun"];
export type DispatchStop = S["DispatchStop"];
export type DispatchManifest = S["DispatchManifest"];
export type ManifestContent = S["ManifestContent"];
export type ManifestStop = S["ManifestStop"];
export type Handoff = S["Handoff"];
export type DispatchRunInput = S["DispatchRunInput"];
export type StopInput = S["StopInput"];
export type HandoffInput = S["HandoffInput"];
export type DeliveryReconciliation = S["DeliveryReconciliation"];
export type DeliveryDiscrepancy = S["DeliveryDiscrepancy"];
export type DeliveryOverview = S["DeliveryOverview"];
export type DeliveryReport = S["DeliveryReport"];
export type DeliveryBreakdown = S["DeliveryBreakdown"];
export type CustomerDelivery = S["CustomerDelivery"];
export type DeliveryMilestone = S["DeliveryMilestone"];
export type DeliverySupportResult = S["DeliverySupportResult"];

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

      // Sprint 3: commerce administration.
      plans: (token: string) => request<{ items: AdminPlan[] }>("/admin/plans", { token, cache: "no-store" }),
      plan: (token: string, id: string) => request<AdminPlan>(`/admin/plans/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createPlan: (token: string, input: PlanInput) =>
        request<AdminPlan>("/admin/plans", { method: "POST", body: input, token, cache: "no-store" }),
      updatePlan: (token: string, id: string, input: PlanInput) =>
        request<AdminPlan>(`/admin/plans/${encodeURIComponent(id)}`, { method: "PUT", body: input, token, cache: "no-store" }),
      addPlanPrice: (token: string, id: string, input: PriceInput) =>
        request<AdminPlan>(`/admin/plans/${encodeURIComponent(id)}/prices`, { method: "POST", body: input, token, cache: "no-store" }),
      setPlanStatus: (token: string, id: string, action: "activate" | "retire") =>
        request<AdminPlan>(`/admin/plans/${encodeURIComponent(id)}/${action}`, { method: "POST", token, cache: "no-store" }),
      menus: (token: string, status?: string) =>
        request<{ items: MenuSummary[]; next_open_week: string; time_zone: string }>("/admin/menus", {
          token,
          query: { status },
          cache: "no-store",
        }),
      menu: (token: string, id: string) => request<AdminMenu>(`/admin/menus/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createMenu: (token: string, input: MenuInput) =>
        request<AdminMenu>("/admin/menus", { method: "POST", body: input, token, cache: "no-store" }),
      updateMenu: (token: string, id: string, input: MenuInput) =>
        request<AdminMenu>(`/admin/menus/${encodeURIComponent(id)}`, { method: "PUT", body: input, token, cache: "no-store" }),
      publishMenu: (token: string, id: string, publishAt?: string) =>
        request<AdminMenu>(`/admin/menus/${encodeURIComponent(id)}/publish`, {
          method: "POST",
          body: publishAt ? { publish_at: publishAt } : undefined,
          token,
          cache: "no-store",
        }),
      unpublishMenu: (token: string, id: string) =>
        request<AdminMenu>(`/admin/menus/${encodeURIComponent(id)}/unpublish`, { method: "POST", token, cache: "no-store" }),
      subscriptions: (token: string, query: { status?: string; customer_id?: string; page?: number } = {}) =>
        request<{ items: AdminSubscriptionRow[] }>("/admin/subscriptions", { token, query, cache: "no-store" }),
      subscription: (token: string, id: string) =>
        request<AdminSubscription>(`/admin/subscriptions/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      orders: (token: string, query: { week?: string; status?: string; customer_id?: string } = {}) =>
        request<{ items: OrderSummary[] }>("/admin/orders", { token, query, cache: "no-store" }),
      order: (token: string, id: string) => request<Order>(`/admin/orders/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      demand: (token: string, week?: string) => request<Demand>("/admin/demand", { token, query: { week }, cache: "no-store" }),
      invoice: (token: string, id: string) => request<Invoice>(`/admin/invoices/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      payments: (token: string, query: { status?: string; attention?: boolean; customer_id?: string } = {}) =>
        request<{ items: Payment[] }>("/admin/payments", { token, query, cache: "no-store" }),
      payment: (token: string, id: string) =>
        request<PaymentDetail>(`/admin/payments/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      refund: (token: string, id: string, input: RefundInput) =>
        request<PaymentDetail>(`/admin/payments/${encodeURIComponent(id)}/refunds`, { method: "POST", body: input, token, cache: "no-store" }),
      webhooks: (token: string, status?: string) =>
        request<{ items: StoredWebhook[] }>("/admin/payment-webhooks", { token, query: { status }, cache: "no-store" }),
      promotions: (token: string) => request<{ items: Promotion[] }>("/admin/promotions", { token, cache: "no-store" }),
      createPromotion: (token: string, input: PromotionInput) =>
        request<Promotion>("/admin/promotions", { method: "POST", body: input, token, cache: "no-store" }),
      setPromotionStatus: (token: string, id: string, action: "activate" | "archive") =>
        request<Promotion>(`/admin/promotions/${encodeURIComponent(id)}/${action}`, { method: "POST", token, cache: "no-store" }),
      failedJobs: (token: string) => request<{ items: FailedJob[] }>("/admin/commerce/jobs", { token, cache: "no-store" }),

      // Sprint 4: administration and CRM.
      dashboard: (token: string) => request<Dashboard>("/admin/dashboard", { token, cache: "no-store" }),
      report: (token: string, kind: string, from: string, to: string) =>
        request<Report>(`/admin/reports/${encodeURIComponent(kind)}`, { token, query: { from, to }, cache: "no-store" }),
      customers: (token: string, filter: CustomerFilter = {}) =>
        request<CustomerPage>("/admin/customers", { token, query: filter, cache: "no-store" }),
      customer: (token: string, id: string) => request<Customer360>(`/admin/customers/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      customerOrders: (token: string, id: string) =>
        request<{ items: OrderSummary[] }>(`/admin/customers/${encodeURIComponent(id)}/orders`, { token, cache: "no-store" }),
      customerBilling: (token: string, id: string) =>
        request<CustomerBilling>(`/admin/customers/${encodeURIComponent(id)}/billing`, { token, cache: "no-store" }),
      customerSensitive: (token: string, id: string) =>
        request<CustomerSensitive>(`/admin/customers/${encodeURIComponent(id)}/preferences`, { token, cache: "no-store" }),
      customerActivity: (token: string, id: string) =>
        request<{ items: ActivityEntry[] }>(`/admin/customers/${encodeURIComponent(id)}/activity`, { token, cache: "no-store" }),
      notes: (token: string, subject: "customers" | "leads", id: string) =>
        request<{ items: CrmNote[] }>(`/admin/${subject}/${encodeURIComponent(id)}/notes`, { token, cache: "no-store" }),
      addNote: (token: string, subject: "customers" | "leads", id: string, body: string) =>
        request<CrmNote>(`/admin/${subject}/${encodeURIComponent(id)}/notes`, { method: "POST", body: { body }, token, cache: "no-store" }),
      leadCRM: (token: string, id: string) => request<LeadCRM>(`/admin/leads/${encodeURIComponent(id)}/crm`, { token, cache: "no-store" }),
      setLeadCRM: (token: string, id: string, stage: string, assignedTo: string | null) =>
        request<LeadCRM>(`/admin/leads/${encodeURIComponent(id)}/crm`, {
          method: "PUT",
          body: { stage, assigned_to: assignedTo },
          token,
          cache: "no-store",
        }),
      segments: (token: string) => request<{ items: Segment[] }>("/admin/segments", { token, cache: "no-store" }),
      segment: (token: string, id: string) =>
        request<{ segment: Segment; preview: CustomerPage }>(`/admin/segments/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      saveSegment: (token: string, input: SegmentInput, id?: string) =>
        request<Segment>(id ? `/admin/segments/${encodeURIComponent(id)}` : "/admin/segments", {
          method: id ? "PUT" : "POST",
          body: input,
          token,
          cache: "no-store",
        }),
      deleteSegment: (token: string, id: string) =>
        request<{ deleted: boolean }>(`/admin/segments/${encodeURIComponent(id)}`, { method: "DELETE", token, cache: "no-store" }),
      previewSegment: (token: string, rules: SegmentRules) =>
        request<CustomerPage>("/admin/segments/preview", { method: "POST", body: rules, token, cache: "no-store" }),
      campaigns: (token: string) => request<{ items: Campaign[] }>("/admin/campaigns", { token, cache: "no-store" }),
      campaign: (token: string, id: string) =>
        request<{ campaign: Campaign; attribution: Attribution }>(`/admin/campaigns/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      saveCampaign: (token: string, input: CampaignInput, id?: string) =>
        request<Campaign>(id ? `/admin/campaigns/${encodeURIComponent(id)}` : "/admin/campaigns", {
          method: id ? "PUT" : "POST",
          body: input,
          token,
          cache: "no-store",
        }),
      setCampaignStatus: (token: string, id: string, status: string) =>
        request<Campaign>(`/admin/campaigns/${encodeURIComponent(id)}/status`, { method: "POST", body: { status }, token, cache: "no-store" }),
      referralsReview: (token: string, flagged = false, page = 1) =>
        request<{ items: ReferralRow[]; total: number }>("/admin/referrals", { token, query: { flagged, page }, cache: "no-store" }),
      retention: (token: string) =>
        request<{ items: RetentionSignal[]; rules: Record<string, string> }>("/admin/retention", { token, cache: "no-store" }),
      cases: (token: string, filter: CaseFilter = {}) =>
        request<SupportCasePage>("/admin/support/cases", { token, query: filter, cache: "no-store" }),
      case: (token: string, id: string) => request<SupportCase>(`/admin/support/cases/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createCase: (token: string, input: CaseInput) =>
        request<SupportCase>("/admin/support/cases", { method: "POST", body: input, token, cache: "no-store" }),
      updateCase: (token: string, id: string, input: CaseUpdate) =>
        request<SupportCase>(`/admin/support/cases/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      caseNote: (token: string, id: string, body: string) =>
        request<SupportCase>(`/admin/support/cases/${encodeURIComponent(id)}/notes`, { method: "POST", body: { body }, token, cache: "no-store" }),
      subscriptionAction: (token: string, id: string, action: "pause" | "resume" | "cancel", input: StaffActionInput) =>
        request<{ subscription: Subscription | null }>(`/admin/subscriptions/${encodeURIComponent(id)}/${action}`, {
          method: "POST",
          body: input,
          token,
          cache: "no-store",
        }),
      invoices: (token: string, query: { status?: string; q?: string; page?: number } = {}) =>
        request<{ items: InvoiceRow[]; total: number }>("/admin/invoices", { token, query, cache: "no-store" }),
      exports: (token: string) => request<{ items: ExportJob[]; limit: number }>("/admin/exports", { token, cache: "no-store" }),
      requestExport: (token: string, input: ExportRequest) =>
        request<ExportJob>("/admin/exports", { method: "POST", body: input, token, cache: "no-store" }),
      /** The raw CSV response, to stream to the browser. */
      downloadExport: (token: string, id: string) =>
        send(`/admin/exports/${encodeURIComponent(id)}/download`, { token, cache: "no-store" }),
      alerts: (token: string, unread = false) =>
        request<{ items: AdminAlert[]; unread: number }>("/admin/alerts", { token, query: { unread: unread || undefined }, cache: "no-store" }),
      readAlerts: (token: string, ids: number[] = []) =>
        request<{ read: boolean }>("/admin/alerts/read", { method: "POST", body: { ids }, token, cache: "no-store" }),
      settings: (token: string) => request<Settings>("/admin/settings", { token, cache: "no-store" }),
      putSettings: (token: string, values: Record<string, string>) =>
        request<Settings>("/admin/settings", { method: "PUT", body: values, token, cache: "no-store" }),
      integrations: (token: string) => request<{ items: IntegrationStatus[] }>("/admin/integrations", { token, cache: "no-store" }),
      audit: (token: string, filter: AuditFilter = {}) =>
        request<{ items: AuditEntry[]; total: number }>("/admin/audit-events", { token, query: filter, cache: "no-store" }),
      staff: (token: string) =>
        request<{ items: StaffMember[]; invitations: StaffInvitation[] }>("/admin/staff", { token, cache: "no-store" }),
      invite: (token: string, input: { email: string; name: string; role: string }) =>
        request<StaffInvitation>("/admin/staff/invitations", { method: "POST", body: input, token, cache: "no-store" }),
      revokeInvitation: (token: string, id: string) =>
        request<{ revoked: boolean }>(`/admin/staff/invitations/${encodeURIComponent(id)}`, { method: "DELETE", token, cache: "no-store" }),
      staffAction: (token: string, id: string, action: "roles" | "roles/revoke" | "disable" | "enable" | "signout", input: { role?: string; reason: string }) =>
        request<Record<string, unknown>>(`/admin/staff/${encodeURIComponent(id)}/${action}`, { method: "POST", body: input, token, cache: "no-store" }),
      roles: (token: string) => request<{ roles: RoleInfo[]; permissions: PermissionInfo[] }>("/admin/roles", { token, cache: "no-store" }),
      acceptInvitation: (invitationToken: string, password: string) =>
        request<{ accepted: boolean }>("/admin/auth/invitations/accept", {
          method: "POST",
          body: { token: invitationToken, password },
          cache: "no-store",
        }),
    },

    /** Sprint 5: culinary (ingredients, recipes, reviews) and production. Server-side only. */
    /** Sprint 6: suppliers, procurement, purchasing, receiving, inventory and traceability. Admin session; permissions per route. */
    supply: {
      suppliers: (token: string, query: { q?: string; type?: string; status?: string; farmers?: boolean; page?: number; page_size?: number } = {}) =>
        request<SupplierPage>("/admin/suppliers", { token, query, cache: "no-store" }),
      supplier: (token: string, id: string) => request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createSupplier: (token: string, input: SupplierInput) => request<Supplier>("/admin/suppliers", { method: "POST", body: input, token, cache: "no-store" }),
      updateSupplier: (token: string, id: string, input: SupplierInput) =>
        request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      supplierAction: (token: string, id: string, action: "submit" | "suspend" | "reactivate" | "deactivate", input: SupplierActionInput = {}) =>
        request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}/${action}`, { method: "POST", body: input, token, cache: "no-store" }),
      reviewSupplier: (token: string, id: string, input: SupplierReviewInput) =>
        request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}/review`, { method: "POST", body: input, token, cache: "no-store" }),
      saveFarm: (token: string, id: string, input: FarmInput) =>
        request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}/farm`, { method: "PUT", body: input, token, cache: "no-store" }),
      addDocument: (token: string, id: string, input: SupplierDocumentInput) =>
        request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}/documents`, { method: "POST", body: input, token, cache: "no-store" }),
      verifyDocument: (token: string, id: string) =>
        request<Supplier>(`/admin/supplier-documents/${encodeURIComponent(id)}/verify`, { method: "POST", token, cache: "no-store" }),
      logCommunication: (token: string, id: string, input: CommunicationInput) =>
        request<Supplier>(`/admin/suppliers/${encodeURIComponent(id)}/communications`, { method: "POST", body: input, token, cache: "no-store" }),
      criteria: (token: string) => request<{ items: SupplierCriterion[] }>("/admin/supplier-criteria", { token, cache: "no-store" }),
      saveCriterion: (token: string, id: string | null, input: CriterionInput) =>
        id
          ? request<{ items: SupplierCriterion[] }>(`/admin/supplier-criteria/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" })
          : request<{ items: SupplierCriterion[] }>("/admin/supplier-criteria", { method: "POST", body: input, token, cache: "no-store" }),
      products: (token: string, query: { supplier_id?: string; ingredient_id?: string; mapping?: string; q?: string; purchasable?: boolean } = {}) =>
        request<{ items: SupplierProduct[] }>("/admin/supplier-products", { token, query, cache: "no-store" }),
      product: (token: string, id: string) => request<SupplierProduct>(`/admin/supplier-products/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createProduct: (token: string, supplierId: string, input: SupplierProductInput) =>
        request<SupplierProduct>(`/admin/suppliers/${encodeURIComponent(supplierId)}/products`, { method: "POST", body: input, token, cache: "no-store" }),
      updateProduct: (token: string, id: string, input: SupplierProductInput) =>
        request<SupplierProduct>(`/admin/supplier-products/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      reviewMapping: (token: string, id: string, input: MappingInput) =>
        request<SupplierProduct>(`/admin/supplier-products/${encodeURIComponent(id)}/mapping`, { method: "POST", body: input, token, cache: "no-store" }),
      addPrice: (token: string, id: string, input: SupplierPriceInput) =>
        request<SupplierProduct>(`/admin/supplier-products/${encodeURIComponent(id)}/prices`, { method: "POST", body: input, token, cache: "no-store" }),
      availability: (token: string, query: { supplier_id?: string; ingredient_id?: string; current?: boolean } = {}) =>
        request<{ items: SupplierAvailability[] }>("/admin/supplier-availability", { token, query, cache: "no-store" }),
      recordAvailability: (token: string, input: AvailabilityInput) =>
        request<{ items: SupplierAvailability[] }>("/admin/supplier-availability", { method: "POST", body: input, token, cache: "no-store" }),

      runs: (token: string, status?: string) => request<{ items: ProcurementRun[] }>("/admin/procurement/runs", { token, query: { status }, cache: "no-store" }),
      run: (token: string, id: string) => request<ProcurementRun>(`/admin/procurement/runs/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createRun: (token: string, planId: string) =>
        request<ProcurementRun>("/admin/procurement/runs", { method: "POST", body: { plan_id: planId }, token, cache: "no-store" }),
      decide: (token: string, runId: string, lineId: string, input: DecisionInput) =>
        request<ProcurementRun>(`/admin/procurement/runs/${encodeURIComponent(runId)}/lines/${encodeURIComponent(lineId)}/decision`, { method: "POST", body: input, token, cache: "no-store" }),
      approveRun: (token: string, id: string) =>
        request<ProcurementRun>(`/admin/procurement/runs/${encodeURIComponent(id)}/approve`, { method: "POST", token, cache: "no-store" }),
      reopenRun: (token: string, id: string, reason: string) =>
        request<ProcurementRun>(`/admin/procurement/runs/${encodeURIComponent(id)}/reopen`, { method: "POST", body: { reason }, token, cache: "no-store" }),
      ordersFromRun: (token: string, id: string) =>
        request<{ items: PurchaseOrder[] }>(`/admin/procurement/runs/${encodeURIComponent(id)}/purchase-orders`, { method: "POST", token, cache: "no-store" }),
      pendingPlans: (token: string) => request<{ items: ProductionPlanRef[] }>("/admin/procurement/pending-plans", { token, cache: "no-store" }),
      deliveries: (token: string) => request<EventDeliveries>("/admin/procurement/deliveries", { token, cache: "no-store" }),
      procurementSettings: (token: string) => request<ProcurementSettings>("/admin/procurement/settings", { token, cache: "no-store" }),
      saveProcurementSettings: (token: string, input: ProcurementSettingsInput) =>
        request<ProcurementSettings>("/admin/procurement/settings", { method: "PUT", body: input, token, cache: "no-store" }),
      rfqs: (token: string, status?: string) => request<{ items: RFQ[] }>("/admin/procurement/rfqs", { token, query: { status }, cache: "no-store" }),
      rfq: (token: string, id: string) => request<RFQ>(`/admin/procurement/rfqs/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createRFQ: (token: string, input: RFQInput) => request<RFQ>("/admin/procurement/rfqs", { method: "POST", body: input, token, cache: "no-store" }),
      recordInvitation: (token: string, id: string, input: RFQInviteInput) =>
        request<RFQ>(`/admin/procurement/rfqs/${encodeURIComponent(id)}/invitations`, { method: "POST", body: input, token, cache: "no-store" }),
      endRFQ: (token: string, id: string, action: "close" | "cancel") =>
        request<RFQ>(`/admin/procurement/rfqs/${encodeURIComponent(id)}/${action}`, { method: "POST", token, cache: "no-store" }),
      quotations: (token: string) => request<{ items: Quotation[] }>("/admin/procurement/quotations", { token, cache: "no-store" }),
      recordQuotation: (token: string, input: QuotationInput) =>
        request<Quotation>("/admin/procurement/quotations", { method: "POST", body: input, token, cache: "no-store" }),

      purchaseOrders: (token: string, query: { status?: string; supplier_id?: string; run_id?: string; q?: string; receivable?: boolean } = {}) =>
        request<{ items: PurchaseOrder[] }>("/admin/purchase-orders", { token, query, cache: "no-store" }),
      purchaseOrder: (token: string, id: string) => request<PurchaseOrder>(`/admin/purchase-orders/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createPurchaseOrder: (token: string, input: POInput) =>
        request<PurchaseOrder>("/admin/purchase-orders", { method: "POST", body: input, token, cache: "no-store" }),
      updatePurchaseOrder: (token: string, id: string, input: POInput) =>
        request<PurchaseOrder>(`/admin/purchase-orders/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      purchaseOrderAction: (token: string, id: string, action: "submit" | "approve" | "reject" | "issue" | "cancel" | "close", body?: object) =>
        request<PurchaseOrder>(`/admin/purchase-orders/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),

      stock: (token: string, q?: string) => request<StockView>("/admin/inventory", { token, query: { q }, cache: "no-store" }),
      lots: (token: string, query: { ingredient_id?: string; status?: string; location_id?: string; supplier_id?: string; q?: string; held?: boolean } = {}) =>
        request<{ items: InventoryLot[] }>("/admin/inventory/lots", { token, query, cache: "no-store" }),
      lot: (token: string, id: string) => request<InventoryLot>(`/admin/inventory/lots/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      lotAction: (token: string, id: string, action: "status" | "expiry" | "transfer" | "waste" | "supplier-return", body: object) =>
        request<InventoryLot>(`/admin/inventory/lots/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      adjust: (token: string, input: AdjustmentInput) =>
        request<InventoryLot>("/admin/inventory/adjustments", { method: "POST", body: input, token, cache: "no-store" }),
      movements: (token: string, query: { lot_id?: string; ingredient_id?: string; type?: string; batch_id?: string; from?: string; to?: string } = {}) =>
        request<{ items: InventoryMovement[] }>("/admin/inventory/movements", { token, query, cache: "no-store" }),
      reservations: (token: string, query: { plan_id?: string; ingredient_id?: string; lot_id?: string; status?: string } = {}) =>
        request<{ items: InventoryReservation[] }>("/admin/inventory/reservations", { token, query, cache: "no-store" }),
      reserve: (token: string, input: ReserveInput) =>
        request<ReserveResult>("/admin/inventory/reservations", { method: "POST", body: input, token, cache: "no-store" }),
      releaseReservation: (token: string, id: string, reason: string) =>
        request<InventoryReservation>(`/admin/inventory/reservations/${encodeURIComponent(id)}/release`, { method: "POST", body: { reason }, token, cache: "no-store" }),
      issue: (token: string, input: IssueStockInput) => request<IssueResult>("/admin/inventory/issues", { method: "POST", body: input, token, cache: "no-store" }),
      returnStock: (token: string, input: ReturnStockInput) =>
        request<BatchLotUse>("/admin/inventory/returns", { method: "POST", body: input, token, cache: "no-store" }),
      reconciliation: (token: string, batchId: string) =>
        request<Reconciliation>(`/admin/inventory/batches/${encodeURIComponent(batchId)}/reconciliation`, { token, cache: "no-store" }),
      counts: (token: string) => request<{ items: StockCount[] }>("/admin/inventory/counts", { token, cache: "no-store" }),
      count: (token: string, id: string) => request<StockCount>(`/admin/inventory/counts/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      startCount: (token: string, locationId: string, notes = "") =>
        request<StockCount>("/admin/inventory/counts", { method: "POST", body: { location_id: locationId, notes }, token, cache: "no-store" }),
      recordCounts: (token: string, id: string, lines: { lot_id: string; counted: string }[]) =>
        request<StockCount>(`/admin/inventory/counts/${encodeURIComponent(id)}/lines`, { method: "PUT", body: { lines }, token, cache: "no-store" }),
      countAction: (token: string, id: string, action: "submit" | "post" | "cancel") =>
        request<StockCount>(`/admin/inventory/counts/${encodeURIComponent(id)}/${action}`, { method: "POST", token, cache: "no-store" }),
      locations: (token: string) => request<{ items: StorageLocation[] }>("/admin/inventory/locations", { token, cache: "no-store" }),
      saveLocation: (token: string, id: string | null, input: LocationInput) =>
        id
          ? request<{ items: StorageLocation[] }>(`/admin/inventory/locations/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" })
          : request<{ items: StorageLocation[] }>("/admin/inventory/locations", { method: "POST", body: input, token, cache: "no-store" }),
      policies: (token: string) => request<{ items: InventoryPolicy[] }>("/admin/inventory/policies", { token, cache: "no-store" }),
      savePolicy: (token: string, input: InventoryPolicyInput) =>
        request<{ items: InventoryPolicy[] }>("/admin/inventory/policies", { method: "PUT", body: input, token, cache: "no-store" }),
      receipts: (token: string, poId?: string) => request<{ items: GoodsReceipt[] }>("/admin/receiving", { token, query: { po_id: poId }, cache: "no-store" }),
      receipt: (token: string, id: string) => request<GoodsReceipt>(`/admin/receiving/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      receive: (token: string, input: ReceiptInput) => request<GoodsReceipt>("/admin/receiving", { method: "POST", body: input, token, cache: "no-store" }),
      checklist: (token: string) => request<{ items: ReceivingChecklistItem[] }>("/admin/receiving/checklist", { token, cache: "no-store" }),
      saveChecklistItem: (token: string, id: string | null, input: ReceivingChecklistInput) =>
        id
          ? request<{ items: ReceivingChecklistItem[] }>(`/admin/receiving/checklist/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" })
          : request<{ items: ReceivingChecklistItem[] }>("/admin/receiving/checklist", { method: "POST", body: input, token, cache: "no-store" }),
      trace: (token: string, query: { lot?: string; batch?: string; supplier?: string; purchase_order?: string }) =>
        request<Trace>("/admin/traceability", { token, query, cache: "no-store" }),
      report: (token: string, kind: "procurement" | "inventory" | "suppliers", from: string, to: string) =>
        request<Report>(`/admin/reports/${kind}`, { token, query: { from, to }, cache: "no-store" }),
    },
    kitchen: {
      units: (token: string) => request<{ items: Unit[] }>("/admin/units", { token, cache: "no-store" }),
      ingredients: (token: string, query: { q?: string; category?: string; active?: string; page?: number; page_size?: number } = {}) =>
        request<IngredientPage>("/admin/ingredients", { token, query, cache: "no-store" }),
      ingredient: (token: string, id: string) => request<Ingredient>(`/admin/ingredients/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createIngredient: (token: string, input: IngredientInput) =>
        request<Ingredient>("/admin/ingredients", { method: "POST", body: input, token, cache: "no-store" }),
      updateIngredient: (token: string, id: string, input: IngredientInput) =>
        request<Ingredient>(`/admin/ingredients/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      setConversion: (token: string, id: string, input: ConversionInput) =>
        request<Ingredient>(`/admin/ingredients/${encodeURIComponent(id)}/conversions`, { method: "POST", body: input, token, cache: "no-store" }),
      costReferences: (token: string, kind: "ingredient" | "packaging" = "packaging") =>
        request<{ items: CostRef[] }>("/admin/cost-references", { token, query: { kind }, cache: "no-store" }),
      addCost: (token: string, input: CostInput) =>
        request<CostRef>("/admin/cost-references", { method: "POST", body: input, token, cache: "no-store" }),
      chefs: (token: string) => request<{ items: Chef[] }>("/admin/chefs/team", { token, cache: "no-store" }),
      updateChef: (token: string, id: string, input: ChefTeamInput) =>
        request<{ items: Chef[] }>(`/admin/chefs/${encodeURIComponent(id)}/team`, { method: "PATCH", body: input, token, cache: "no-store" }),
      recipes: (token: string, query: { q?: string; status?: string; mine?: boolean; chef_id?: string; page?: number } = {}) =>
        request<RecipePage>("/admin/recipes", { token, query: { ...query, mine: query.mine || undefined }, cache: "no-store" }),
      queue: (token: string, stage: "culinary" | "nutrition" | "final" | "publish") =>
        request<{ items: QueueItem[] }>("/admin/recipes/queue", { token, query: { stage }, cache: "no-store" }),
      recipe: (token: string, id: string) => request<Recipe>(`/admin/recipes/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createRecipe: (token: string, input: RecipeInput) =>
        request<Recipe>("/admin/recipes", { method: "POST", body: input, token, cache: "no-store" }),
      updateRecipe: (token: string, id: string, input: RecipeInput) =>
        request<Recipe>(`/admin/recipes/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      version: (token: string, id: string, n = 0) =>
        request<RecipeVersion>(`/admin/recipes/${encodeURIComponent(id)}/versions/${n}`, { token, cache: "no-store" }),
      saveDraft: (token: string, id: string, input: DraftInput) =>
        request<RecipeVersion>(`/admin/recipes/${encodeURIComponent(id)}/draft`, { method: "PUT", body: input, token, cache: "no-store" }),
      recipeAction: (
        token: string,
        id: string,
        action: "submit" | "review" | "request-changes" | "approve" | "publish" | "archive" | "revise",
        body: Record<string, unknown> = {},
      ) => request<RecipeVersion>(`/admin/recipes/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      nutritionQueue: (token: string) => request<{ items: QueueItem[] }>("/admin/nutrition/reviews", { token, cache: "no-store" }),
      nutritionAction: (token: string, id: string, action: "approve" | "request-changes", body: Record<string, unknown> = {}) =>
        request<RecipeVersion>(`/admin/nutrition/reviews/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      costing: (token: string, id: string, version = 0) =>
        request<Costing>(`/admin/recipes/${encodeURIComponent(id)}/costing`, { token, query: { version: version || undefined }, cache: "no-store" }),

      overview: (token: string) => request<ProductionOverview>("/admin/production/overview", { token, cache: "no-store" }),
      calendar: (token: string, from?: string, days?: number) =>
        request<ProductionCalendar>("/admin/production/calendar", { token, query: { from, days }, cache: "no-store" }),
      demand: (token: string, week?: string) => request<ProductionDemand>("/admin/production/demand", { token, query: { week }, cache: "no-store" }),
      snapshot: (token: string, week: string) =>
        request<{ snapshot: DemandSnapshot; created: boolean }>("/admin/production/snapshots", { method: "POST", body: { week }, token, cache: "no-store" }),
      plans: (token: string, status?: string) => request<{ items: ProductionPlan[] }>("/admin/production/plans", { token, query: { status }, cache: "no-store" }),
      plan: (token: string, id: string) => request<ProductionPlan>(`/admin/production/plans/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createPlan: (token: string, input: { week: string; production_date?: string; notes?: string }) =>
        request<ProductionPlan>("/admin/production/plans", { method: "POST", body: input, token, cache: "no-store" }),
      adjustItem: (token: string, id: string, item: string, input: { planned_kits: number; reason?: string }) =>
        request<ProductionPlan>(`/admin/production/plans/${encodeURIComponent(id)}/items/${encodeURIComponent(item)}`, {
          method: "PATCH",
          body: input,
          token,
          cache: "no-store",
        }),
      planAction: (token: string, id: string, action: "approve" | "release" | "cancel", body?: { reason: string }) =>
        request<ProductionPlan>(`/admin/production/plans/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      requirements: (token: string, query: { plan_id?: string; from?: string; to?: string } = {}) =>
        request<{ items: RequirementRow[] }>("/admin/production/requirements", { token, query, cache: "no-store" }),
      /** The raw CSV response, to stream to the browser. */
      requirementsCsv: (token: string, query: { plan_id?: string; from?: string; to?: string } = {}) =>
        send("/admin/production/requirements", { token, query: { ...query, format: "csv" }, cache: "no-store" }),
      batches: (token: string, query: { plan_id?: string; status?: string; date?: string; from?: string; to?: string; open?: boolean } = {}) =>
        request<{ items: Batch[] }>("/admin/production/batches", { token, query: { ...query, open: query.open || undefined }, cache: "no-store" }),
      batch: (token: string, id: string) => request<Batch>(`/admin/production/batches/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      batchAction: (token: string, id: string, action: "start" | "output" | "complete" | "cancel", body?: Record<string, unknown>) =>
        request<Batch>(`/admin/production/batches/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      packaging: (token: string, id: string, input: PackagingInput) =>
        request<Batch>(`/admin/production/batches/${encodeURIComponent(id)}/packaging`, { method: "PUT", body: input, token, cache: "no-store" }),
      label: (token: string, id: string) => request<KitLabel>(`/admin/production/batches/${encodeURIComponent(id)}/label`, { token, cache: "no-store" }),
      tasks: (token: string, query: { batch_id?: string; date?: string; status?: string; mine?: boolean; all?: boolean } = {}) =>
        request<{ items: KitchenTask[] }>("/admin/production/tasks", {
          token,
          query: { ...query, mine: query.mine || undefined, all: query.all || undefined },
          cache: "no-store",
        }),
      updateTask: (token: string, id: string, input: TaskInput) =>
        request<KitchenTask>(`/admin/production/tasks/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      taskTemplates: (token: string) => request<{ items: TaskTemplate[] }>("/admin/production/task-templates", { token, cache: "no-store" }),
      saveTaskTemplate: (token: string, input: TaskTemplateInput, id?: string) =>
        request<{ items: TaskTemplate[] }>(id ? `/admin/production/task-templates/${encodeURIComponent(id)}` : "/admin/production/task-templates", {
          method: id ? "PATCH" : "POST",
          body: input,
          token,
          cache: "no-store",
        }),
      settings: (token: string) => request<ProductionSettings>("/admin/production/settings", { token, cache: "no-store" }),
      saveSettings: (token: string, input: ProductionSettingsInput) =>
        request<ProductionSettings>("/admin/production/settings", { method: "PUT", body: input, token, cache: "no-store" }),
      waste: (token: string, query: { batch_id?: string; from?: string; to?: string } = {}) =>
        request<{ items: Waste[] }>("/admin/production/waste", { token, query, cache: "no-store" }),
      recordWaste: (token: string, input: WasteInput) =>
        request<Waste>("/admin/production/waste", { method: "POST", body: input, token, cache: "no-store" }),
      qualityTemplates: (token: string) => request<{ items: QualityTemplate[] }>("/admin/quality/templates", { token, cache: "no-store" }),
      saveQualityTemplate: (token: string, input: QualityTemplateInput, id?: string) =>
        request<{ items: QualityTemplate[] }>(id ? `/admin/quality/templates/${encodeURIComponent(id)}` : "/admin/quality/templates", {
          method: id ? "PATCH" : "POST",
          body: input,
          token,
          cache: "no-store",
        }),
      recordCheck: (token: string, input: { batch_id: string; template_id: string; result: "pass" | "fail"; notes?: string; evidence_ref?: string }) =>
        request<Batch>("/admin/quality/checks", { method: "POST", body: input, token, cache: "no-store" }),
      holds: (token: string, open = false) =>
        request<{ items: QualityHold[] }>("/admin/quality/holds", { token, query: { open: open || undefined }, cache: "no-store" }),
      placeHold: (token: string, input: { batch_id: string; reason: string }) =>
        request<Batch>("/admin/quality/holds", { method: "POST", body: input, token, cache: "no-store" }),
      resolveHold: (token: string, id: string, input: { decision: "release" | "discard"; notes: string }) =>
        request<Batch>(`/admin/quality/holds/${encodeURIComponent(id)}/release`, { method: "POST", body: input, token, cache: "no-store" }),
      productionReport: (token: string, from: string, to: string) =>
        request<Report>("/admin/reports/production", { token, query: { from, to }, cache: "no-store" }),
    },

    /** Sprint 7: fulfillment and delivery. Admin session; permissions per route (couriers see only their own runs). */
    delivery: {
      fulfillments: (token: string, query: { status?: string; week?: string; q?: string } = {}) =>
        request<{ items: Fulfillment[] }>("/admin/fulfillment", { token, query, cache: "no-store" }),
      fulfillment: (token: string, id: string) => request<Fulfillment>(`/admin/fulfillment/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      syncFulfillment: (token: string) => request<{ items: FulfillmentOutcome[] }>("/admin/fulfillment/sync", { method: "POST", token, cache: "no-store" }),
      consumer: (token: string) => request<ConsumerStatus>("/admin/fulfillment/consumer", { token, cache: "no-store" }),
      verify: (token: string, id: string, input: VerifyInput) =>
        request<Fulfillment>(`/admin/fulfillment/${encodeURIComponent(id)}/verify`, { method: "POST", body: input, token, cache: "no-store" }),
      releaseFulfillment: (token: string, id: string, version?: number) =>
        request<Fulfillment>(`/admin/fulfillment/${encodeURIComponent(id)}/release`, { method: "POST", body: { version }, token, cache: "no-store" }),
      labels: (token: string, id: string) => request<{ items: PackageLabel[] }>(`/admin/fulfillment/${encodeURIComponent(id)}/labels`, { token, cache: "no-store" }),
      fulfillmentTrace: (token: string, id: string) => request<FulfillmentTrace>(`/admin/fulfillment/${encodeURIComponent(id)}/trace`, { token, cache: "no-store" }),
      flagBatch: (token: string, id: string, reason: string) =>
        request<{ exception_id: string }>(`/admin/fulfillment/batches/${encodeURIComponent(id)}/flag`, { method: "POST", body: { reason }, token, cache: "no-store" }),

      overview: (token: string, date?: string) => request<DeliveryOverview>("/admin/delivery/overview", { token, query: { date }, cache: "no-store" }),
      report: (token: string, from: string, to: string) => request<DeliveryReport>("/admin/reports/delivery", { token, query: { from, to }, cache: "no-store" }),
      settings: (token: string) => request<DeliverySettings>("/admin/delivery/settings", { token, cache: "no-store" }),
      saveSettings: (token: string, input: DeliverySettingsInput) =>
        request<DeliverySettings>("/admin/delivery/settings", { method: "PUT", body: input, token, cache: "no-store" }),
      zones: (token: string) => request<{ items: DeliveryZone[] }>("/admin/delivery/zones", { token, cache: "no-store" }),
      saveZone: (token: string, id: string | null, input: DeliveryZoneInput) =>
        id
          ? request<DeliveryZone>(`/admin/delivery/zones/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" })
          : request<DeliveryZone>("/admin/delivery/zones", { method: "POST", body: input, token, cache: "no-store" }),
      coverage: (token: string) => request<{ items: RegionCoverage[] }>("/admin/delivery/coverage", { token, cache: "no-store" }),
      calendar: (token: string, from?: string, days?: number) =>
        request<{ items: ServiceDay[] }>("/admin/delivery/calendar", { token, query: { from, days }, cache: "no-store" }),
      saveServiceDay: (token: string, input: ServiceDayInput) =>
        request<ServiceDay>("/admin/delivery/calendar", { method: "PUT", body: input, token, cache: "no-store" }),
      capacity: (token: string, date: string) => request<{ items: DeliveryCapacity[] }>("/admin/delivery/capacity", { token, query: { date }, cache: "no-store" }),
      setCapacity: (token: string, input: DeliveryCapacityInput) =>
        request<{ items: DeliveryCapacity[] }>("/admin/delivery/capacity", { method: "PUT", body: input, token, cache: "no-store" }),
      partners: (token: string) => request<{ items: DeliveryPartner[] }>("/admin/delivery/partners", { token, cache: "no-store" }),
      partner: (token: string, id: string) => request<DeliveryPartner>(`/admin/delivery/partners/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      savePartner: (token: string, id: string | null, input: DeliveryPartnerInput) =>
        id
          ? request<DeliveryPartner>(`/admin/delivery/partners/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" })
          : request<DeliveryPartner>("/admin/delivery/partners", { method: "POST", body: input, token, cache: "no-store" }),
      partnerCriterion: (token: string, id: string, input: PartnerCriterionInput) =>
        request<DeliveryPartner>(`/admin/delivery/partners/${encodeURIComponent(id)}/criteria`, { method: "PUT", body: input, token, cache: "no-store" }),
      partnerStatus: (token: string, id: string, input: PartnerStatusInput) =>
        request<DeliveryPartner>(`/admin/delivery/partners/${encodeURIComponent(id)}/status`, { method: "POST", body: input, token, cache: "no-store" }),
      couriers: (token: string) => request<{ items: Courier[] }>("/admin/delivery/couriers", { token, cache: "no-store" }),
      saveCourier: (token: string, id: string | null, input: CourierInput) =>
        id
          ? request<Courier>(`/admin/delivery/couriers/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" })
          : request<Courier>("/admin/delivery/couriers", { method: "POST", body: input, token, cache: "no-store" }),
      costs: (token: string, runId?: string) => request<{ items: DeliveryCost[] }>("/admin/delivery/costs", { token, query: { run_id: runId }, cache: "no-store" }),
      recordCost: (token: string, input: DeliveryCostInput) =>
        request<DeliveryCost>("/admin/delivery/costs", { method: "POST", body: input, token, cache: "no-store" }),
      exceptions: (token: string, query: { status?: string; stage?: string; delivery_id?: string; customer_id?: string } = {}) =>
        request<{ items: DeliveryException[] }>("/admin/delivery/exceptions", { token, query, cache: "no-store" }),
      exception: (token: string, id: string) => request<DeliveryException>(`/admin/delivery/exceptions/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      raiseException: (token: string, input: DeliveryExceptionInput) =>
        request<DeliveryException>("/admin/delivery/exceptions", { method: "POST", body: input, token, cache: "no-store" }),
      updateException: (token: string, id: string, input: DeliveryExceptionUpdate) =>
        request<DeliveryException>(`/admin/delivery/exceptions/${encodeURIComponent(id)}`, { method: "PATCH", body: input, token, cache: "no-store" }),
      deliveries: (token: string, query: { status?: string; date?: string; zone_id?: string; customer_id?: string; order_id?: string; q?: string } = {}) =>
        request<{ items: Delivery[] }>("/admin/delivery/deliveries", { token, query, cache: "no-store" }),
      delivery: (token: string, id: string) => request<Delivery>(`/admin/delivery/deliveries/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      deliveryAction: (
        token: string,
        id: string,
        action: "schedule" | "events" | "proof" | "deliver" | "fail" | "reattempt" | "return" | "cancel" | "correct" | "reschedule-requests" | "cases",
        body: object,
      ) => request<Delivery>(`/admin/delivery/deliveries/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      correctAddress: (token: string, id: string, input: AddressCorrectionInput) =>
        request<Delivery>(`/admin/delivery/deliveries/${encodeURIComponent(id)}/address`, { method: "PUT", body: input, token, cache: "no-store" }),
      rescheduleRequests: (token: string) => request<{ items: PendingReschedule[] }>("/admin/delivery/reschedule-requests", { token, cache: "no-store" }),
      decideReschedule: (token: string, id: string, input: RescheduleDecisionInput) =>
        request<Delivery>(`/admin/delivery/reschedule-requests/${encodeURIComponent(id)}/decide`, { method: "POST", body: input, token, cache: "no-store" }),
      returns: (token: string, status?: string) => request<{ items: DeliveryReturn[] }>("/admin/delivery/returns", { token, query: { status }, cache: "no-store" }),
      reviewReturn: (token: string, id: string, input: ReturnReviewInput) =>
        request<{ items: DeliveryReturn[] }>(`/admin/delivery/returns/${encodeURIComponent(id)}/review`, { method: "POST", body: input, token, cache: "no-store" }),
      runs: (token: string, query: { date?: string; status?: string } = {}) =>
        request<{ items: DispatchRun[] }>("/admin/delivery/dispatch-runs", { token, query, cache: "no-store" }),
      run: (token: string, id: string) => request<DispatchRun>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      createRun: (token: string, input: DispatchRunInput) =>
        request<DispatchRun>("/admin/delivery/dispatch-runs", { method: "POST", body: input, token, cache: "no-store" }),
      addStop: (token: string, id: string, input: StopInput) =>
        request<DispatchRun>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/stops`, { method: "POST", body: input, token, cache: "no-store" }),
      removeStop: (token: string, id: string, stop: string, reason: string) =>
        request<DispatchRun>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/stops/${encodeURIComponent(stop)}/remove`, { method: "POST", body: { reason }, token, cache: "no-store" }),
      sequence: (token: string, id: string, stopIds: string[]) =>
        request<DispatchRun>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/sequence`, { method: "PUT", body: { stop_ids: stopIds }, token, cache: "no-store" }),
      runAction: (token: string, id: string, action: "finalize" | "reopen" | "release", body: object = {}) =>
        request<DispatchRun>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/${action}`, { method: "POST", body, token, cache: "no-store" }),
      handoff: (token: string, id: string, input: HandoffInput) =>
        request<DispatchRun>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/handoffs`, { method: "POST", body: input, token, cache: "no-store" }),
      manifest: (token: string, id: string) => request<DispatchManifest>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/manifest`, { token, cache: "no-store" }),
      /** The raw CSV response, to stream to the browser. */
      manifestCsv: (token: string, id: string) =>
        send(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/manifest`, { token, query: { format: "csv" }, cache: "no-store" }),
      reconciliation: (token: string, id: string) =>
        request<DeliveryReconciliation>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/reconciliation`, { token, cache: "no-store" }),
      reconcile: (token: string, id: string, explanation: string) =>
        request<DeliveryReconciliation>(`/admin/delivery/dispatch-runs/${encodeURIComponent(id)}/reconcile`, { method: "POST", body: { explanation }, token, cache: "no-store" }),
    },

    /** Plans and published menus. Public: the same for everyone. */
    shop: {
      info: () => request<SubscriptionInfo>("/subscription-info", { cache: "no-store" }),
      plans: () => request<{ items: Plan[] }>("/plans", { cache: "no-store" }),
      plan: (slug: string) => request<Plan>(`/plans/${encodeURIComponent(slug)}`, { cache: "no-store" }),
      menus: () => request<{ items: MenuSummary[] }>("/weekly-menus", { cache: "no-store" }),
      menu: (week: string) => request<WeeklyMenu>(`/weekly-menus/${encodeURIComponent(week)}`, { cache: "no-store" }),
    },

    /** The signed-in customer's subscription, meals, orders and billing. Server-side only. */
    commerce: {
      subscription: (token: string) =>
        request<{ subscription: Subscription | null }>("/me/subscription", { token, cache: "no-store" }),
      quote: (token: string, planId: string, promotionCode?: string) =>
        request<Quote>("/me/subscriptions/quote", {
          method: "POST",
          body: { plan_id: planId, ...(promotionCode ? { promotion_code: promotionCode } : {}) },
          token,
          cache: "no-store",
        }),
      subscribe: (token: string, input: SubscribeInput) =>
        request<SubscribeResult>("/me/subscriptions", { method: "POST", body: input, token, cache: "no-store" }),
      changePlan: (token: string, id: string, planId: string) =>
        request<{ subscription: Subscription | null }>(`/me/subscriptions/${encodeURIComponent(id)}/change-plan`, {
          method: "POST",
          body: { plan_id: planId },
          token,
          cache: "no-store",
        }),
      pause: (token: string, id: string, weeks: number) =>
        request<{ subscription: Subscription | null }>(`/me/subscriptions/${encodeURIComponent(id)}/pause`, {
          method: "POST",
          body: { weeks },
          token,
          cache: "no-store",
        }),
      resume: (token: string, id: string) =>
        request<{ subscription: Subscription | null }>(`/me/subscriptions/${encodeURIComponent(id)}/resume`, {
          method: "POST",
          token,
          cache: "no-store",
        }),
      cancel: (token: string, id: string, reason?: string) =>
        request<{ subscription: Subscription | null }>(`/me/subscriptions/${encodeURIComponent(id)}/cancel`, {
          method: "POST",
          body: reason ? { reason } : undefined,
          token,
          cache: "no-store",
        }),
      setAddress: (token: string, id: string, addressId: string) =>
        request<{ subscription: Subscription | null }>(`/me/subscriptions/${encodeURIComponent(id)}/address`, {
          method: "PUT",
          body: { address_id: addressId },
          token,
          cache: "no-store",
        }),
      weeks: (token: string) => request<{ items: SubscriptionCycle[] }>("/me/subscription-cycles", { token, cache: "no-store" }),
      selection: (token: string, cycleId: string) =>
        request<SelectionPage>(`/me/meal-selections/${encodeURIComponent(cycleId)}`, { token, cache: "no-store" }),
      saveSelection: (token: string, cycleId: string, items: SelectionItem[], version?: number | null) =>
        request<SelectionPage>(`/me/meal-selections/${encodeURIComponent(cycleId)}`, {
          method: "PUT",
          body: { items, version: version ?? null },
          token,
          cache: "no-store",
        }),
      finalizeSelection: (token: string, cycleId: string, items?: SelectionItem[], version?: number | null) =>
        request<SelectionPage>(`/me/meal-selections/${encodeURIComponent(cycleId)}/finalize`, {
          method: "POST",
          body: items ? { items, version: version ?? null } : undefined,
          token,
          cache: "no-store",
        }),
      orders: (token: string, page = 1) =>
        request<{ items: OrderSummary[] }>("/me/orders", { token, query: { page }, cache: "no-store" }),
      order: (token: string, id: string) => request<Order>(`/me/orders/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      billing: (token: string) => request<BillingSummary>("/me/billing", { token, cache: "no-store" }),
      invoices: (token: string, page = 1) =>
        request<{ items: InvoiceSummary[] }>("/me/invoices", { token, query: { page }, cache: "no-store" }),
      invoice: (token: string, id: string) => request<Invoice>(`/me/invoices/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      /** The raw PDF response, to stream to the browser. */
      invoicePdf: (token: string, id: string) =>
        send(`/me/invoices/${encodeURIComponent(id)}/pdf`, { token, cache: "no-store" }),
      pay: (token: string, billingCycleId: string) =>
        request<Checkout>("/checkout/sessions", { method: "POST", body: { billing_cycle_id: billingCycleId }, token, cache: "no-store" }),
      checkout: (token: string, id: string) =>
        request<CheckoutResult>(`/checkout/sessions/${encodeURIComponent(id)}`, { token, cache: "no-store" }),
      /** Sprint 7: the order's delivery (milestones, confirmed date, code while out). */
      orderDelivery: (token: string, id: string) => request<CustomerDelivery>(`/me/orders/${encodeURIComponent(id)}/delivery`, { token, cache: "no-store" }),
      deliverySupport: (token: string, id: string, message: string) =>
        request<DeliverySupportResult>(`/me/orders/${encodeURIComponent(id)}/delivery/support`, { method: "POST", body: { message }, token, cache: "no-store" }),
      requestReschedule: (token: string, id: string, input: { preferred_date?: string; reason: string }) =>
        request<CustomerDelivery>(`/me/orders/${encodeURIComponent(id)}/delivery/reschedule-requests`, { method: "POST", body: input, token, cache: "no-store" }),
    },

    /** The test payment simulator (never available in production). */
    testpay: {
      session: (ref: string) => request<TestpaySession>(`/test-payments/sessions/${encodeURIComponent(ref)}`, { cache: "no-store" }),
      complete: (ref: string, outcome: "approve" | "decline" | "cancel", card: "approve" | "decline") =>
        request<TestpaySession>(`/test-payments/sessions/${encodeURIComponent(ref)}/complete`, {
          method: "POST",
          body: { outcome, card },
          cache: "no-store",
        }),
    },
  };
}

export type MiseClient = ReturnType<typeof createClient>;
