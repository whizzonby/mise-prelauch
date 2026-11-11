import { ApiError, NetworkError } from "@mise/api-client";

export interface FormState {
  error?: string;
  fields?: Record<string, string>;
  done?: boolean;
  /** Shown after a successful save, e.g. "Saved at 4:12 pm." */
  message?: string;
}

/** Turns an API failure into what the person filling the form should read. */
export function formError(error: unknown): FormState {
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") {
      return {
        error: "Some details need attention.",
        fields: Object.fromEntries(error.fields.map((f) => [f.field, f.message])),
      };
    }
    switch (error.code) {
      case "invalid_credentials":
        return { error: "Email or password is incorrect." };
      case "email_not_verified":
        return { error: "Confirm your email address first. We have sent you a new link." };
      case "registration_closed":
        return { error: "Mise accounts are not open yet. Join the waitlist on mise.tt and we will let you know." };
      case "token_invalid":
        return { error: "This link is not valid or has expired. Ask for a new one." };
      case "rate_limited":
        return { error: "Too many attempts. Wait a few minutes and try again." };
      case "email_taken":
        return { error: "That address now belongs to another Mise account, so your email was not changed." };
      case "too_many_addresses":
        return { error: "You can save up to 10 addresses. Remove one to add another." };
    }
    return { error: error.message };
  }
  if (error instanceof NetworkError) return { error: "Mise could not be reached. Try again in a moment." };
  return { error: "Something went wrong. Try again in a moment." };
}
