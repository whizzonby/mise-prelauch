import { ConversationView } from "@mise/chat";
import { Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { requireCustomer } from "@/lib/session";

export const metadata: Metadata = { title: "Conversation" };

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/**
 * One conversation on its own page (the reply email links here). It loads
 * through this app's chat relay, and the API shows it only if it belongs to
 * the signed-in customer.
 */
export default async function ConversationPage({ params }: { params: Promise<{ id: string }> }) {
  await requireCustomer();
  const { id } = await params;
  if (!ID.test(id)) notFound();
  return (
    <Container className="py-8 md:py-12">
      <Link href="/dashboard?chat=open" className="type-body-sm font-semibold underline underline-offset-4">
        All messages
      </Link>
      <div className="mt-6 flex h-[min(44rem,calc(100svh-14rem))] max-w-[44rem] flex-col overflow-hidden rounded-2xl border border-border bg-background">
        <ConversationView key={id} id={id} />
      </div>
    </Container>
  );
}
