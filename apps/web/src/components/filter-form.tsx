"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { SlidersHorizontal } from "lucide-react";
import { useEffect, useRef, useState, useTransition, type ReactNode } from "react";

/**
 * The meal filters are an ordinary GET form: they work before JavaScript
 * loads, and every filtered view has its own URL. With JavaScript, changing a
 * choice applies it straight away (text search waits for Enter or the button).
 */
export function FilterForm({ children, className }: { children: ReactNode; className?: string }) {
  const router = useRouter();
  const ref = useRef<HTMLFormElement>(null);
  const [pending, startTransition] = useTransition();
  const search = useSearchParams();

  // When the URL changes another way (back button, "Clear all filters"), the
  // fields follow it. Focus stays where the person left it.
  useEffect(() => {
    const form = ref.current;
    if (!form) return;
    for (const el of Array.from(form.elements)) {
      if (el instanceof HTMLInputElement && (el.type === "checkbox" || el.type === "radio")) {
        el.checked = search.getAll(el.name).includes(el.value);
      } else if (el instanceof HTMLInputElement || el instanceof HTMLSelectElement) {
        if (el.name && document.activeElement !== el) el.value = search.get(el.name) ?? "";
      }
    }
  }, [search]);

  const apply = () => {
    const form = ref.current;
    if (!form) return;
    const params = new URLSearchParams();
    for (const [key, value] of new FormData(form)) {
      if (typeof value === "string" && value !== "") params.append(key, value);
    }
    const query = params.toString();
    startTransition(() => router.push(query ? `/meals?${query}` : "/meals", { scroll: false }));
  };

  return (
    <form
      ref={ref}
      action="/meals"
      method="get"
      role="search"
      aria-label="Filter meals"
      aria-busy={pending}
      className={className}
      onClick={(e) => {
        // "Clear all filters" empties the fields at once, before the page reloads.
        if ((e.target as HTMLElement).closest("a[data-clear-filters]")) ref.current?.reset();
      }}
      onChange={(e) => {
        if ((e.target as HTMLElement).getAttribute("type") !== "search") apply();
      }}
      onSubmit={(e) => {
        e.preventDefault();
        apply();
      }}
    >
      {children}
    </form>
  );
}

/**
 * The filters below search. On a phone they fold away behind one button so
 * the meals are on the first screen; from a laptop up they are always open.
 */
export function FilterPanel({ active, children }: { active: number; children: ReactNode }) {
  const [open, setOpen] = useState(active > 0);
  useEffect(() => {
    const wide = window.matchMedia("(min-width: 64rem)");
    const sync = () => {
      if (wide.matches) setOpen(true);
    };
    sync();
    wide.addEventListener("change", sync);
    return () => wide.removeEventListener("change", sync);
  }, []);
  return (
    <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)} className="filter-panel mt-5">
      <summary className="type-body-sm inline-flex min-h-11 cursor-pointer list-none items-center gap-2 rounded-md border border-border-strong bg-surface px-4 font-semibold lg:hidden [&::-webkit-details-marker]:hidden">
        <SlidersHorizontal aria-hidden="true" className="size-4" strokeWidth={1.75} />
        Filters{active ? ` (${active})` : ""}
      </summary>
      <div className="mt-5 lg:mt-0">{children}</div>
    </details>
  );
}
