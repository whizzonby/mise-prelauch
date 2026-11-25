import { faq, launchFaq } from "@/content/home";

import { env } from "./env";
import { subscriptionInfo } from "./storefront";

/** The questions for the current site mode, with live rules in launch mode. */
export async function faqItems() {
  if (env.siteMode !== "launch") return [...faq.items];
  const info = await subscriptionInfo();
  return launchFaq({ cutoffRule: info?.cutoff_rule ?? null, maxPauseWeeks: info?.max_pause_weeks ?? null });
}
