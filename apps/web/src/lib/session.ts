import "server-only";

import { ApiError, createClient, type Customer } from "@mise/api-client";
import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";
import { cache } from "react";

export const SESSION_COOKIE = "mise_session";

/**
 * The end user's address and browser, passed to the API with the shared BFF
 * key so per-address limits apply to people rather than to this server.
 */
async function forwardedHeaders(): Promise<Record<string, string>> {
  const key = process.env.BFF_SHARED_KEY;
  if (!key) return {};
  try {
    const h = await headers();
    const ip = h.get("x-forwarded-for")?.split(",").at(-1)?.trim() ?? "";
    return { "X-Mise-Bff-Key": key, "X-Mise-Client-Ip": ip, "X-Mise-Client-Ua": h.get("user-agent") ?? "" };
  } catch {
    return {};
  }
}

/** Runs on this app's server only. The browser never sees an API token. */
export const api = createClient({
  baseUrl: process.env.API_INTERNAL_URL ?? "http://localhost:8090",
  headers: forwardedHeaders,
});

export async function sessionToken(): Promise<string | undefined> {
  return (await cookies()).get(SESSION_COOKIE)?.value;
}

export async function storeSession(token: string, expiresIn: number): Promise<void> {
  (await cookies()).set(SESSION_COOKIE, token, {
    httpOnly: true,
    secure: process.env.WEB_COOKIE_SECURE !== "false",
    // Lax, not Strict: someone arriving from an email link should arrive
    // signed in. State-changing requests are server actions, which Next.js
    // accepts only from this origin (docs/security/AUTHENTICATION.md, CSRF).
    sameSite: "lax",
    path: "/",
    maxAge: expiresIn,
  });
}

export async function clearSession(): Promise<void> {
  (await cookies()).delete(SESSION_COOKIE);
}

export interface Session {
  token: string;
  customer: Customer;
}

/** Where the current request was going, set by src/proxy.ts on protected pages. */
async function requestedPath(): Promise<string> {
  try {
    return (await headers()).get("x-mise-path") ?? "";
  } catch {
    return "";
  }
}

/**
 * The signed-in customer, or a redirect to sign in. Every protected page and
 * action calls it. A session that has ended (expired, revoked, signed out
 * elsewhere) is cleared and the person is sent back here after signing in.
 */
export const requireCustomer = cache(async (): Promise<Session> => {
  const token = await sessionToken();
  const next = await requestedPath();
  const back = next ? `&next=${encodeURIComponent(next)}` : "";
  if (!token) redirect(`/sign-in?${back.slice(1)}`);
  try {
    return { token, customer: await api.auth.me(token) };
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) redirect(`/sign-in?expired=1${back}`);
    throw error;
  }
});

/** The signed-in customer, or null. For pages that change when someone is signed in. */
export async function currentCustomer(): Promise<Customer | null> {
  const token = await sessionToken();
  if (!token) return null;
  return api.auth.me(token).catch(() => null);
}
