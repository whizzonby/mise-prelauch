import type { Metadata } from "next";
import Link from "next/link";

import { forgotPassword } from "@/app/actions";
import { ActionForm } from "@/components/action-form";
import { AuthShell } from "@/components/auth-shell";

export const metadata: Metadata = { title: "Reset your password" };

export default function ForgotPasswordPage() {
  return (
    <AuthShell
      title="Reset your password"
      footer={
        <Link href="/sign-in" className="underline underline-offset-4">
          Back to sign in
        </Link>
      }
    >
      <ActionForm
        action={forgotPassword}
        submit="Send reset link"
        pending="Sending…"
        fields={[{ name: "email", label: "Email", type: "email", autoComplete: "email" }]}
        doneMessage={
          <p className="type-body">
            If there is a Mise account for that address, a link to reset its password is on its way. It works once, for
            an hour.
          </p>
        }
      />
    </AuthShell>
  );
}
