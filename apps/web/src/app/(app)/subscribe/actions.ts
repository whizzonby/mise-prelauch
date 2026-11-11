"use server";

import { redirect } from "next/navigation";

import { formError, type FormState } from "@/lib/messages";
import { api, requireCustomer } from "@/lib/session";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();

/**
 * Creates the subscription and sends the customer to the provider's hosted
 * checkout. Nothing here decides the payment worked: the API activates the
 * subscription when the provider confirms it.
 */
export async function startSubscription(_: FormState, form: FormData): Promise<FormState> {
  const { token } = await requireCustomer();
  let target: string;
  try {
    const res = await api.commerce.subscribe(token, {
      plan_id: text(form, "plan_id"),
      address_id: text(form, "address_id"),
      ...(text(form, "promotion_code") ? { promotion_code: text(form, "promotion_code") } : {}),
    });
    target = res.checkout?.redirect_url ?? `/subscribe/result?activated=1`;
  } catch (error) {
    return formError(error);
  }
  redirect(target);
}

/** Opens checkout for an unpaid week (a failed renewal or an unfinished first payment). */
export async function payWeek(_: FormState, form: FormData): Promise<FormState> {
  const { token } = await requireCustomer();
  let target: string;
  try {
    const c = await api.commerce.pay(token, text(form, "billing_cycle_id"));
    if (!c.redirect_url) return { error: "Checkout could not be opened. Try again in a moment." };
    target = c.redirect_url;
  } catch (error) {
    return formError(error);
  }
  redirect(target);
}

/** What the server knows about a checkout; the result page asks repeatedly. */
export async function checkoutStatus(id: string) {
  const { token } = await requireCustomer();
  try {
    const r = await api.commerce.checkout(token, id);
    return { outcome: r.outcome, status: r.subscription_status, weekLabel: r.week_label, invoiceId: r.invoice_id };
  } catch {
    return null;
  }
}
