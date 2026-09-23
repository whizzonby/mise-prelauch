import { buttonClasses, Heading, Text } from "@mise/ui";
import Link from "next/link";

import { PageShell } from "@/components/page-shell";

export default function NotFound() {
  return (
    <PageShell>
      <div className="max-w-[40rem]">
        <Heading as="h1" className="text-primary">
          Nothing in this place
        </Heading>
        <Text size="lg" className="mt-6">
          The page you asked for does not exist, or has moved.
        </Text>
        <Link href="/" className={buttonClasses("primary", "lg", "mt-8")}>
          Go to the home page
        </Link>
      </div>
    </PageShell>
  );
}
