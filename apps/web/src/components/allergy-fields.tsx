"use client";

import type { Term } from "@mise/api-client";
import { Choice, ChoiceGroup } from "@mise/ui";
import { useState } from "react";

import { TextField, useFormState } from "./forms";

/**
 * The allergy question. Leaving every box clear and saving records "no
 * allergies" as an answer, which is different from never having answered.
 */
export function AllergyFields({
  options,
  allergies,
  legend = "Do you have any food allergies?",
}: {
  options: Term[];
  allergies: { allergen: string; detail: string | null }[];
  legend?: string;
}) {
  const chosen = allergies.map((a) => a.allergen);
  const [other, setOther] = useState(chosen.includes("other"));
  const error = useFormState().fields?.allergies;
  return (
    <>
      <ChoiceGroup
        id="allergen"
        legend={legend}
        hint="Tick every allergy in your household. If there are none, leave them all clear and save."
        error={error}
      >
        {options.map((o) => (
          <Choice
            key={o.key}
            name="allergen"
            value={o.key}
            label={o.label}
            defaultChecked={chosen.includes(o.key)}
            onChange={o.key === "other" ? (e) => setOther(e.currentTarget.checked) : undefined}
          />
        ))}
      </ChoiceGroup>
      <div hidden={!other}>
        <TextField
          name="other_detail"
          label="What is the other allergy?"
          maxLength={200}
          defaultValue={allergies.find((a) => a.allergen === "other")?.detail ?? ""}
        />
      </div>
    </>
  );
}
