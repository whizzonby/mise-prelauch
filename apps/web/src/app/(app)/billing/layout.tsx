import { Container } from "@mise/ui";
import type { ReactNode } from "react";

import { AccountTabs } from "@/components/account-tabs";

export default function BillingLayout({ children }: { children: ReactNode }) {
  return (
    <Container className="py-10 md:py-14">
      <AccountTabs />
      <div className="mt-8">{children}</div>
    </Container>
  );
}
