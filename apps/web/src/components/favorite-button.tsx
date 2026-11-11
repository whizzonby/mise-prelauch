"use client";

import { cx } from "@mise/ui";
import { Heart } from "lucide-react";
import { useOptimistic, useState, useTransition } from "react";

import { setFavorite } from "@/app/(app)/favorite-actions";

/**
 * Save or unsave a meal. The heart changes at once; if the server refuses or
 * the connection fails, it returns to the real state and says why.
 */
export function FavoriteButton({
  mealId,
  mealName,
  initial,
  variant = "icon",
}: {
  mealId: string;
  mealName: string;
  initial: boolean;
  variant?: "icon" | "full";
}) {
  const [saved, setSaved] = useState(initial);
  const [optimistic, setOptimistic] = useOptimistic(saved);
  const [error, setError] = useState<string>();
  const [saving, startTransition] = useTransition();

  const toggle = () => {
    const next = !saved;
    setError(undefined);
    startTransition(async () => {
      setOptimistic(next);
      const res = await setFavorite(mealId, next);
      if (res.ok) setSaved(res.saved);
      else setError(res.error);
    });
  };

  const label = optimistic ? `Remove ${mealName} from saved meals` : `Save ${mealName}`;
  return (
    <span className={cx("inline-flex flex-col", variant === "full" ? "items-start" : "items-end")}>
      <button
        type="button"
        onClick={toggle}
        aria-pressed={optimistic}
        aria-busy={saving}
        aria-label={variant === "icon" ? label : undefined}
        className={cx(
          "inline-flex items-center justify-center gap-2 transition-colors duration-(--duration-fast) ease-interaction",
          variant === "icon"
            ? "size-11 rounded-full border border-border bg-surface text-primary hover:border-foreground"
            : "type-body-sm min-h-11 rounded-md border border-foreground px-4 font-semibold hover:bg-foreground hover:text-surface",
        )}
      >
        <Heart aria-hidden="true" className="size-5" strokeWidth={1.75} fill={optimistic ? "currentColor" : "none"} />
        {variant === "full" ? (optimistic ? "Saved" : "Save this meal") : null}
      </button>
      <span aria-live="polite" className={error ? "type-caption mt-1 max-w-[16rem] font-semibold text-error" : "sr-only"}>
        {error ?? (optimistic === initial ? "" : optimistic ? `${mealName} saved.` : `${mealName} removed from saved meals.`)}
      </span>
    </span>
  );
}
