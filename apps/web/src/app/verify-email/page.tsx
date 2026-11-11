import type { Metadata } from "next";
import Link from "next/link";

import { verifyEmail } from "@/app/actions";
import { ActionForm } from "@/components/action-form";
import { AuthShell } from "@/components/auth-shell";

export const metadata: Metadata = { title: "Confirm your email" };

/**
 * The emailed link lands here, and confirming takes a press of the button.
 * Mail scanners that open every link would otherwise use up the single-use
 * token before the person does.
 */
export default async function VerifyEmailPage({ searchParams }: { searchParams: Promise<{ token?: string }> }) {
  const { token } = await searchParams;
  return (
    <AuthShell
      title="Confirm your email"
      footer={
        <Link href="/sign-in" className="underline underline-offset-4">
          Back to sign in
        </Link>
      }
    >
      {token ? (
        <ActionForm action={verifyEmail} hidden={{ token }} fields={[]} submit="Confirm my email" pending="Confirming…" />
      ) : (
        <p className="type-body mt-3">This link is incomplete. Open the most recent email from Mise and use its button.</p>
      )}
    </AuthShell>
  );
}
