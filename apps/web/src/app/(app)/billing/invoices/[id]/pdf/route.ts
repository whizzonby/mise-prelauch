import { ApiError } from "@mise/api-client";

import { api, sessionToken } from "@/lib/session";

/**
 * Streams an invoice PDF from the API. The session token stays on this
 * server; the API decides whether the invoice belongs to the customer.
 */
export async function GET(_: Request, { params }: { params: Promise<{ id: string }> }) {
  const token = await sessionToken();
  if (!token) return new Response("Sign in to download invoices.", { status: 401 });
  const { id } = await params;
  try {
    const res = await api.commerce.invoicePdf(token, id);
    return new Response(res.body, {
      headers: {
        "Content-Type": "application/pdf",
        "Content-Disposition": res.headers.get("Content-Disposition") ?? `attachment; filename="invoice.pdf"`,
        "Cache-Control": "private, no-store",
      },
    });
  } catch (e) {
    const status = e instanceof ApiError ? e.status : 502;
    return new Response(status === 404 ? "Not found." : "The invoice could not be downloaded.", { status });
  }
}
