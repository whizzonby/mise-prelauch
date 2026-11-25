import type { Metadata } from "next";
import Link from "next/link";

import { register } from "@/app/actions";
import { ActionForm } from "@/components/action-form";
import { AuthShell } from "@/components/auth-shell";
import { safeNext } from "@/lib/safe-redirect";

export const metadata: Metadata = { title: "Create an account" };

export default async function RegisterPage({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const next = safeNext((await searchParams).next, "");
  return (
    <AuthShell
      title="Create your Mise account"
      footer={
        <span>
          Already have one?{" "}
          <Link href={next ? `/sign-in?next=${encodeURIComponent(next)}` : "/sign-in"} className="underline underline-offset-4">
            Sign in
          </Link>
        </span>
      }
    >
      <p className="type-body-sm mt-3 text-muted">
        On the waitlist already? Use the same email address and your invitations and preferences come with you.
      </p>
      <ActionForm
        action={register}
        hidden={next ? { next } : undefined}
        submit="Create account"
        pending="Creating your account…"
        fields={[
          { name: "first_name", label: "First name", autoComplete: "given-name" },
          { name: "last_name", label: "Last name", autoComplete: "family-name" },
          { name: "email", label: "Email", type: "email", autoComplete: "email" },
          {
            name: "password",
            label: "Password",
            type: "password",
            autoComplete: "new-password",
            hint: "At least 12 characters. A short sentence works well.",
          },
          { name: "accept_terms", label: "I accept the Mise terms and privacy policy.", type: "checkbox" },
        ]}
      />
    </AuthShell>
  );
}
