import type { QueueItem } from "@mise/api-client";
import Link from "next/link";

import { EmptyState, StatusTag } from "@/components/admin-ui";
import { DataTable } from "@/components/commerce-ui";
import { formatDateTime } from "@/lib/format";

/** Recipe versions waiting at a review stage, oldest submission first. */
export function RecipeQueue({ items, empty }: { items: QueueItem[]; empty: string }) {
  if (!items.length) return <EmptyState>{empty}</EmptyState>;
  return (
    <DataTable head={["Recipe", "Version", "Chef", "Written by", "Submitted"]} minWidth="36rem">
      {items.map((q) => (
        <tr key={q.recipe_id}>
          <td className="p-3">
            <Link href={`/recipes/${q.recipe_id}?step=review`} className="font-semibold underline underline-offset-4">
              {q.name}
            </Link>
            <span className="type-caption block text-muted">
              {q.code}
              {q.meal_name ? ` · ${q.meal_name}` : ""}
            </span>
          </td>
          <td className="p-3">
            v{q.version} <StatusTag status={q.status} />
          </td>
          <td className="p-3">{q.chef_name || "—"}</td>
          <td className="p-3">{q.author_name || "—"}</td>
          <td className="p-3">{q.submitted_at ? formatDateTime(q.submitted_at) : "—"}</td>
        </tr>
      ))}
    </DataTable>
  );
}
