import { NextResponse, type NextRequest } from "next/server";

/**
 * An early, cheap check only: pages behind sign-in redirect visitors without
 * a session cookie straight to sign-in, remembering where they were going.
 * It is not the security boundary. Every page still asks the API who the
 * session belongs to (requireCustomer), and the API authorises every call.
 */
const SESSION_COOKIE = "mise_session";

export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  const target = pathname + search;
  if (!request.cookies.has(SESSION_COOKIE)) {
    const url = request.nextUrl.clone();
    url.pathname = "/sign-in";
    url.search = `?next=${encodeURIComponent(target)}`;
    return NextResponse.redirect(url);
  }
  // Lets a page that finds the session expired send the person back here after sign-in.
  const headers = new Headers(request.headers);
  headers.set("x-mise-path", target);
  return NextResponse.next({ request: { headers } });
}

export const config = {
  matcher: [
    "/dashboard/:path*",
    "/meals/:path*",
    "/favorites/:path*",
    "/profile/:path*",
    "/onboarding/:path*",
    "/chefs/:path*",
    "/plans/:path*",
    "/subscribe/:path*",
    "/subscription/:path*",
    "/menu/:path*",
    "/orders/:path*",
    "/billing/:path*",
  ],
};
