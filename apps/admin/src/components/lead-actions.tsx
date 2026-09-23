"use client";

import type { LeadStatus } from "@mise/api-client";
import { Checkbox, Field, MiseButton, Select, Text } from "@mise/ui";
import { useActionState } from "react";

import { eraseLead, setLeadStatus, type FormState } from "@/app/actions";

const OPTIONS: { value: LeadStatus; label: string; meaning: string }[] = [
  { value: "VERIFIED", label: "Verified", meaning: "On the list and receiving email." },
  { value: "QUALIFIED", label: "Qualified", meaning: "A strong candidate for the first launch group." },
  { value: "UNSUBSCRIBED", label: "Unsubscribed", meaning: "Stays on record but receives no email." },
  { value: "BLOCKED", label: "Blocked", meaning: "Abuse. Excluded from email, statistics and referral counts." },
];

export function StatusForm({ id, current }: { id: string; current: LeadStatus }) {
  const [state, action, pending] = useActionState<FormState, FormData>(setLeadStatus, {});
  return (
    <form action={action} className="grid gap-4">
      <input type="hidden" name="id" value={id} />
      <Field id="status" label="New status" hint="Pending and Converted are set by the lead's own actions, not by hand.">
        <Select id="status" name="status" defaultValue={OPTIONS.some((o) => o.value === current) ? current : "VERIFIED"}>
          {OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}: {option.meaning}
            </option>
          ))}
        </Select>
      </Field>
      <p role="alert" className={state.error ? "type-body-sm font-semibold text-error" : "sr-only"}>
        {state.error}
      </p>
      <div>
        <MiseButton type="submit" disabled={pending}>
          {pending ? "Saving…" : "Save status"}
        </MiseButton>
      </div>
    </form>
  );
}

export function EraseLeadForm({ id, name }: { id: string; name: string }) {
  const [state, action, pending] = useActionState<FormState, FormData>(eraseLead, {});
  return (
    <form action={action} className="grid gap-4">
      <input type="hidden" name="id" value={id} />
      <Text size="sm">
        Use this when {name} asks for their details to be removed. It deletes the lead, their preferences, attribution,
        activity and referral links. It cannot be undone.
      </Text>
      <Checkbox id="confirm" name="confirm" label={`Delete everything Mise holds about ${name}`} />
      <p role="alert" className={state.error ? "type-body-sm font-semibold text-error" : "sr-only"}>
        {state.error}
      </p>
      <div>
        <button
          type="submit"
          disabled={pending}
          className="inline-flex min-h-11 items-center rounded-md border border-error px-5 font-semibold text-error transition-colors duration-(--duration-fast) hover:bg-error hover:text-surface disabled:opacity-55"
        >
          {pending ? "Deleting…" : "Delete lead"}
        </button>
      </div>
    </form>
  );
}
