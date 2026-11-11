"use server";

import { redirect } from "next/navigation";

import { api } from "@/lib/session";

/** A button on the test provider's page. The simulator then sends its signed webhook. */
export async function completeTestPayment(form: FormData): Promise<void> {
  const ref = String(form.get("ref") ?? "");
  const outcome = String(form.get("outcome") ?? "") as "approve" | "decline" | "cancel";
  const card = (String(form.get("card") ?? "approve") === "decline" ? "decline" : "approve") as "approve" | "decline";
  const session = await api.testpay.complete(ref, outcome, card);
  redirect(session.return_url);
}
