"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { formError, type FormState } from "@/lib/messages";
import { safeNext } from "@/lib/safe-redirect";
import { api, clearSession, sessionToken, storeSession } from "@/lib/session";

const text = (form: FormData, key: string) => String(form.get(key) ?? "");

/**
 * Where a new customer was heading (a plan they chose) survives the trip
 * through the verification email: kept for a day in this browser, used once
 * at the first sign-in, then removed.
 */
const AFTER_SIGNUP_COOKIE = "mise_after_signup";

export async function signIn(_previous: FormState, form: FormData): Promise<FormState> {
  const email = text(form, "email").trim();
  const password = text(form, "password");
  if (!email || !password) return { error: "Enter your email and password." };
  const jar = await cookies();
  const remembered = jar.get(AFTER_SIGNUP_COOKIE)?.value ?? "";
  const requested = text(form, "next") || remembered;
  let destination = safeNext(requested);
  try {
    const result = await api.auth.login(email, password);
    await storeSession(result.token, result.expires_in);
    if (remembered) jar.delete(AFTER_SIGNUP_COOKIE);
    // Someone who has not set up their kitchen yet starts there, unless they
    // followed a link to a particular page.
    const onboarding = requested ? null : await api.account.onboarding(result.token).catch(() => null);
    if (onboarding && !onboarding.completed_at && onboarding.step === "welcome") destination = "/onboarding";
  } catch (error) {
    return formError(error);
  }
  redirect(destination);
}

export async function register(_previous: FormState, form: FormData): Promise<FormState> {
  try {
    await api.auth.register({
      email: text(form, "email").trim(),
      password: text(form, "password"),
      first_name: text(form, "first_name"),
      last_name: text(form, "last_name"),
      accept_terms: form.get("accept_terms") === "on",
    });
  } catch (error) {
    return formError(error);
  }
  const next = text(form, "next");
  if (next && safeNext(next, "") !== "") {
    (await cookies()).set(AFTER_SIGNUP_COOKIE, safeNext(next), {
      httpOnly: true,
      secure: process.env.WEB_COOKIE_SECURE !== "false",
      sameSite: "lax",
      path: "/",
      maxAge: 24 * 60 * 60,
    });
  }
  redirect("/check-email");
}

export async function verifyEmail(_previous: FormState, form: FormData): Promise<FormState> {
  try {
    await api.auth.verifyEmail(text(form, "token"));
  } catch (error) {
    return formError(error);
  }
  redirect("/sign-in?verified=1");
}

export async function forgotPassword(_previous: FormState, form: FormData): Promise<FormState> {
  try {
    await api.auth.forgotPassword(text(form, "email").trim());
  } catch (error) {
    return formError(error);
  }
  return { done: true };
}

export async function resetPassword(_previous: FormState, form: FormData): Promise<FormState> {
  try {
    await api.auth.resetPassword(text(form, "token"), text(form, "password"));
  } catch (error) {
    return formError(error);
  }
  await clearSession();
  redirect("/sign-in?reset=1");
}

export async function signOut(): Promise<void> {
  const token = await sessionToken();
  if (token) await api.auth.logout(token).catch(() => undefined);
  await clearSession();
  redirect("/sign-in");
}

export async function confirmEmailChange(_previous: FormState, form: FormData): Promise<FormState> {
  try {
    await api.account.confirmEmailChange(text(form, "token"));
  } catch (error) {
    return formError(error);
  }
  return { done: true };
}
