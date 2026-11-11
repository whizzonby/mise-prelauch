import { mfaComplete } from "@mise/api-client";
import { Heading, Text, Wordmark } from "@mise/ui";
import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { logout } from "@/app/actions";
import { MfaForm } from "@/components/mfa-form";
import { api, requireSession } from "@/lib/session";

export const metadata: Metadata = { title: "Authenticator code" };

/**
 * Second step of signing in. With an authenticator already set up, it asks
 * for a code. Without one (required outside local development), it shows the
 * QR code to add Mise to an app and asks for the first code.
 */
export default async function MfaPage() {
  const { token, admin } = await requireSession();
  if (mfaComplete(admin.mfa)) redirect("/");

  const enrolling = admin.mfa === "enrolment_required";
  const setup = enrolling ? await api.admin.mfaSetup(token) : null;

  return (
    <main className="grid min-h-svh place-items-center px-5 py-12">
      <div className="w-full max-w-[30rem]">
        <p className="text-[2.5rem] text-primary">
          <Wordmark />
        </p>
        <div className="mt-8 border-t-4 border-primary bg-surface p-7 sm:p-9">
          <Heading as="h1" size="h3">
            {enrolling ? "Set up your authenticator" : "Enter your code"}
          </Heading>
          {setup ? (
            <>
              <Text size="sm" className="mt-3">
                Admin accounts use a second step to sign in. Scan this code with an authenticator app
                (Google Authenticator, 1Password, Authy or similar), then enter the 6-digit code it shows.
              </Text>
              {/* A data: URL from the API; nothing is loaded from elsewhere. */}
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={`data:image/png;base64,${setup.qr_png_base64}`}
                width={200}
                height={200}
                alt="QR code for adding Mise to an authenticator app"
                className="mt-5 border border-border"
              />
              <Text size="caption" tone="muted" className="mt-3">
                Cannot scan? Enter this key by hand:{" "}
                <code className="break-all font-semibold text-foreground">{setup.secret}</code>
              </Text>
            </>
          ) : (
            <Text size="sm" className="mt-3">
              Signed in as {admin.email}. Open your authenticator app and enter the 6-digit code for Mise.
            </Text>
          )}
          <MfaForm mode={enrolling ? "enrol" : "verify"} />
        </div>
        <form action={logout} className="mt-5">
          <button type="submit" className="type-body-sm underline underline-offset-4 hover:decoration-2">
            Use a different account
          </button>
        </form>
        <Text size="caption" tone="muted" className="mt-3">
          Lost your authenticator? Ask a super administrator to reset it.
        </Text>
      </div>
    </main>
  );
}
