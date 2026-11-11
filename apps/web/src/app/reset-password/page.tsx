import type { Metadata } from "next";
import Link from "next/link";

import { resetPassword } from "@/app/actions";
import { ActionForm } from "@/components/action-form";
import { AuthShell } from "@/components/auth-shell";

export const metadata: Metadata = { title: "Choose a new password" };

export default async function ResetPasswordPage({ searchParams }: { searchParams: Promise<{ token?: string }> }) {
  const { token } = await searchParams;
  return (
    <AuthShell
      title="Choose a new password"
      footer={
        <Link href="/forgot-password" className="underline underline-offset-4">
          Ask for a new link
        </Link>
      }
    >
      {token ? (
        <ActionForm
          action={resetPassword}
          hidden={{ token }}
          submit="Save new password"
          pending="Saving…"
          fields={[
            {
              name: "password",
              label: "New password",
              type: "password",
              autoComplete: "new-password",
              hint: "At least 12 characters. You will be signed out everywhere.",
            },
          ]}
        />
      ) : (
        <p className="type-body mt-3">This link is incomplete. Ask for a new one.</p>
      )}
    </AuthShell>
  );
}
