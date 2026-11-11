import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { signIn } from "@/app/actions";
import { ActionForm } from "@/components/action-form";
import { AuthShell, Notice } from "@/components/auth-shell";
import { currentCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Sign in" };

type Search = Promise<{ expired?: string; verified?: string; reset?: string }>;

export default async function SignInPage({ searchParams }: { searchParams: Search }) {
  if (await currentCustomer()) redirect("/");
  const { expired, verified, reset } = await searchParams;
  return (
    <AuthShell
      title="Sign in to Mise"
      footer={
        <>
          <Link href="/forgot-password" className="underline underline-offset-4">
            Forgotten your password?
          </Link>
          <span>
            New to Mise?{" "}
            <Link href="/register" className="underline underline-offset-4">
              Create an account
            </Link>
          </span>
        </>
      }
    >
      {verified ? <Notice>Your email is confirmed. Sign in to continue.</Notice> : null}
      {reset ? <Notice>Your password is changed and you are signed out everywhere. Sign in with the new one.</Notice> : null}
      {expired ? <Notice tone="warning">Your session ended. Sign in again to continue.</Notice> : null}
      <ActionForm
        action={signIn}
        submit="Sign in"
        pending="Signing in…"
        fields={[
          { name: "email", label: "Email", type: "email", autoComplete: "username" },
          { name: "password", label: "Password", type: "password", autoComplete: "current-password" },
        ]}
      />
    </AuthShell>
  );
}
