import { Wordmark } from "@mise/ui";
import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";

import { OnboardingFlow } from "@/components/onboarding-flow";
import { reference } from "@/lib/data";
import { api, requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Set up your kitchen" };

/**
 * Onboarding: one question at a time, mobile first. It resumes where the
 * customer left off, on any device, and every answer is saved as it is given.
 */
export default async function OnboardingPage() {
  const { token, customer } = await requireCustomer();
  const [progress, prefs, { household }, profile, { items: addresses }, ref] = await Promise.all([
    api.account.onboarding(token),
    api.account.preferences(token),
    api.account.household(token),
    api.account.profile(token),
    api.account.addresses(token),
    reference(),
  ]);
  return (
    <div className="grid min-h-svh lg:grid-cols-[1fr_minmax(0,34rem)]">
      <div className="flex flex-col px-5 py-6 sm:px-10 lg:py-10">
        <header className="flex items-center justify-between">
          <span className="text-[1.875rem] text-primary">
            <Wordmark />
          </span>
          <Link href="/dashboard" className="type-body-sm font-semibold text-muted underline underline-offset-4 hover:text-foreground">
            Finish later
          </Link>
        </header>
        <main id="main" className="flex flex-1 items-center py-10">
          <OnboardingFlow
            initialStep={progress.step}
            initialSkipped={progress.skipped}
            reference={ref}
            data={{
              firstName: customer.first_name,
              householdSize: household?.size ?? null,
              interests: prefs.meal_interests,
              dietary: prefs.dietary,
              allergies: prefs.allergies,
              region: profile.delivery_region,
              availability: profile.delivery_availability,
              hasAddress: addresses.length > 0,
            }}
          />
        </main>
      </div>
      <div className="relative hidden lg:block">
        <Image src="/brand/spices.jpg" alt="" fill priority sizes="34rem" className="object-cover" />
      </div>
    </div>
  );
}
