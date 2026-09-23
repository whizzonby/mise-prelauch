"use client";

import { ApiError, type PreferencesInput } from "@mise/api-client";
import { Choice, ChoiceGroup, Heading, MiseButton, Text } from "@mise/ui";
import { preferencesDefaults, type PreferencesValues } from "@mise/validation";
import { useMutation } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";

import { profiling, waitlistForm } from "@/content/waitlist";
import { api } from "@/lib/api";

import { useTrack } from "./providers";

/** Optional profiling after signup. Every question can be left blank, and the whole form can be skipped. */
export function PreferencesForm({ token }: { token: string }) {
  const track = useTrack();
  const started = useRef(false);
  const [dismissed, setDismissed] = useState(false);
  const [error, setError] = useState<string>();
  const { register, handleSubmit } = useForm<PreferencesValues>({ defaultValues: preferencesDefaults });
  const mutation = useMutation({ mutationFn: (input: PreferencesInput) => api.updatePreferences(token, input) });
  const q = profiling.questions;

  if (dismissed) return null;

  if (mutation.isSuccess) {
    return (
      <p role="status" className="type-body border-l-4 border-success pl-5">
        {profiling.saved}
      </p>
    );
  }

  const onSubmit = handleSubmit(async (values) => {
    setError(undefined);
    const input: PreferencesInput = {
      household_type: values.householdType || undefined,
      meals_per_week: values.mealsPerWeek ? Number(values.mealsPerWeek) : undefined,
      cooking_frequency: values.cookingFrequency || undefined,
      meal_interests: values.mealInterests.length > 0 ? values.mealInterests : undefined,
      usage: values.usage || undefined,
    };
    const answered = Object.values(input).filter((value) => value !== undefined).length;
    if (answered === 0) {
      setDismissed(true);
      return;
    }
    try {
      await mutation.mutateAsync(input);
      track("preferences_completed", { answered });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : waitlistForm.errors.network);
    }
  });

  return (
    <section aria-labelledby="profiling-title">
      <Heading as="h2" id="profiling-title" size="h3">
        {profiling.title}
      </Heading>
      <Text className="mt-3 max-w-[52ch]" tone="muted">
        {profiling.body}
      </Text>

      <form
        onSubmit={onSubmit}
        noValidate
        className="mt-8 grid gap-8"
        onFocus={() => {
          if (started.current) return;
          started.current = true;
          track("preferences_started", {});
        }}
      >
        <ChoiceGroup id="pref-household" legend={q.householdType.label}>
          {q.householdType.options.map((o) => (
            <Choice key={o.value} type="radio" value={o.value} label={o.label} {...register("householdType")} />
          ))}
        </ChoiceGroup>

        <ChoiceGroup id="pref-meals" legend={q.mealsPerWeek.label}>
          {q.mealsPerWeek.options.map((o) => (
            <Choice key={o.value} type="radio" value={o.value} label={o.label} {...register("mealsPerWeek")} />
          ))}
        </ChoiceGroup>

        <ChoiceGroup id="pref-cooking" legend={q.cookingFrequency.label}>
          {q.cookingFrequency.options.map((o) => (
            <Choice key={o.value} type="radio" value={o.value} label={o.label} {...register("cookingFrequency")} />
          ))}
        </ChoiceGroup>

        <ChoiceGroup id="pref-interests" legend={q.mealInterests.label} hint="Choose any that apply.">
          {q.mealInterests.options.map((o) => (
            <Choice key={o.value} value={o.value} label={o.label} {...register("mealInterests")} />
          ))}
        </ChoiceGroup>

        <ChoiceGroup id="pref-usage" legend={q.usage.label}>
          {q.usage.options.map((o) => (
            <Choice key={o.value} type="radio" value={o.value} label={o.label} {...register("usage")} />
          ))}
        </ChoiceGroup>

        <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
          <MiseButton type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Saving…" : profiling.submit}
          </MiseButton>
          <MiseButton variant="quiet" onClick={() => setDismissed(true)}>
            {profiling.skip}
          </MiseButton>
        </div>
        <p role="alert" className={error ? "type-body-sm font-semibold text-error" : "sr-only"}>
          {error}
        </p>
      </form>
    </section>
  );
}
