"use server";

import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "../../meals/actions";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();

function failure(e: unknown, forbidden: string): CatalogState {
  if (e instanceof ApiError) {
    if (e.code === "validation_failed") return { error: "Some fields need attention.", fields: e.fields };
    return { error: e.status === 403 ? forbidden : e.message };
  }
  return { error: "The Mise API could not be reached. Try again." };
}

/** Enables or disables a provider for checkout (test mode). The API refuses while the adapter is not ready. */
export async function setProviderEnabled(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const enabled = text(form, "enabled") === "true";
  try {
    await api.admin.updatePaymentProvider(token, text(form, "provider"), { enabled, note: text(form, "note") });
  } catch (e) {
    return failure(e, "Your role cannot change payment providers.");
  }
  revalidatePath("/payments/providers");
  return { message: enabled ? "Provider enabled for checkout." : "Provider disabled: customers no longer see it." };
}

/** Records the merchant account's status and reference with the provider. */
export async function setMerchantStatus(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  try {
    await api.admin.updatePaymentProvider(token, text(form, "provider"), {
      merchant_status: text(form, "merchant_status") as "not_started" | "applied" | "verified" | "rejected",
      merchant_reference: text(form, "merchant_reference"),
      note: text(form, "note"),
    });
  } catch (e) {
    return failure(e, "Your role cannot change payment providers.");
  }
  revalidatePath("/payments/providers");
  return { message: "Merchant status recorded." };
}

/** Switches test/live. Live is refused unless live credentials, a verified merchant and an approval reference exist. */
export async function setProviderMode(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const mode = text(form, "mode") as "test" | "live";
  try {
    await api.admin.setPaymentProviderMode(token, text(form, "provider"), { mode, reference: text(form, "reference"), note: text(form, "note") });
  } catch (e) {
    return failure(e, "Only an owner with live-payment approval can change the mode.");
  }
  revalidatePath("/payments/providers");
  return { message: mode === "live" ? "Live mode approved and recorded." : "Returned to test mode." };
}
