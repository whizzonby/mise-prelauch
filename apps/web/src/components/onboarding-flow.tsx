"use client";

import type { OnboardingStep, Reference } from "@mise/api-client";
import { buttonClasses, Choice, ChoiceGroup, cx, MiseButton } from "@mise/ui";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, useTransition, type FormEvent, type ReactNode } from "react";

import { goBack, skipStep, submitStep, type StepResult } from "@/app/onboarding/actions";
import { availabilityText } from "@/lib/format";
import { householdOptions } from "@/lib/options";
import type { DeliveryAvailability } from "@mise/api-client";

import { AllergyFields } from "./allergy-fields";
import { ChoiceField, FormMessage, FormStateProvider, SelectField, TextField } from "./forms";

export interface OnboardingData {
  firstName: string;
  householdSize: number | null;
  interests: string[];
  dietary: string[];
  allergies: { allergen: string; detail: string | null }[];
  region: string | null;
  availability: DeliveryAvailability;
  hasAddress: boolean;
}

const QUESTIONS: OnboardingStep[] = ["household", "interests", "dietary", "allergies", "location"];

export function OnboardingFlow({
  initialStep,
  initialSkipped,
  data,
  reference,
}: {
  initialStep: OnboardingStep;
  initialSkipped: string[];
  data: OnboardingData;
  reference: Reference;
}) {
  const router = useRouter();
  const [step, setStep] = useState<OnboardingStep>(initialStep);
  const [skipped, setSkipped] = useState<string[]>(initialSkipped);
  const [state, setState] = useState<StepResult>({});
  const [pending, startTransition] = useTransition();
  const heading = useRef<HTMLHeadingElement>(null);
  const first = useRef(true);

  // Each new step is announced by moving focus to its question.
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    heading.current?.focus();
  }, [step]);

  const apply = (res: StepResult) => {
    if (res.step) {
      setStep(res.step);
      setSkipped(res.skipped ?? []);
      setState({});
      router.refresh(); // so a step revisited shows what was saved
    } else {
      setState(res);
    }
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    startTransition(async () => apply(await submitStep(step, skipped, form)));
  };
  const skip = () => startTransition(async () => apply(await skipStep(step, skipped)));
  const back = () => startTransition(async () => apply(await goBack(step, skipped)));

  const position = QUESTIONS.indexOf(step);
  const isQuestion = position >= 0;

  return (
    <div className="mx-auto w-full max-w-[40rem]">
      {isQuestion ? (
        <div className="mb-8">
          <p className="type-caption type-figures font-semibold text-muted" aria-live="polite">
            Question {position + 1} of {QUESTIONS.length}
          </p>
          <ol className="mt-2 grid grid-cols-5 gap-1.5" aria-hidden="true">
            {QUESTIONS.map((q, i) => (
              <li key={q} className={cx("h-1 rounded-full transition-colors duration-(--duration-standard)", i <= position ? "bg-primary" : "bg-border")} />
            ))}
          </ol>
        </div>
      ) : null}

      <div key={step} className="motion-safe:animate-[step-in_var(--duration-standard)_var(--ease-settle)]">
        <FormStateProvider value={state}>
          {step === "welcome" ? (
            <Panel heading={heading} title={`Welcome to Mise, ${data.firstName}.`}>
              <p className="type-body-lg mt-4 text-muted">
                Tell us a little about your kitchen: who you cook for, what you like, and anything you cannot eat. It takes
                about a minute, and every question except the first can be skipped.
              </p>
              <p className="type-body mt-4 text-muted">
                We use your answers to suggest meals that suit you. You can change them at any time in your account.
              </p>
              <form onSubmit={submit} className="mt-10">
                <FormMessage />
                <MiseButton type="submit" size="lg" disabled={pending}>
                  {pending ? "One moment…" : "Let’s get started"}
                </MiseButton>
              </form>
            </Panel>
          ) : null}

          {step === "household" ? (
            <Question heading={heading} title="Who are you cooking for?" onSubmit={submit} onBack={back} pending={pending}>
              <HouseholdChoice size={data.householdSize} />
              <p className="type-caption text-muted">
                Not everyone has to eat the same. You can tell us about each person&rsquo;s needs later.
              </p>
            </Question>
          ) : null}

          {step === "interests" ? (
            <Question heading={heading} title="What kind of meals interest you?" onSubmit={submit} onBack={back} onSkip={skip} pending={pending}>
              <ChoiceField name="meal_interests" legend="Choose as many as you like" options={reference.meal_interests} selected={data.interests} />
            </Question>
          ) : null}

          {step === "dietary" ? (
            <Question heading={heading} title="Any dietary preferences?" onSubmit={submit} onBack={back} onSkip={skip} pending={pending}>
              <ChoiceField
                name="dietary"
                legend="Choose any that apply"
                hint="Vegan already includes vegetarian. Allergies come next, separately."
                options={[...reference.dietary, { key: "no-preference", label: "No specific preference" }]}
                selected={data.dietary}
              />
            </Question>
          ) : null}

          {step === "allergies" ? (
            <Question heading={heading} title="Allergies and restrictions" onSubmit={submit} onBack={back} onSkip={skip} pending={pending} skipLabel="Answer later">
              <p className="type-body-sm border-l-2 border-warning bg-surface p-4">{reference.cross_contact_notice}</p>
              <AllergyFields options={reference.allergens} allergies={data.allergies} legend="Does anyone you cook for have a food allergy?" />
            </Question>
          ) : null}

          {step === "location" ? (
            <Question heading={heading} title="Where would you like deliveries?" onSubmit={submit} onBack={back} onSkip={skip} pending={pending}>
              <p className="type-body-sm text-muted">Mise is starting in Trinidad and Tobago. Country: Trinidad and Tobago.</p>
              <SelectField name="delivery_region" label="Region" options={reference.regions} defaultValue={data.region ?? ""} placeholder="Choose a region" />
              {data.hasAddress ? null : (
                <details className="border-t border-border pt-4">
                  <summary className="type-body-sm min-h-11 cursor-pointer content-center font-semibold underline underline-offset-4">
                    Add a street address now (optional)
                  </summary>
                  <div className="mt-4 grid gap-5">
                    <TextField name="line1" label="Street address" autoComplete="address-line1" maxLength={120} />
                    <div className="grid gap-5 sm:grid-cols-2">
                      <TextField name="area" label="Area" optional maxLength={80} />
                      <TextField name="city" label="Town or city" autoComplete="address-level2" maxLength={80} />
                    </div>
                  </div>
                </details>
              )}
              <p className="type-caption border-l-2 border-border-strong pl-3 text-muted">{availabilityText(data.availability)}</p>
            </Question>
          ) : null}

          {step === "complete" ? (
            <Panel heading={heading} title="Your Mise kitchen is ready.">
              <p className="type-body-lg mt-4 text-muted">
                {skipped.length
                  ? "Thank you. You skipped a question or two; answer them any time from your account."
                  : "Thank you. The meals we suggest now follow what you told us."}
              </p>
              <div className="mt-10 flex flex-wrap items-center gap-4">
                <Link href="/meals" className={buttonClasses("primary", "lg")}>
                  Explore meals
                </Link>
                <Link href="/dashboard" className="type-body-sm font-semibold underline underline-offset-4">
                  Go to your home page
                </Link>
              </div>
            </Panel>
          ) : null}
        </FormStateProvider>
      </div>
    </div>
  );
}

function Panel({ heading, title, children }: { heading: React.RefObject<HTMLHeadingElement | null>; title: string; children: ReactNode }) {
  return (
    <section>
      <h1 ref={heading} tabIndex={-1} className="type-h1 outline-none">
        {title}
      </h1>
      {children}
    </section>
  );
}

function Question({
  heading,
  title,
  children,
  onSubmit,
  onBack,
  onSkip,
  pending,
  skipLabel = "Skip",
}: {
  heading: React.RefObject<HTMLHeadingElement | null>;
  title: string;
  children: ReactNode;
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  onBack: () => void;
  onSkip?: () => void;
  pending: boolean;
  skipLabel?: string;
}) {
  return (
    <section>
      <h1 ref={heading} tabIndex={-1} className="type-h2 outline-none">
        {title}
      </h1>
      <form onSubmit={onSubmit} noValidate className="mt-8 grid gap-6">
        {children}
        <FormMessage />
        <div className="flex flex-wrap items-center gap-x-6 gap-y-3 border-t border-border pt-6">
          <MiseButton type="submit" size="lg" disabled={pending}>
            {pending ? "Saving…" : "Continue"}
          </MiseButton>
          {onSkip ? (
            <button type="button" onClick={onSkip} disabled={pending} className="type-body-sm min-h-11 font-semibold underline underline-offset-4">
              {skipLabel}
            </button>
          ) : null}
          <button type="button" onClick={onBack} disabled={pending} className="type-body-sm ml-auto inline-flex min-h-11 items-center gap-1.5 font-semibold text-muted hover:text-foreground">
            <ArrowLeft aria-hidden="true" className="size-4" strokeWidth={1.75} /> Back
          </button>
        </div>
      </form>
    </section>
  );
}

/** Four quick answers, and the exact number for anyone who wants to give it. */
function HouseholdChoice({ size }: { size: number | null }) {
  const initial = size == null ? "" : size === 1 ? "1" : size === 2 ? "2" : size <= 4 ? "family" : "large";
  const [choice, setChoice] = useState(initial);
  const [count, setCount] = useState(size && size > 2 ? String(size) : "");
  return (
    <>
      <ChoiceGroup id="household" legend="Choose one">
        {householdOptions.map((o) => (
          <Choice
            key={o.key}
            type="radio"
            name="household"
            value={o.key}
            label={o.label}
            checked={choice === o.key}
            onChange={() => {
              setChoice(o.key);
              setCount(o.key === "family" || o.key === "large" ? String(o.size) : "");
            }}
          />
        ))}
      </ChoiceGroup>
      <div hidden={choice !== "family" && choice !== "large"}>
        <TextField
          name="size"
          label="How many people, exactly?"
          type="number"
          inputMode="numeric"
          min={1}
          max={20}
          value={count}
          onChange={(e) => setCount(e.currentTarget.value)}
          className="max-w-[12rem]"
        />
      </div>
    </>
  );
}
