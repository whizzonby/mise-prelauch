"use server";

import { ApiError } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { api, requireAdmin } from "@/lib/session";

import type { CatalogState } from "../meals/actions";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();

/** Records a sign-off. The API checks the permission and refuses approval while the gate's evidence does not hold. */
export async function signOffGate(_: CatalogState, form: FormData): Promise<CatalogState> {
  const { token } = await requireAdmin();
  const decision = text(form, "decision") as "approved" | "not_applicable" | "revoked";
  try {
    await api.launch.signoff(token, text(form, "gate"), { decision, reference: text(form, "reference"), note: text(form, "note") });
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === "validation_failed") return { error: "Some fields need attention.", fields: e.fields };
      return { error: e.status === 403 ? "Your role cannot sign off launch gates." : e.message };
    }
    return { error: "The Mise API could not be reached. Try again." };
  }
  revalidatePath("/launch");
  return { message: decision === "revoked" ? "Sign-off revoked." : "Sign-off recorded." };
}
