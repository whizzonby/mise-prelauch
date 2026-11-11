/**
 * Where to send someone after signing in. Only a path on this site is
 * accepted: "//evil.example", "/\evil.example", absolute URLs and anything
 * with a scheme fall back to the dashboard, so a crafted sign-in link cannot
 * bounce a customer to another site.
 */
export function safeNext(raw: string | null | undefined, fallback = "/dashboard"): string {
  if (!raw || typeof raw !== "string") return fallback;
  const value = raw.trim();
  if (!value.startsWith("/") || value.startsWith("//") || value.startsWith("/\\")) return fallback;
  if (/[\u0000-\u001f\u007f]/.test(value) || value.includes("\\")) return fallback;
  try {
    const url = new URL(value, "https://mise.invalid");
    if (url.origin !== "https://mise.invalid") return fallback;
    // Signing in should never lead straight back to a signed-out page.
    if (["/sign-in", "/login", "/register"].includes(url.pathname)) return fallback;
    return url.pathname + url.search + url.hash;
  } catch {
    return fallback;
  }
}
