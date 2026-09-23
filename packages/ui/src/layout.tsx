import type { ComponentPropsWithoutRef, ElementType, ReactNode } from "react";

import { cx } from "./cx";

type ContainerProps = ComponentPropsWithoutRef<"div"> & {
  /** `wide` is the page grid; `prose` holds long-form text at a readable measure. */
  width?: "wide" | "prose";
};

export function Container({ width = "wide", className, ...props }: ContainerProps) {
  return (
    <div
      className={cx(
        "mx-auto w-full px-(--page-gutter)",
        width === "wide" ? "max-w-[100rem]" : "max-w-[46rem]",
        className,
      )}
      {...props}
    />
  );
}

const tones = {
  background: "bg-background text-foreground",
  surface: "bg-surface text-foreground",
  primary: "on-dark bg-primary text-primary-foreground",
} as const;

type SectionProps = ComponentPropsWithoutRef<"section"> & {
  tone?: keyof typeof tones;
  /** `flush` removes vertical padding, for sections that manage their own. */
  spacing?: "default" | "tight" | "flush";
};

export function Section({ tone = "background", spacing = "default", className, ...props }: SectionProps) {
  return (
    <section
      className={cx(
        tones[tone],
        spacing === "default" && "py-[clamp(4.5rem,9vw,9.5rem)]",
        spacing === "tight" && "py-[clamp(3rem,6vw,5.5rem)]",
        className,
      )}
      {...props}
    />
  );
}

const headingSizes = {
  display: "type-display",
  h1: "type-h1",
  h2: "type-h2",
  h3: "type-h3",
  h4: "type-h4",
} as const;

type HeadingProps = {
  /** The semantic level. Choose it for document outline, not appearance. */
  as: "h1" | "h2" | "h3" | "h4";
  /** The visual size. Defaults to match the level. */
  size?: keyof typeof headingSizes;
  className?: string;
  id?: string;
  children: ReactNode;
};

export function Heading({ as: Tag, size, className, ...props }: HeadingProps) {
  return <Tag className={cx(headingSizes[size ?? Tag], className)} {...props} />;
}

const textSizes = {
  lg: "type-body-lg",
  base: "type-body",
  sm: "type-body-sm",
  label: "type-label",
  caption: "type-caption",
} as const;

type TextProps<T extends ElementType> = {
  as?: T;
  size?: keyof typeof textSizes;
  tone?: "default" | "muted";
  className?: string;
} & Omit<ComponentPropsWithoutRef<T>, "as" | "className">;

export function Text<T extends ElementType = "p">({ as, size = "base", tone = "default", className, ...props }: TextProps<T>) {
  const Tag: ElementType = as ?? "p";
  // On a dark section "muted" is the foreground at reduced strength, which keeps contrast.
  return (
    <Tag
      className={cx(textSizes[size], tone === "muted" && "text-muted [.on-dark_&]:text-primary-foreground/75", className)}
      {...props}
    />
  );
}

type MediaFrameProps = {
  /** CSS aspect-ratio, e.g. "4 / 5". Fixing it prevents layout shift while the image loads. */
  ratio?: string;
  caption?: ReactNode;
  className?: string;
  children: ReactNode;
};

/**
 * Frames a photograph: a fixed-ratio box with square corners and an optional
 * caption. The child should be an image set to fill and cover.
 */
export function MediaFrame({ ratio = "4 / 5", caption, className, children }: MediaFrameProps) {
  return (
    <figure className={className}>
      <div className="relative overflow-hidden bg-border" style={{ aspectRatio: ratio }}>
        {children}
      </div>
      {caption ? <figcaption className="type-caption mt-2 text-muted [.on-dark_&]:text-primary-foreground/75">{caption}</figcaption> : null}
    </figure>
  );
}
