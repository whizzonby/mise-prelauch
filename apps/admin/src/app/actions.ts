"use server";

import { ApiError, type LeadStatus } from "@mise/api-client";
import { revalidatePath } from "next/cache";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { api, requireAdmin, SESSION_COOKIE, sessionToken } from "@/lib/session";

export interface FormState {
  error?: string;
}

export async function login(_previous: FormState, form: FormData): Promise<FormState> {
  const email = String(form.get("email") ?? "").trim();
  const password = String(form.get("password") ?? "");
  if (!email || !password) return { error: "Enter your email and password." };

  let result;
  try {
    result = await api.admin.login(email, password);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return { error: "Email or password is incorrect." };
    if (error instanceof ApiError && error.status === 429) return { error: "Too many attempts. Wait a few minutes and try again." };
    return { error: "The Mise API could not be reached. Try again in a moment." };
  }

  (await cookies()).set(SESSION_COOKIE, result.token, {
    httpOnly: true,
    // Only ever disabled for local development over http.
    secure: process.env.ADMIN_COOKIE_SECURE !== "false",
    sameSite: "strict",
    path: "/",
    maxAge: result.expires_in,
  });
  redirect("/");
}

export async function logout(): Promise<void> {
  const token = await sessionToken();
  if (token) {
    try {
      await api.admin.logout(token);
    } catch {
      // The session may already be gone; the cookie is cleared either way.
    }
  }
  (await cookies()).delete(SESSION_COOKIE);
  redirect("/login");
}

const SETTABLE: LeadStatus[] = ["VERIFIED", "QUALIFIED", "UNSUBSCRIBED", "BLOCKED"];

export async function setLeadStatus(_previous: FormState, form: FormData): Promise<FormState> {
  const { token } = await requireAdmin();
  const id = String(form.get("id") ?? "");
  const status = String(form.get("status") ?? "") as LeadStatus;
  if (!SETTABLE.includes(status)) return { error: "Choose a status." };
  try {
    await api.admin.setLeadStatus(token, id, status);
  } catch (error) {
    return { error: error instanceof ApiError ? error.message : "The status could not be changed. Try again." };
  }
  revalidatePath(`/leads/${id}`);
  return {};
}

export async function eraseLead(_previous: FormState, form: FormData): Promise<FormState> {
  const { token } = await requireAdmin();
  const id = String(form.get("id") ?? "");
  if (form.get("confirm") !== "on") return { error: "Tick the box to confirm the deletion." };
  try {
    await api.admin.eraseLead(token, id);
  } catch (error) {
    return { error: error instanceof ApiError ? error.message : "The lead could not be deleted. Try again." };
  }
  redirect("/leads?erased=1");
}
