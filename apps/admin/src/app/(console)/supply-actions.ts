"use server";

import { ApiError, type CriterionResult, type InspectionResult, type POLineInput } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "./meals/actions";

/*
 * Sprint 6 actions: suppliers, products, procurement planning, quotations,
 * purchase orders, receiving and inventory. The API checks every permission
 * and rule again (separation of duties, tolerances, locks); these only pass
 * the form along and report the outcome.
 */

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const opt = (form: FormData, key: string) => (form.has(key) ? text(form, key) : undefined);
const all = (form: FormData, key: string) => form.getAll(key).map(String).map((v) => v.trim());
const flag = (form: FormData, key: string) => form.get(key) === "on" || form.get(key) === "true";
const int = (form: FormData, key: string) => {
  const v = text(form, key);
  return v === "" ? undefined : Number.parseInt(v, 10);
};
/** Dollars and cents typed by a person, in cents. */
const cents = (form: FormData, key: string) => {
  const v = text(form, key).replace(/[,\s$]/g, "");
  if (v === "") return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? Math.round(n * 100) : Number.NaN;
};

function failure(error: unknown): CatalogState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") return { error: "Some fields need attention.", fields: error.fields };
    if (error.status === 403) return { error: error.code === "forbidden" ? "Your role does not allow this." : error.message };
    return { error: error.message };
  }
  return { error: "The Mise API could not be reached. Try again in a moment." };
}

async function run(fn: (token: string) => Promise<unknown>, paths: string[], message: string): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await fn(token);
  } catch (e) {
    return failure(e);
  }
  for (const p of paths) revalidatePath(p);
  return { message };
}

// ---------------------------------------------------------------- suppliers

export async function createSupplier(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    const s = await api.supply.createSupplier(token, {
      name: text(form, "name"),
      type: text(form, "type") as never,
      contact_name: text(form, "contact_name"),
      phone: text(form, "phone"),
      email: text(form, "email"),
      preferred_channel: text(form, "preferred_channel") || "phone",
      region: text(form, "region"),
    });
    id = s.id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/suppliers");
  redirect(`/suppliers/${id}`);
}

export async function updateSupplier(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const input: Record<string, unknown> = {};
  for (const k of ["name", "trading_name", "type", "contact_name", "phone", "email", "preferred_channel", "region", "location", "payment_terms", "delivery_arrangements", "notes"]) {
    const v = opt(form, k);
    if (v !== undefined) input[k] = v;
  }
  if (form.has("operating_regions")) input.operating_regions = text(form, "operating_regions").split(/[,\n]/).map((x) => x.trim()).filter(Boolean);
  return run((t) => api.supply.updateSupplier(t, id, input), [`/suppliers/${id}`, "/suppliers"], "Supplier saved.");
}

export async function supplierAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "submit" | "suspend" | "reactivate" | "deactivate";
  const msg = { submit: "Sent for review.", suspend: "Supplier suspended.", reactivate: "Reactivated.", deactivate: "Supplier deactivated." }[action];
  return run((t) => api.supply.supplierAction(t, id, action, { reason: text(form, "reason"), note: text(form, "note") }), [`/suppliers/${id}`, "/suppliers"], msg);
}

export async function reviewSupplier(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const results: CriterionResult[] = all(form, "criterion_id").map((cid) => ({
    criterion_id: cid,
    name: "",
    met: form.get(`met_${cid}`) === "yes",
    note: text(form, `note_${cid}`),
  }));
  const decision = text(form, "decision") as "approved" | "rejected" | "changes_requested";
  const msg = { approved: "Supplier approved.", rejected: "Supplier rejected.", changes_requested: "Sent back as a draft." }[decision] ?? "Reviewed.";
  return run(
    (t) => api.supply.reviewSupplier(t, id, { decision, results, notes: text(form, "notes"), expires_on: text(form, "expires_on") || undefined }),
    [`/suppliers/${id}`, "/suppliers"],
    msg,
  );
}

export async function saveFarm(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.supply.saveFarm(t, id, {
        farm_name: text(form, "farm_name"),
        farm_location: text(form, "farm_location"),
        crops: text(form, "crops").split(/[,\n]/).map((x) => x.trim()).filter(Boolean),
        growing_seasons: text(form, "growing_seasons"),
        harvest_windows: text(form, "harvest_windows"),
        estimated_capacity: text(form, "estimated_capacity"),
        collection_arrangements: text(form, "collection_arrangements"),
        information_source: text(form, "information_source"),
      }),
    [`/suppliers/${id}`],
    "Farm profile saved.",
  );
}

export async function addDocument(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.supply.addDocument(t, id, {
        kind: text(form, "kind"),
        title: text(form, "title"),
        reference: text(form, "reference"),
        issued_by: text(form, "issued_by"),
        expires_on: text(form, "expires_on") || undefined,
      }),
    [`/suppliers/${id}`],
    "Document recorded. It counts once someone has verified it.",
  );
}

export async function verifyDocument(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run((t) => api.supply.verifyDocument(t, text(form, "document_id")), [`/suppliers/${text(form, "id")}`], "Document verified.");
}

export async function logCommunication(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) =>
      api.supply.logCommunication(t, id, {
        channel: text(form, "channel") as never,
        direction: text(form, "direction") as never,
        summary: text(form, "summary"),
      }),
    [`/suppliers/${id}`],
    "Recorded.",
  );
}

export async function saveCriterion(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id") || null;
  return run(
    (t) =>
      api.supply.saveCriterion(t, id, {
        name: opt(form, "name"),
        description: opt(form, "description"),
        supplier_types: form.has("types_present") ? all(form, "supplier_types").filter(Boolean) : undefined,
        required: form.has("required_present") ? flag(form, "required") : undefined,
        procedure_ref: opt(form, "procedure_ref"),
        active: form.has("active_present") ? flag(form, "active") : undefined,
      }),
    ["/supply/settings"],
    "Criterion saved.",
  );
}

// ---------------------------------------------------------------- products

function productInput(form: FormData) {
  const input: Record<string, unknown> = {};
  for (const k of ["kind", "ingredient_id", "sku", "name", "description", "grade", "specification", "pack_quantity", "pack_unit", "pack_description", "min_order_packs", "availability", "delivery_constraints"]) {
    const v = opt(form, k);
    if (v !== undefined && !(k === "ingredient_id" && v === "")) input[k] = v;
  }
  const lead = int(form, "lead_time_days");
  if (lead !== undefined) input.lead_time_days = lead;
  if (form.has("active_present")) input.active = flag(form, "active");
  return input;
}

export async function createProduct(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.supply.createProduct(token, text(form, "supplier_id"), productInput(form))).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath(`/suppliers/${text(form, "supplier_id")}`);
  redirect(`/supplier-products/${id}`);
}

export async function updateProduct(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run((t) => api.supply.updateProduct(t, id, productInput(form)), [`/supplier-products/${id}`], "Product saved.");
}

export async function reviewMapping(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  // The primary button confirms; the secondary one sends decision=rejected.
  const decision = (text(form, "decision") || "confirmed") as "confirmed" | "rejected";
  return run(
    (t) => api.supply.reviewMapping(t, id, { decision, note: text(form, "note") }),
    [`/supplier-products/${id}`, "/supplier-products"],
    decision === "confirmed" ? "Mapping confirmed: this product is now a purchasing option." : "Mapping rejected.",
  );
}

export async function addPrice(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const amount = cents(form, "amount");
  if (amount === undefined || Number.isNaN(amount)) return { error: "Enter the price per pack.", fields: [{ field: "amount_minor", message: "Enter dollars and cents." }] };
  return run(
    (t) =>
      api.supply.addPrice(t, id, {
        amount_minor: amount,
        currency: text(form, "currency") || "TTD",
        valid_from: text(form, "valid_from"),
        valid_until: text(form, "valid_until") || undefined,
        origin: (text(form, "origin") || "price_list") as never,
        note: text(form, "note"),
      }),
    [`/supplier-products/${id}`],
    "Price recorded. Earlier prices are kept.",
  );
}

export async function recordAvailability(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) =>
      api.supply.recordAvailability(t, {
        supplier_id: text(form, "supplier_id"),
        ingredient_id: text(form, "ingredient_id"),
        status: text(form, "status"),
        estimated_quantity: text(form, "estimated_quantity") || undefined,
        unit: text(form, "unit") || undefined,
        available_from: text(form, "available_from") || undefined,
        available_until: text(form, "available_until") || undefined,
        confidence: text(form, "confidence"),
        source: text(form, "source"),
      }),
    ["/supplier-availability", `/suppliers/${text(form, "supplier_id")}`],
    "Availability recorded. It is not stock.",
  );
}

// ---------------------------------------------------------------- procurement

export async function createRun(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.supply.createRun(token, text(form, "plan_id"))).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/procurement");
  redirect(`/procurement/${id}`);
}

export async function decide(_: CatalogState, form: FormData): Promise<CatalogState> {
  const run_ = text(form, "run_id");
  const choice = text(form, "choice"); // product id, or a decision word
  const decision = ["no_purchase", "deferred", "pending"].includes(choice) ? choice : "buy";
  return run(
    (t) =>
      api.supply.decide(t, run_, text(form, "line_id"), {
        decision: decision as never,
        product_id: decision === "buy" ? choice : undefined,
        packs: decision === "buy" ? text(form, `packs_${choice}`) || text(form, "packs") : undefined,
        note: text(form, "note"),
      }),
    [`/procurement/${run_}`],
    "Decision recorded.",
  );
}

export async function runAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action");
  if (action === "approve") return run((t) => api.supply.approveRun(t, id), [`/procurement/${id}`, "/procurement"], "Purchasing decisions approved.");
  if (action === "reopen") return run((t) => api.supply.reopenRun(t, id, text(form, "reason")), [`/procurement/${id}`], "Reopened: decisions can change.");
  if (action === "orders") return run((t) => api.supply.ordersFromRun(t, id), [`/procurement/${id}`, "/purchase-orders"], "Draft orders made from the buy decisions.");
  return { error: "Unknown action." };
}

export async function saveProcurementSettings(_: CatalogState, form: FormData): Promise<CatalogState> {
  const limit = cents(form, "approval_limit");
  if (Number.isNaN(limit)) return { error: "Enter the limit in dollars.", fields: [{ field: "approval_limit_minor", message: "A number." }] };
  const tolerance = text(form, "receiving_tolerance_percent");
  return run(
    (t) =>
      api.supply.saveProcurementSettings(t, {
        approval_limit_minor: limit,
        clear_approval_limit: limit === undefined,
        receiving_tolerance_percent: tolerance || undefined,
        clear_tolerance: tolerance === "",
        currencies: text(form, "currencies").split(/[,\s]+/).filter(Boolean),
        allow_unapproved_suppliers: flag(form, "allow_unapproved_suppliers"),
        unapproved_policy_ref: text(form, "unapproved_policy_ref"),
      }),
    ["/supply/settings"],
    "Procurement policies saved.",
  );
}

export async function createRFQ(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const ingredients = all(form, "line_ingredient");
  const quantities = all(form, "line_quantity");
  const units = all(form, "line_unit");
  const lines = ingredients
    .map((ing, i) => ({ ingredient_id: ing, quantity: quantities[i] ?? "", unit: units[i] ?? "g" }))
    .filter((l) => l.ingredient_id && l.quantity);
  let id: string;
  try {
    id = (
      await api.supply.createRFQ(token, {
        run_id: text(form, "run_id") || undefined,
        required_by: text(form, "required_by") || undefined,
        deadline: text(form, "deadline") || undefined,
        notes: text(form, "notes"),
        lines,
        supplier_ids: all(form, "supplier_ids").filter(Boolean),
      })
    ).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/quotations");
  redirect(`/quotations/${id}`);
}

export async function recordInvitation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  return run(
    (t) => api.supply.recordInvitation(t, id, { supplier_id: text(form, "supplier_id"), channel: text(form, "channel") as never, note: text(form, "note") }),
    [`/quotations/${id}`],
    "Recorded that you asked them.",
  );
}

export async function endRFQ(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "close" | "cancel";
  return run((t) => api.supply.endRFQ(t, id, action), [`/quotations/${id}`, "/quotations"], action === "close" ? "Request closed." : "Request cancelled.");
}

export async function recordQuotation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const products = all(form, "q_product");
  const packs = all(form, "q_packs");
  const prices = form.getAll("q_price").map((v) => String(v).replace(/[,\s$]/g, ""));
  const lines = products
    .map((p, i) => ({ product_id: p, packs: packs[i] ?? "", unit_price_minor: Math.round(Number(prices[i] ?? "") * 100) }))
    .filter((l) => l.product_id && l.packs && Number.isFinite(l.unit_price_minor));
  const rfq = text(form, "rfq_id");
  return run(
    (t) =>
      api.supply.recordQuotation(t, {
        rfq_id: rfq || undefined,
        supplier_id: text(form, "supplier_id"),
        reference: text(form, "reference"),
        currency: text(form, "currency") || "TTD",
        lead_time_days: int(form, "lead_time_days"),
        delivery_terms: text(form, "delivery_terms"),
        valid_until: text(form, "valid_until") || undefined,
        received_via: text(form, "received_via") as never,
        lines,
      }),
    [rfq ? `/quotations/${rfq}` : "/quotations"],
    "Quotation recorded as received.",
  );
}

// ---------------------------------------------------------------- purchase orders

function poLines(form: FormData): POLineInput[] {
  const products = all(form, "line_product");
  const packs = all(form, "line_packs");
  const prices = form.getAll("line_price").map((v) => String(v).replace(/[,\s$]/g, ""));
  const plines = all(form, "line_procurement");
  return products
    .map((p, i) => {
      const price = prices[i] ? Math.round(Number(prices[i]) * 100) : undefined;
      return { product_id: p, packs: packs[i] ?? "", unit_price_minor: price, procurement_line_id: plines[i] || undefined } as POLineInput;
    })
    .filter((l) => l.product_id && l.packs);
}

export async function createPO(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (
      await api.supply.createPurchaseOrder(token, {
        supplier_id: text(form, "supplier_id"),
        currency: text(form, "currency") || "TTD",
        expected_delivery: text(form, "expected_delivery") || undefined,
        location_id: text(form, "location_id") || undefined,
      })
    ).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/purchase-orders");
  redirect(`/purchase-orders/${id}`);
}

export async function updatePO(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const fee = cents(form, "delivery_fee");
  if (Number.isNaN(fee)) return { error: "Enter the delivery fee in dollars.", fields: [{ field: "delivery_fee_minor", message: "A number." }] };
  return run(
    (t) =>
      api.supply.updatePurchaseOrder(t, id, {
        expected_delivery: opt(form, "expected_delivery"),
        location_id: opt(form, "location_id"),
        delivery_fee_minor: fee,
        notes: opt(form, "notes"),
        supplier_exception: opt(form, "supplier_exception"),
        lines: form.has("lines_present") ? poLines(form) : undefined,
        reason: text(form, "reason"),
      }),
    [`/purchase-orders/${id}`, "/purchase-orders"],
    "Order saved.",
  );
}

export async function poAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "submit" | "approve" | "reject" | "issue" | "cancel" | "close";
  const body = action === "issue" ? { channel: text(form, "channel"), note: text(form, "note") } : { reason: text(form, "reason") };
  const msg = {
    submit: "Sent for approval.",
    approve: "Approval recorded.",
    reject: "Returned to the buyer.",
    issue: "Recorded as sent to the supplier.",
    cancel: "Order cancelled.",
    close: "Order closed.",
  }[action];
  return run((t) => api.supply.purchaseOrderAction(t, id, action, action === "submit" || action === "approve" ? undefined : body), [`/purchase-orders/${id}`, "/purchase-orders"], msg);
}

// ---------------------------------------------------------------- receiving

/** One delivered line per PO line row on the receiving form. */
export async function receive(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const items = all(form, "checklist_item");
  const lines = all(form, "po_line_id")
    .map((lid) => {
      const delivered = text(form, `delivered_${lid}`);
      if (!delivered || delivered === "0") return null;
      const inspection: InspectionResult[] = items
        .filter((it) => form.has(`check_${lid}_${it}`))
        .map((it) => ({ item_id: it, name: "", result: text(form, `check_${lid}_${it}`) as never, note: text(form, `checknote_${lid}_${it}`) }));
      return {
        po_line_id: lid,
        delivered_packs: delivered,
        accepted_packs: text(form, `accepted_${lid}`) || "0",
        quarantined_packs: text(form, `quarantined_${lid}`) || "0",
        rejected_packs: text(form, `rejected_${lid}`) || "0",
        supplier_lot_ref: text(form, `lotref_${lid}`),
        expiry_date: text(form, `expiry_${lid}`),
        location_id: text(form, `location_${lid}`),
        quarantine_location_id: text(form, `quarantine_${lid}`),
        temperature_c: text(form, `temp_${lid}`) || null,
        discrepancy: text(form, `discrepancy_${lid}`) || "none",
        discrepancy_note: text(form, `discnote_${lid}`),
        inspection,
      };
    })
    .filter((l) => l !== null);
  if (lines.length === 0) return { error: "Enter what was delivered on at least one line." };
  let id: string;
  try {
    id = (
      await api.supply.receive(token, {
        po_id: text(form, "po_id"),
        idempotency_key: text(form, "idempotency_key"),
        delivery_ref: text(form, "delivery_ref"),
        notes: text(form, "notes"),
        lines,
      })
    ).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/receiving");
  revalidatePath(`/purchase-orders/${text(form, "po_id")}`);
  redirect(`/receiving/${id}`);
}

// ---------------------------------------------------------------- inventory

export async function lotAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  // The waste form's secondary button returns the stock to the supplier instead.
  const action = (form.get("to_supplier") ? "supplier-return" : text(form, "action")) as "status" | "expiry" | "transfer" | "waste" | "supplier-return";
  const body: Record<string, unknown> = { reason: text(form, "reason") };
  if (action === "status") body.status = text(form, "status");
  if (action === "expiry") Object.assign(body, { expiry_date: text(form, "expiry_date"), none_applies: flag(form, "none_applies"), note: text(form, "note") });
  if (action === "transfer") body.location_id = text(form, "location_id");
  if (action === "waste" || action === "supplier-return") body.quantity = text(form, "quantity");
  const msg = { status: "Status changed.", expiry: "Expiry recorded.", transfer: "Lot moved.", waste: "Store waste recorded.", "supplier-return": "Return recorded." }[action];
  return run((t) => api.supply.lotAction(t, id, action, body), [`/inventory/lots/${id}`, "/inventory"], msg);
}

export async function adjust(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "lot_id");
  return run(
    (t) =>
      api.supply.adjust(t, {
        lot_id: id,
        category: text(form, "category") as never,
        new_on_hand: text(form, "new_on_hand"),
        reason: text(form, "reason"),
        evidence: text(form, "evidence"),
      }),
    [`/inventory/lots/${id}`, "/inventory"],
    "Adjustment recorded.",
  );
}

export async function reserve(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    const r = await api.supply.reserve(token, { plan_id: text(form, "plan_id"), ingredient_id: text(form, "ingredient_id"), quantity: text(form, "quantity") });
    const back = text(form, "back");
    if (back) revalidatePath(back);
    revalidatePath("/inventory");
    return {
      message:
        Number(r.short) > 0
          ? `Reserved ${r.reserved}; ${r.short} could not be covered from usable stock.`
          : `Reserved ${r.reserved} (${r.method.toUpperCase()}).`,
    };
  } catch (e) {
    return failure(e);
  }
}

export async function releaseReservation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const back = text(form, "back") || "/inventory";
  return run((t) => api.supply.releaseReservation(t, text(form, "id"), text(form, "reason")), [back], "Reservation released.");
}

export async function issueStock(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const batch = text(form, "batch_id");
  try {
    const r = await api.supply.issue(token, {
      batch_id: batch,
      ingredient_id: text(form, "ingredient_id"),
      quantity: text(form, "quantity"),
      lot_id: text(form, "lot_id") || undefined,
    });
    revalidatePath(`/production/batches/${batch}`);
    revalidatePath(`/inventory/batches/${batch}`);
    return { message: `Issued from ${r.issued.map((l) => `${l.lot_code} (${l.quantity} ${l.unit})`).join(", ")}.` };
  } catch (e) {
    return failure(e);
  }
}

export async function returnStock(_: CatalogState, form: FormData): Promise<CatalogState> {
  const batch = text(form, "batch_id");
  return run(
    (t) => api.supply.returnStock(t, { batch_id: batch, lot_id: text(form, "lot_id"), quantity: text(form, "quantity"), reason: text(form, "reason") }),
    [`/production/batches/${batch}`, `/inventory/batches/${batch}`],
    "Returned to the store.",
  );
}

export async function startCount(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  let id: string;
  try {
    id = (await api.supply.startCount(token, text(form, "location_id"), text(form, "notes"))).id;
  } catch (e) {
    return failure(e);
  }
  revalidatePath("/inventory/counts");
  redirect(`/inventory/counts/${id}`);
}

export async function recordCounts(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const lines = all(form, "lot_id")
    .map((lot) => ({ lot_id: lot, counted: text(form, `counted_${lot}`) }))
    .filter((l) => l.counted !== "");
  if (lines.length === 0) return { error: "Enter at least one counted quantity." };
  return run((t) => api.supply.recordCounts(t, id, lines), [`/inventory/counts/${id}`], "Counts recorded.");
}

export async function countAction(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id");
  const action = text(form, "action") as "submit" | "post" | "cancel";
  const msg = { submit: "Submitted for approval.", post: "Count posted: variances adjusted.", cancel: "Count cancelled." }[action];
  return run((t) => api.supply.countAction(t, id, action), [`/inventory/counts/${id}`, "/inventory/counts", "/inventory"], msg);
}

export async function saveLocation(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id") || null;
  return run(
    (t) =>
      api.supply.saveLocation(t, id, {
        code: opt(form, "code"),
        name: opt(form, "name"),
        type: opt(form, "type"),
        capacity_note: opt(form, "capacity_note"),
        temperature_note: opt(form, "temperature_note"),
        active: form.has("active_present") ? flag(form, "active") : undefined,
      }),
    ["/supply/settings"],
    "Location saved.",
  );
}

export async function savePolicy(_: CatalogState, form: FormData): Promise<CatalogState> {
  return run(
    (t) => api.supply.savePolicy(t, { category: text(form, "category"), method: text(form, "method") as never, note: text(form, "note") }),
    ["/supply/settings"],
    "Allocation policy saved.",
  );
}

export async function saveChecklistItem(_: CatalogState, form: FormData): Promise<CatalogState> {
  const id = text(form, "id") || null;
  return run(
    (t) =>
      api.supply.saveChecklistItem(t, id, {
        name: opt(form, "name"),
        criteria: opt(form, "criteria"),
        categories: form.has("categories_present") ? all(form, "categories").filter(Boolean) : undefined,
        procedure_ref: opt(form, "procedure_ref"),
        active: form.has("active_present") ? flag(form, "active") : undefined,
      }),
    ["/supply/settings"],
    "Checklist item saved.",
  );
}
