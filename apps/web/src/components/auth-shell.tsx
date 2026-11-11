import { Heading, Wordmark } from "@mise/ui";
import Link from "next/link";
import type { ReactNode } from "react";

/**
 * The frame for every signed-out page: the wordmark, one panel, one task.
 * Same tokens, type and hairline rules as the marketing site, at the density
 * of a form.
 */
export function AuthShell({ title, children, footer }: { title: string; children: ReactNode; footer?: ReactNode }) {
  return (
    <main id="main" className="grid min-h-svh place-items-center px-5 py-12">
      <div className="w-full max-w-[28rem]">
        <Link href="/" className="inline-block text-[2.5rem] text-primary" aria-label="Mise account home">
          <Wordmark />
        </Link>
        <div className="mt-8 border-t-4 border-primary bg-surface p-7 sm:p-9">
          <Heading as="h1" size="h3">
            {title}
          </Heading>
          {children}
        </div>
        {footer ? <div className="type-body-sm mt-5 grid gap-2">{footer}</div> : null}
      </div>
    </main>
  );
}

export function Notice({ children, tone = "success" }: { children: ReactNode; tone?: "success" | "warning" }) {
  return (
    <p className={`type-body-sm mt-4 border-l-2 pl-3 ${tone === "success" ? "border-success" : "border-warning"}`}>
      {children}
    </p>
  );
}
