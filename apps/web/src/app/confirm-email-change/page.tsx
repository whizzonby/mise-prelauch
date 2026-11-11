import type { Metadata } from "next";
import Link from "next/link";

import { confirmEmailChange } from "@/app/actions";
import { ActionForm } from "@/components/action-form";
import { AuthShell, Notice } from "@/components/auth-shell";

export const metadata: Metadata = { title: "Confirm your new email address" };

/** Like email verification, the change happens on a button press, not on opening the link. */
export default async function ConfirmEmailChangePage({ searchParams }: { searchParams: Promise<{ token?: string }> }) {
  const { token } = await searchParams;
  return (
    <AuthShell
      title="Use this email address"
      footer={
        <Link href="/profile/security" className="underline underline-offset-4">
          Back to your account
        </Link>
      }
    >
      {token ? (
        <ActionForm
          action={confirmEmailChange}
          hidden={{ token }}
          fields={[]}
          submit="Use this address"
          pending="Changing…"
          doneMessage={
            <Notice>
              Your Mise account now uses this address.{" "}
              <Link href="/profile/security" className="underline underline-offset-4">
                Go to your account
              </Link>
            </Notice>
          }
        />
      ) : (
        <p className="type-body mt-3">This link is incomplete. Open the most recent email from Mise and use its button.</p>
      )}
    </AuthShell>
  );
}
