"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { formError, type FormState } from "@/lib/messages";
import { api, requireCustomer } from "@/lib/session";

const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();

async function act(fn: (token: string) => Promise<unknown>): Promise<FormState> {
  const { token } = await requireCustomer();
  try {
    await fn(token);
  } catch (error) {
    return formError(error);
  }
  revalidatePath("/subscription");
  revalidatePath("/dashboard");
  revalidatePath("/menu");
  redirect("/subscription?updated=1");
}

export async function changePlan(_: FormState, form: FormData): Promise<FormState> {
  return act((token) => api.commerce.changePlan(token, text(form, "subscription_id"), text(form, "plan_id")));
}

export async function pause(_: FormState, form: FormData): Promise<FormState> {
  return act((token) => api.commerce.pause(token, text(form, "subscription_id"), Number(text(form, "weeks"))));
}

export async function resume(_: FormState, form: FormData): Promise<FormState> {
  return act((token) => api.commerce.resume(token, text(form, "subscription_id")));
}

export async function cancel(_: FormState, form: FormData): Promise<FormState> {
  return act((token) => api.commerce.cancel(token, text(form, "subscription_id"), text(form, "reason") || undefined));
}

export async function setAddress(_: FormState, form: FormData): Promise<FormState> {
  return act((token) => api.commerce.setAddress(token, text(form, "subscription_id"), text(form, "address_id")));
}
