import "server-only";

import { ApiError, createClient, type AdminPrincipal } from "@mise/api-client";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { cache } from "react";

export const SESSION_COOKIE = "mise_admin";

/** The API client. It runs only on this app's server; the browser never calls the API. */
export const api = createClient({ baseUrl: process.env.API_INTERNAL_URL ?? "http://localhost:8090" });

export async function sessionToken(): Promise<string | undefined> {
  return (await cookies()).get(SESSION_COOKIE)?.value;
}

export interface Session {
  token: string;
  admin: AdminPrincipal;
}

/**
 * Returns the signed-in admin or sends the browser to the login page. Every
 * page, action and route handler calls this itself: a layout check alone does
 * not protect the routes beneath it. The API re-checks the session and the
 * permission on every call, so this is the first gate, not the only one.
 */
export const requireAdmin = cache(async (): Promise<Session> => {
  const token = await sessionToken();
  if (!token) redirect("/login");
  try {
    return { token, admin: await api.admin.me(token) };
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) redirect("/login?expired=1");
    throw error;
  }
});

export function can(admin: AdminPrincipal, permission: string): boolean {
  return admin.permissions.includes(permission);
}
