import { Heading, Text, Wordmark } from "@mise/ui";
import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { LoginForm } from "@/components/login-form";
import { api, sessionToken } from "@/lib/session";

export const metadata: Metadata = { title: "Sign in" };

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ expired?: string }> }) {
  // Someone already signed in has no use for this page.
  const token = await sessionToken();
  if (token) {
    const valid = await api.admin.me(token).then(
      () => true,
      () => false,
    );
    if (valid) redirect("/");
  }
  const { expired } = await searchParams;

  return (
    <main className="grid min-h-svh place-items-center px-5 py-12">
      <div className="w-full max-w-[26rem]">
        <p className="text-[2.5rem] text-primary">
          <Wordmark />
        </p>
        <div className="mt-8 border-t-4 border-primary bg-surface p-7 sm:p-9">
          <Heading as="h1" size="h3">
            Sign in to the admin
          </Heading>
          {expired ? (
            <Text size="sm" className="mt-3 border-l-2 border-warning pl-3">
              Your session ended. Sign in again to continue.
            </Text>
          ) : null}
          <LoginForm />
        </div>
        <Text size="caption" tone="muted" className="mt-5">
          For the Mise team only. Sign-ins and exports are recorded.
        </Text>
      </div>
    </main>
  );
}
