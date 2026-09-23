"use client";

import * as Accordion from "@radix-ui/react-accordion";
import type { ReactNode } from "react";

import { cx } from "./cx";

type FAQListProps = {
  /** Called with the item value when a question is opened. */
  onOpen?: (value: string) => void;
  className?: string;
  children: ReactNode;
};

export function FAQList({ onOpen, className, children }: FAQListProps) {
  return (
    <Accordion.Root
      type="single"
      collapsible
      className={cx("border-t border-foreground", className)}
      onValueChange={(value) => {
        if (value) onOpen?.(value);
      }}
    >
      {children}
    </Accordion.Root>
  );
}

type FAQItemProps = {
  /** A stable identifier for the question (used for analytics, not shown). */
  value: string;
  question: string;
  children: ReactNode;
};

export function FAQItem({ value, question, children }: FAQItemProps) {
  return (
    <Accordion.Item value={value} className="border-b border-border">
      <Accordion.Header asChild>
        <h3>
          <Accordion.Trigger className="group flex w-full items-start justify-between gap-6 py-6 text-left type-h4 md:text-[1.375rem]">
            <span>{question}</span>
            {/* A plus that loses its upright stroke when open. */}
            <span aria-hidden="true" className="relative mt-1.5 size-4 shrink-0">
              <span className="absolute inset-x-0 top-1/2 h-0.5 -translate-y-1/2 bg-current" />
              <span className="absolute inset-y-0 left-1/2 w-0.5 -translate-x-1/2 bg-current transition-transform duration-(--duration-standard) ease-interaction group-data-[state=open]:scale-y-0" />
            </span>
          </Accordion.Trigger>
        </h3>
      </Accordion.Header>
      <Accordion.Content className="overflow-hidden data-[state=closed]:animate-[faq-close_var(--duration-standard)_var(--ease-interaction)] data-[state=open]:animate-[faq-open_var(--duration-standard)_var(--ease-interaction)] motion-reduce:animate-none!">
        <div className="type-body max-w-[62ch] pb-7 text-muted">{children}</div>
      </Accordion.Content>
    </Accordion.Item>
  );
}
