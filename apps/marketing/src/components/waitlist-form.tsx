"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { readAttribution, readReferralCode } from "@mise/analytics";
import { ApiError, type CreateLeadInput } from "@mise/api-client";
import { Checkbox, Choice, ChoiceGroup, describedBy, Field, Input, MiseButton, Select, Text } from "@mise/ui";
import { waitlistDefaults, waitlistSchema, type WaitlistValues } from "@mise/validation";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useRef, useState } from "react";
import { useForm } from "react-hook-form";

import { dietaryInterests, householdSizes, locations, waitlistForm } from "@/content/waitlist";
import { api } from "@/lib/api";
import { saveProfileToken } from "@/lib/lead-session";

import { useAnonymousId, useTrack } from "./providers";

/** API field names (snake_case) to form field names. */
const apiFields: Record<string, keyof WaitlistValues> = {
  first_name: "firstName",
  email: "email",
  phone: "phone",
  location: "location",
  dietary_interests: "dietaryInterests",
  household_size: "householdSize",
  consent: "consent",
};

type WaitlistFormProps = {
  /** Where on the site this form sits, for analytics. */
  placement: string;
  /** Set on an invitation page. Otherwise a code remembered from an earlier visit is used. */
  referralCode?: string;
};

export function WaitlistForm({ placement, referralCode }: WaitlistFormProps) {
  const id = useId();
  const router = useRouter();
  const track = useTrack();
  const anonymousId = useAnonymousId();
  const mountedAt = useRef(0);
  const started = useRef(false);
  const honeypot = useRef<HTMLInputElement>(null);
  const [formError, setFormError] = useState<string>();
  const [alreadyListed, setAlreadyListed] = useState(false);

  useEffect(() => {
    mountedAt.current = Date.now();
  }, []);

  const {
    register,
    handleSubmit,
    setError,
    setFocus,
    formState: { errors },
  } = useForm<WaitlistValues>({
    resolver: zodResolver(waitlistSchema),
    defaultValues: waitlistDefaults,
    // Validate when a field is left, then live once it has an error.
    mode: "onTouched",
  });

  const mutation = useMutation({
    mutationFn: (input: CreateLeadInput) => api.createLead(input),
  });

  const onStart = () => {
    if (started.current) return;
    started.current = true;
    track("waitlist_started", { placement });
  };

  const submit = async (values: WaitlistValues) => {
    setFormError(undefined);
    const code = referralCode ?? readReferralCode();
    const attribution = readAttribution();
    const input: CreateLeadInput = {
      first_name: values.firstName,
      email: values.email,
      phone: values.phone || undefined,
      location: values.location,
      dietary_interests: values.dietaryInterests,
      household_size: values.householdSize ? Number(values.householdSize) : undefined,
      consent: values.consent,
      referral_code: code,
      attribution: attribution.first || attribution.latest ? attribution : undefined,
      anonymous_id: anonymousId(),
      website: honeypot.current?.value ?? "",
      elapsed_ms: Date.now() - mountedAt.current,
    };

    try {
      const result = await mutation.mutateAsync(input);
      track("waitlist_completed", { placement, outcome: result.outcome, referred: Boolean(code) });
      if (result.outcome === "created") {
        saveProfileToken(result.profile_token);
        router.push("/welcome");
      } else {
        setAlreadyListed(true);
      }
    } catch (error) {
      if (error instanceof ApiError && error.code === "validation_failed" && error.fields.length > 0) {
        let first: keyof WaitlistValues | undefined;
        for (const field of error.fields) {
          const name = apiFields[field.field];
          if (!name) continue;
          setError(name, { type: "server", message: field.message });
          first ??= name;
        }
        if (first) setFocus(first);
        return;
      }
      if (error instanceof ApiError && error.status === 429) setFormError(waitlistForm.errors.rateLimited);
      else if (error instanceof ApiError) setFormError(error.message);
      else setFormError(waitlistForm.errors.network);
    }
  };

  if (alreadyListed) {
    return (
      <div role="status" className="border-l-4 border-success pl-5">
        <p className="type-h3">{waitlistForm.alreadyOnList.title}</p>
        <Text className="mt-3 max-w-[40ch]">{waitlistForm.alreadyOnList.body}</Text>
      </div>
    );
  }

  const fid = (name: string) => `${id}-${name}`;

  return (
    <form onSubmit={(event) => void handleSubmit(submit)(event)} onFocus={onStart} noValidate aria-label="Join the Mise waitlist" className="grid gap-6">
      <div className="grid gap-6 sm:grid-cols-2">
        <Field id={fid("firstName")} label="First name" error={errors.firstName?.message}>
          <Input
            id={fid("firstName")}
            autoComplete="given-name"
            aria-required="true"
            aria-invalid={errors.firstName ? true : undefined}
            aria-describedby={describedBy(fid("firstName"), { error: errors.firstName })}
            {...register("firstName")}
          />
        </Field>

        <Field id={fid("email")} label="Email" error={errors.email?.message}>
          <Input
            id={fid("email")}
            type="email"
            inputMode="email"
            autoComplete="email"
            autoCapitalize="none"
            spellCheck={false}
            aria-required="true"
            aria-invalid={errors.email ? true : undefined}
            aria-describedby={describedBy(fid("email"), { error: errors.email })}
            {...register("email")}
          />
        </Field>
      </div>

      <div className="grid gap-6 sm:grid-cols-2">
        <Field id={fid("location")} label="Where would you like delivery?" error={errors.location?.message}>
          <Select
            id={fid("location")}
            aria-required="true"
            aria-invalid={errors.location ? true : undefined}
            aria-describedby={describedBy(fid("location"), { error: errors.location })}
            {...register("location")}
          >
            <option value="">Choose an area</option>
            {locations.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
        </Field>

        <Field id={fid("householdSize")} label="Household size" optional error={errors.householdSize?.message}>
          <Select
            id={fid("householdSize")}
            aria-invalid={errors.householdSize ? true : undefined}
            aria-describedby={describedBy(fid("householdSize"), { error: errors.householdSize })}
            {...register("householdSize")}
          >
            <option value="">Prefer not to say</option>
            {householdSizes.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
        </Field>
      </div>

      <ChoiceGroup
        id={fid("dietaryInterests")}
        legend="Dietary interests"
        optional
        hint={waitlistForm.dietaryHint}
        error={errors.dietaryInterests?.message}
      >
        {dietaryInterests.map((option) => (
          <Choice key={option.value} value={option.value} label={option.label} {...register("dietaryInterests")} />
        ))}
      </ChoiceGroup>

      <Field id={fid("phone")} label="Phone" optional hint={waitlistForm.phoneHint} error={errors.phone?.message}>
        <Input
          id={fid("phone")}
          type="tel"
          inputMode="tel"
          autoComplete="tel"
          aria-invalid={errors.phone ? true : undefined}
          aria-describedby={describedBy(fid("phone"), { hint: true, error: errors.phone })}
          {...register("phone")}
        />
      </Field>

      {/*
        Honeypot. People never see or reach this field; automated form-fillers
        usually complete it, and the API rejects the signup when they do.
      */}
      <div aria-hidden="true" className="absolute -left-[200vw] size-px overflow-hidden">
        <label htmlFor={fid("website")}>Leave this field empty</label>
        <input ref={honeypot} id={fid("website")} name="website" type="text" tabIndex={-1} autoComplete="off" defaultValue="" />
      </div>

      <div>
        <Checkbox
          id={fid("consent")}
          label={waitlistForm.consent}
          aria-required="true"
          aria-invalid={errors.consent ? true : undefined}
          aria-describedby={describedBy(fid("consent"), { error: errors.consent })}
          {...register("consent")}
        />
        <p id={`${fid("consent")}-error`} role="alert" className={errors.consent ? "type-caption mt-1.5 font-semibold text-error" : "sr-only"}>
          {errors.consent?.message}
        </p>
      </div>

      <div>
        <MiseButton type="submit" size="lg" className="w-full sm:w-auto" aria-disabled={mutation.isPending} disabled={mutation.isPending}>
          {mutation.isPending ? waitlistForm.submitting : waitlistForm.submit}
        </MiseButton>
        <p role="alert" className={formError ? "type-body-sm mt-4 font-semibold text-error" : "sr-only"}>
          {formError}
        </p>
        <Text size="caption" tone="muted" className="mt-4">
          {waitlistForm.privacyLead}{" "}
          <Link href="/privacy" className="underline underline-offset-2">
            Read the privacy policy
          </Link>
          .
        </Text>
      </div>
    </form>
  );
}
