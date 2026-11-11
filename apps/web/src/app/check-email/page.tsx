import type { Metadata } from "next";
import Link from "next/link";

import { AuthShell } from "@/components/auth-shell";

export const metadata: Metadata = { title: "Check your email" };

export default function CheckEmailPage() {
  return (
    <AuthShell
      title="Check your email"
      footer={
        <Link href="/sign-in" className="underline underline-offset-4">
          Back to sign in
        </Link>
      }
    >
      <p className="type-body mt-3">
        We have sent a link to confirm your address. It works once, for 48 hours. If nothing arrives in a few minutes,
        look in your spam folder.
      </p>
    </AuthShell>
  );
}
