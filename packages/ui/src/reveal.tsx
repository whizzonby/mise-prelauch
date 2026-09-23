"use client";

import { useEffect, useRef, type CSSProperties, type ReactNode } from "react";

import { cx } from "./cx";

type ImageRevealProps = {
  /** The edge the image is uncovered from. */
  from?: "bottom" | "left";
  /** Seconds to wait after the image enters view. */
  delay?: number;
  className?: string;
  children: ReactNode;
};

const hidden = {
  bottom: "inset(100% 0% 0% 0%)",
  left: "inset(0% 100% 0% 0%)",
} as const;

/**
 * Uncovers a photograph with a moving mask the first time it scrolls into
 * view, like a cloth being drawn back. This is the only scroll-triggered
 * entrance in the system; text never animates in.
 *
 * The image is visible in the server-rendered HTML. The mask is applied only
 * after hydration, only to images still below the fold, and only when motion
 * is allowed, so nothing is ever hidden from a visitor without JavaScript or
 * with reduced motion, and nothing already on screen flickers.
 */
export function ImageReveal({ from = "bottom", delay = 0, className, children }: ImageRevealProps) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    if (node.getBoundingClientRect().top < window.innerHeight) return;

    node.style.clipPath = hidden[from];
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        observer.disconnect();
        node.dataset.revealing = "true";
        node.style.clipPath = "inset(0% 0% 0% 0%)";
      },
      { threshold: 0.2 },
    );
    observer.observe(node);
    return () => {
      observer.disconnect();
      node.style.clipPath = "";
    };
  }, [from]);

  return (
    <div
      ref={ref}
      className={cx(
        "data-[revealing=true]:transition-[clip-path] data-[revealing=true]:duration-(--duration-editorial) data-[revealing=true]:ease-settle",
        className,
      )}
      style={{ transitionDelay: `${delay}s` } as CSSProperties}
    >
      {children}
    </div>
  );
}
