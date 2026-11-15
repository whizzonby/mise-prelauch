"use server";

import { revalidatePath } from "next/cache";

import { formError, type FormState } from "@/lib/messages";
import { api, requireCustomer } from "@/lib/session";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();

/** Ask for help with this order's delivery: opens a support case linked to it. */
export async function deliverySupport(_: FormState, form: FormData): Promise<FormState> {
  const { token } = await requireCustomer();
  const id = text(form, "order_id");
  try {
    const r = await api.commerce.deliverySupport(token, id, text(form, "message"));
    revalidatePath(`/orders/${id}`);
    return { done: true, message: `Thanks. We've received your message (case ${r.case_number}) and will get back to you.` };
  } catch (error) {
    return formError(error);
  }
}

/** Ask to change the delivery date. A request, not a promise: the team confirms. */
export async function requestReschedule(_: FormState, form: FormData): Promise<FormState> {
  const { token } = await requireCustomer();
  const id = text(form, "order_id");
  try {
    await api.commerce.requestReschedule(token, id, { reason: text(form, "reason"), preferred_date: text(form, "preferred_date") || undefined });
    revalidatePath(`/orders/${id}`);
    return { done: true, message: "Request sent. Your date only changes once we confirm it." };
  } catch (error) {
    return formError(error);
  }
}
