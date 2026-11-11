import type { Metadata } from "next";

import { Form, FormMessage, SubmitButton, TextField } from "@/components/forms";
import { api, requireCustomer } from "@/lib/session";

import { saveHousehold } from "../actions";

export const metadata: Metadata = { title: "Household" };

/**
 * Who the customer cooks for, as counts. Nothing is asked about the other
 * people (no names, no ages, no accounts), and nothing assumes they all eat
 * the same.
 */
export default async function HouseholdPage() {
  const { token } = await requireCustomer();
  const { household } = await api.account.household(token);
  return (
    <>
      <h1 className="type-h2">Household</h1>
      <p className="type-body mt-3 text-muted">
        How many people you usually cook for. It helps us suggest the right number of servings.
      </p>
      <Form action={saveHousehold} aria-label="Household" className="mt-8">
        <TextField
          name="name"
          label="Household name"
          optional
          maxLength={60}
          defaultValue={household?.name ?? ""}
          hint="Only for you, for example “The Ramdass table”."
        />
        <TextField
          name="size"
          label="Number of people"
          type="number"
          inputMode="numeric"
          min={1}
          max={20}
          required
          defaultValue={household?.size ?? ""}
          className="max-w-[12rem]"
        />
        <div className="grid gap-5 sm:grid-cols-2">
          <TextField name="adults" label="Adults" type="number" inputMode="numeric" min={1} max={20} optional defaultValue={household?.adults ?? ""} />
          <TextField
            name="children"
            label="Children"
            type="number"
            inputMode="numeric"
            min={0}
            max={19}
            optional
            defaultValue={household?.children ?? ""}
            hint="Just the number. We do not ask anything else about children."
          />
        </div>
        <p className="type-caption text-muted">
          Not everyone at your table has to eat the same. Allergies for anyone in the household belong under Food and allergies.
        </p>
        <FormMessage />
        <div>
          <SubmitButton pending="Saving…">Save household</SubmitButton>
        </div>
      </Form>
    </>
  );
}
