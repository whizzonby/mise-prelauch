import type { Metadata } from "next";

import { ChoiceField, Form, FormMessage, SelectField, SubmitButton, TextField } from "@/components/forms";
import { AllergyFields } from "@/components/allergy-fields";
import { reference } from "@/lib/data";
import { cookTimes } from "@/lib/options";
import { shortDateTime } from "@/lib/greeting";
import { api, requireCustomer } from "@/lib/session";

import { saveAllergies, savePreferences } from "../actions";

export const metadata: Metadata = { title: "Food and allergies" };


/**
 * Preferences and allergies are two forms on purpose. A preference ("I
 * prefer vegetarian meals") shapes suggestions; an allergy ("I have a peanut
 * allergy") is safety information and is saved, and changed, on its own.
 */
export default async function PreferencesPage() {
  const { token } = await requireCustomer();
  const [prefs, ref] = await Promise.all([api.account.preferences(token), reference()]);
  return (
    <>
      <h1 className="type-h2">Food and allergies</h1>

      <section aria-labelledby="prefs" className="mt-8">
        <h2 id="prefs" className="type-h3">
          What you like to eat
        </h2>
        <p className="type-body-sm mt-2 text-muted">
          Used to suggest meals.
          {prefs.updated_at ? ` Last saved ${shortDateTime(prefs.updated_at)}.` : ""}
          {prefs.source === "waitlist" ? " Some answers came from your waitlist sign-up; change anything here." : ""}
        </p>
        <Form action={savePreferences} aria-label="Food preferences" className="mt-6">
          <ChoiceField
            name="dietary"
            legend="Dietary preferences"
            hint="Choose any that apply. Vegan already includes vegetarian."
            options={[...ref.dietary, { key: "no-preference", label: "No specific preference" }]}
            selected={prefs.dietary}
          />
          <ChoiceField name="meal_interests" legend="Meals that interest you" optional options={ref.meal_interests} selected={prefs.meal_interests} />
          <ChoiceField name="proteins" legend="Proteins you enjoy" optional options={ref.proteins} selected={prefs.proteins} />
          <SelectField
            name="max_cook_minutes"
            label="Longest you would like to spend cooking on a weeknight"
            optional
            placeholder="No limit"
            defaultValue={prefs.max_cook_minutes ? String(prefs.max_cook_minutes) : ""}
            options={cookTimes}
          />
          <TextField
            name="other_restrictions"
            label="Anything else we should know"
            optional
            maxLength={300}
            defaultValue={prefs.other_restrictions ?? ""}
            hint="For example: no coriander, less spice. For allergies, use the section below."
          />
          <FormMessage />
          <div>
            <SubmitButton pending="Saving…">Save preferences</SubmitButton>
          </div>
        </Form>
      </section>

      <section aria-labelledby="allergies" className="mt-14 border-t-4 border-accent pt-8">
        <h2 id="allergies" className="type-h3">
          Allergies
        </h2>
        <p className="type-body-sm mt-2 text-muted">
          {prefs.allergies_reviewed_at
            ? `Last confirmed ${shortDateTime(prefs.allergies_reviewed_at)}.`
            : "You have not told us about allergies yet."}
        </p>
        <p className="type-body-sm mt-4 border-l-2 border-warning bg-surface p-4">{ref.cross_contact_notice}</p>
        <Form action={saveAllergies} aria-label="Allergies" className="mt-6">
          <AllergyFields options={ref.allergens} allergies={prefs.allergies} />
          <FormMessage />
          <div>
            <SubmitButton pending="Saving…">Save allergies</SubmitButton>
          </div>
        </Form>
      </section>
    </>
  );
}
