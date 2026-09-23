"use client";

import { buttonClasses, Container, cx, Wordmark } from "@mise/ui";
import * as Dialog from "@radix-ui/react-dialog";
import { Menu, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type MouseEvent } from "react";

import { navigation, primaryCta } from "@/content/site";

export function SiteHeader() {
  const [open, setOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const router = useRouter();
  // Where a menu link is taking the visitor, held until the menu has closed.
  const pendingHref = useRef<string | null>(null);

  // The open menu locks page scrolling, and stays locked until its closing
  // animation ends. Navigating to a section before then would not scroll, so
  // the link closes the menu first and the navigation follows.
  const followAfterClose = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    pendingHref.current = event.currentTarget.getAttribute("href");
    setOpen(false);
  };

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      className={cx(
        "sticky top-0 z-40 border-b bg-background transition-colors duration-(--duration-standard) ease-interaction",
        scrolled ? "border-border" : "border-transparent",
      )}
    >
      <Container className="flex h-18 items-center justify-between gap-6">
        <Link href="/" aria-label="Mise home" className="text-[2rem] text-primary">
          <Wordmark />
        </Link>

        <nav aria-label="Main" className="hidden lg:block">
          <ul className="flex items-center gap-8">
            {navigation.map((item) => (
              <li key={item.href}>
                <Link
                  href={item.href}
                  className="type-body-sm underline decoration-transparent decoration-1 underline-offset-[0.4em] transition-colors duration-(--duration-fast) hover:decoration-current"
                >
                  {item.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>

        <div className="flex items-center gap-2">
          <Link href={primaryCta.href} className={buttonClasses("primary", "md", "max-[26rem]:px-3.5")}>
            {primaryCta.label}
          </Link>

          <Dialog.Root open={open} onOpenChange={setOpen}>
            <Dialog.Trigger
              className="inline-flex size-11 items-center justify-center rounded-md lg:hidden"
              aria-label="Open menu"
            >
              <Menu aria-hidden="true" className="size-6" strokeWidth={1.75} />
            </Dialog.Trigger>
            <Dialog.Portal>
              <Dialog.Content
                aria-describedby={undefined}
                onCloseAutoFocus={(event) => {
                  const href = pendingHref.current;
                  if (!href) return;
                  pendingHref.current = null;
                  // Focus follows the visitor to the section, not back to the menu button.
                  event.preventDefault();
                  router.push(href);
                }}
                className="fixed inset-0 z-50 flex flex-col bg-background data-[state=closed]:animate-[menu-out_var(--duration-fast)_var(--ease-interaction)] data-[state=open]:animate-[menu-in_var(--duration-standard)_var(--ease-settle)] motion-reduce:animate-none! lg:hidden"
              >
                <Container className="flex h-18 shrink-0 items-center justify-between">
                  <Dialog.Title className="text-[2rem] text-primary">
                    <Wordmark />
                    <span className="sr-only"> menu</span>
                  </Dialog.Title>
                  <Dialog.Close className="inline-flex size-11 items-center justify-center rounded-md" aria-label="Close menu">
                    <X aria-hidden="true" className="size-6" strokeWidth={1.75} />
                  </Dialog.Close>
                </Container>

                <Container className="flex flex-1 flex-col justify-between overflow-y-auto pb-8 pt-6">
                  <nav aria-label="Main">
                    <ul className="border-t border-foreground">
                      {navigation.map((item) => (
                        <li key={item.href} className="border-b border-border">
                          <Link
                            href={item.href}
                            onClick={followAfterClose}
                            className="type-h2 block py-4 text-primary"
                          >
                            {item.label}
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </nav>
                  <Link
                    href={primaryCta.href}
                    onClick={followAfterClose}
                    className={buttonClasses("primary", "lg", "mt-8 w-full")}
                  >
                    {primaryCta.label}
                  </Link>
                </Container>
              </Dialog.Content>
            </Dialog.Portal>
          </Dialog.Root>
        </div>
      </Container>
    </header>
  );
}
