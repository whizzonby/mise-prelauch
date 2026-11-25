"use client";

import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react";

import { chat } from "./client";

const Panel = lazy(() => import("./panel"));
const preload = () => void import("./panel");

/**
 * The chat launcher: a small button that stays out of the way. The panel's
 * code loads after the page is idle (or on first press), so it never slows
 * the first paint. Opening ?chat=open (the reply email's link) opens it.
 */
export function ChatLauncher({
  hidden = false,
  bottomClass = "bottom-6",
  signInUrl,
}: {
  /** Hide on pages where it would cover payment or checkout actions. */
  hidden?: boolean;
  /** Tailwind bottom offset, e.g. to sit above a phone tab bar. */
  bottomClass?: string;
  signInUrl?: string;
}) {
  const [open, setOpen] = useState(false);
  const [unread, setUnread] = useState(0);
  const button = useRef<HTMLButtonElement>(null);

  const refreshUnread = useCallback(() => {
    void chat
      .session()
      .then((s) => (s.mode === "none" ? null : chat.conversations()))
      .then((r) => r && setUnread(r.items.reduce((n, c) => n + c.unread, 0)))
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    const idle = "requestIdleCallback" in window ? window.requestIdleCallback : (cb: () => void) => window.setTimeout(cb, 1500);
    idle(() => {
      preload();
      refreshUnread();
      if (new URLSearchParams(window.location.search).get("chat") === "open") setOpen(true);
    });
  }, [refreshUnread]);

  const close = useCallback(() => {
    setOpen(false);
    refreshUnread();
    requestAnimationFrame(() => button.current?.focus());
  }, [refreshUnread]);

  if (hidden) return null;

  return (
    <>
      {open ? (
        <Suspense fallback={null}>
          <Panel onClose={close} onUnread={setUnread} signInUrl={signInUrl} />
        </Suspense>
      ) : null}
      <button
        ref={button}
        type="button"
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-label={unread ? `Chat with Mise, ${unread} unread` : "Chat with Mise"}
        onMouseEnter={preload}
        onFocus={preload}
        onClick={() => (open ? close() : setOpen(true))}
        className={`fixed right-4 z-40 inline-flex size-14 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg transition-transform hover:scale-105 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary sm:right-6 ${bottomClass} ${open ? "max-sm:hidden" : ""}`}
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" className="size-6" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
          {open ? <path d="M6 6l12 12M18 6L6 18" /> : <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12Z" />}
        </svg>
        {unread && !open ? (
          <span aria-hidden="true" className="absolute -right-0.5 -top-0.5 inline-flex min-w-5 items-center justify-center rounded-full bg-accent px-1 text-[0.75rem] font-bold text-accent-foreground">
            {unread > 9 ? "9+" : unread}
          </span>
        ) : null}
      </button>
    </>
  );
}
