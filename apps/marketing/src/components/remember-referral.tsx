"use client";

import { rememberReferralCode } from "@mise/analytics";
import { useEffect } from "react";

/** Keeps the invitation code, so it still counts if the visitor looks around and signs up from the home page. */
export function RememberReferral({ code }: { code: string }) {
  useEffect(() => {
    rememberReferralCode(code);
  }, [code]);
  return null;
}
