import { Container } from "@mise/ui";
import type { ReactNode } from "react";

import { AccountTabs } from "@/components/account-tabs";

/** Subscription, orders and billing share one set of tabs. */
export default function SubscriptionLayout({ children }: { children: ReactNode }) {
  return (
    <Container className="py-10 md:py-14">
      <AccountTabs />
      <div className="mt-8">{children}</div>
    </Container>
  );
}
