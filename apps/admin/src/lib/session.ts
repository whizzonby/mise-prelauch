import "server-only";

import { ApiError, createClient, mfaComplete, type AdminPrincipal, type Permission } from "@mise/api-client";
import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";
import { cache } from "react";

export const SESSION_COOKIE = "mise_admin";

/**
 * The end user's address and browser, passed to the API with the shared BFF
 * key so per-address rate limits apply to people, not to this server.
 */
async function forwardedHeaders(): Promise<Record<string, string>> {
  const key = process.env.BFF_SHARED_KEY;
  if (!key) return {};
  try {
    const h = await headers();
    // Behind exactly one proxy (the load balancer or Caddy), the client is the
    // last address it appended.
    const ip = h.get("x-forwarded-for")?.split(",").at(-1)?.trim() ?? "";
    return { "X-Mise-Bff-Key": key, "X-Mise-Client-Ip": ip, "X-Mise-Client-Ua": h.get("user-agent") ?? "" };
  } catch {
    // Outside a request (build time): nothing to forward.
    return {};
  }
}

/** The API client. It runs only on this app's server; the browser never calls the API. */
export const api = createClient({
  baseUrl: process.env.API_INTERNAL_URL ?? "http://localhost:8090",
  headers: forwardedHeaders,
});

export async function sessionToken(): Promise<string | undefined> {
  return (await cookies()).get(SESSION_COOKIE)?.value;
}

/** Stores a session token in an httpOnly cookie, for as long as the API says it lasts. */
export async function storeSession(token: string, expiresIn: number): Promise<void> {
  (await cookies()).set(SESSION_COOKIE, token, {
    httpOnly: true,
    // Only ever disabled for local development over http.
    secure: process.env.ADMIN_COOKIE_SECURE !== "false",
    sameSite: "strict",
    path: "/",
    maxAge: expiresIn,
  });
}

export interface Session {
  token: string;
  admin: AdminPrincipal;
}

/** The current session whatever its MFA state, or a redirect to sign in. */
export const requireSession = cache(async (): Promise<Session> => {
  const token = await sessionToken();
  if (!token) redirect("/login");
  try {
    return { token, admin: await api.admin.me(token) };
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) redirect("/login?expired=1");
    throw error;
  }
});

/**
 * Returns the signed-in admin who has finished MFA, or redirects. Every page,
 * action and route handler calls this itself: a layout check alone does not
 * protect the routes beneath it. The API re-checks the session, MFA and the
 * permission on every call, so this is the first gate, not the only one.
 */
export const requireAdmin = cache(async (): Promise<Session> => {
  const session = await requireSession();
  if (!mfaComplete(session.admin.mfa)) redirect("/login/mfa");
  return session;
});

export function can(admin: AdminPrincipal, permission: Permission): boolean {
  return admin.permissions.includes(permission);
}
