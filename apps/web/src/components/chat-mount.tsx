"use client";

import { ChatLauncher } from "@mise/chat";
import { usePathname } from "next/navigation";

/** Pages where the launcher would sit over payment steps, or where chat is the page itself. */
const HIDDEN = ["/subscribe", "/test-checkout", "/support/chat"];

export function ChatMount({ signedIn }: { signedIn: boolean }) {
  const pathname = usePathname();
  const hidden = HIDDEN.some((p) => pathname === p || pathname.startsWith(p + "/"));
  // Above the phone tab bar when signed in (it is 4rem plus the safe area).
  return <ChatLauncher hidden={hidden} bottomClass={signedIn ? "bottom-[calc(5rem+env(safe-area-inset-bottom))] md:bottom-6" : "bottom-6"} />;
}
