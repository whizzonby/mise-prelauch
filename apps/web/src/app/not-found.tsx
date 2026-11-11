import Link from "next/link";

import { AuthShell } from "@/components/auth-shell";

export default function NotFound() {
  return (
    <AuthShell title="Page not found">
      <p className="type-body mt-3">
        That page does not exist.{" "}
        <Link href="/" className="underline underline-offset-4">
          Go to your account
        </Link>
        .
      </p>
    </AuthShell>
  );
}
