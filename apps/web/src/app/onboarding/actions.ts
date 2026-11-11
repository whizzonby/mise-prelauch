"use server";

import type { OnboardingStep } from "@mise/api-client";
import { revalidatePath } from "next/cache";

import { formError, type FormState } from "@/lib/messages";
import { householdOptions } from "@/lib/options";
import { api, requireCustomer } from "@/lib/session";

const ORDER: OnboardingStep[] = ["welcome", "household", "interests", "dietary", "allergies", "location", "complete"];
const text = (form: FormData, key: string) => String(form.get(key) ?? "").trim();
const all = (form: FormData, key: string) => form.getAll(key).map(String).filter(Boolean);

export interface StepResult extends FormState {
  step?: OnboardingStep;
  skipped?: string[];
}

function nextOf(step: OnboardingStep): OnboardingStep {
  return ORDER[Math.min(ORDER.indexOf(step) + 1, ORDER.length - 1)]!;
}

/** Saves one step's answers into the customer's records, then moves on. */
export async function submitStep(step: OnboardingStep, skipped: string[], form: FormData): Promise<StepResult> {
  const { token, customer } = await requireCustomer();
  const stillSkipped = skipped.filter((s) => s !== step);
  try {
    switch (step) {
      case "household": {
        const choice = householdOptions.find((o) => o.key === text(form, "household"));
        const size = Number(text(form, "size")) || choice?.size || 0;
        // Keep adults, children and the name the customer may have set before,
        // as long as they still fit the new size.
        const { household } = await api.account.household(token);
        const fits = household && (household.adults ?? 0) + (household.children ?? 0) <= size;
        await api.account.putHousehold(token, {
          size,
          name: household?.name ?? null,
          adults: fits ? household.adults : null,
          children: fits ? household.children : null,
        });
        break;
      }
      case "interests":
        await api.account.putPreferences(token, { meal_interests: all(form, "meal_interests") });
        break;
      case "dietary":
        await api.account.putPreferences(token, { dietary: all(form, "dietary") });
        break;
      case "allergies": {
        const allergies = all(form, "allergen").map((allergen) =>
          allergen === "other" ? { allergen, detail: text(form, "other_detail") } : { allergen },
        );
        await api.account.putPreferences(token, { allergies });
        break;
      }
      case "location": {
        const region = text(form, "delivery_region");
        await api.account.updateProfile(token, { delivery_country: region ? "TT" : "", delivery_region: region });
        const line1 = text(form, "line1");
        if (line1) {
          // Going back and forward must not save the same address twice.
          const { items } = await api.account.addresses(token);
          if (!items.some((a) => a.line1 === line1)) {
            await api.account.createAddress(token, {
              recipient_name: `${customer.first_name} ${customer.last_name}`.trim(),
              line1,
              area: text(form, "area"),
              city: text(form, "city"),
              region,
              country: "TT",
            });
          }
        }
        break;
      }
    }
    const next = nextOf(step);
    const saved = await api.account.putOnboarding(token, next, stillSkipped);
    revalidatePath("/onboarding");
    revalidatePath("/dashboard");
    return { step: saved.step, skipped: saved.skipped };
  } catch (error) {
    const state = formError(error);
    if (state.fields?.["allergies.other"]) state.fields.other_detail = state.fields["allergies.other"];
    if (state.fields?.delivery_region) state.fields.region = state.fields.delivery_region;
    return state;
  }
}

/** Skips an optional question. Nothing is saved for it, and it stays open. */
export async function skipStep(step: OnboardingStep, skipped: string[]): Promise<StepResult> {
  const { token } = await requireCustomer();
  try {
    const saved = await api.account.putOnboarding(token, nextOf(step), [...new Set([...skipped, step])]);
    revalidatePath("/dashboard");
    return { step: saved.step, skipped: saved.skipped };
  } catch (error) {
    return formError(error);
  }
}

/** Goes back a step. Answers already saved stay saved. */
export async function goBack(step: OnboardingStep, skipped: string[]): Promise<StepResult> {
  const { token } = await requireCustomer();
  const prev = ORDER[Math.max(ORDER.indexOf(step) - 1, 0)]!;
  try {
    const saved = await api.account.putOnboarding(token, prev, skipped);
    return { step: saved.step, skipped: saved.skipped };
  } catch (error) {
    return formError(error);
  }
}
