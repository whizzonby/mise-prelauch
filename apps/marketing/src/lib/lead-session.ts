/**
 * The profile token lets the browser that signed up see its own referral page
 * and answer the profiling questions. It is scoped to one lead, expires after
 * 30 days and unlocks nothing sensitive, so localStorage is an acceptable home.
 */
const KEY = "mise.profile_token";

export function saveProfileToken(token: string): void {
  try {
    window.localStorage.setItem(KEY, token);
  } catch {
    // Storage is unavailable; the email link still opens the page.
  }
}

export function readProfileToken(): string | undefined {
  try {
    return window.localStorage.getItem(KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

export function clearProfileToken(): void {
  try {
    window.localStorage.removeItem(KEY);
  } catch {
    // Nothing to clear.
  }
}
