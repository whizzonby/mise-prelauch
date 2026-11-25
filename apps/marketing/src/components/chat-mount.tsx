"use client";

import { ChatLauncher } from "@mise/chat";

import { env } from "@/lib/env";

/** Guest chat on the marketing site. Account questions go to the customer app, signed in. */
export function ChatMount() {
  return <ChatLauncher signInUrl={`${env.appUrl}/sign-in?next=${encodeURIComponent("/dashboard?chat=open")}`} />;
}
