import { notFound, redirect } from "next/navigation";

import { DASHBOARDS } from "@/components/intelligence-ui";
import { can, requireAdmin } from "@/lib/session";

/** Opens the first dashboard this person may see. */
export default async function IntelligencePage() {
  const { admin } = await requireAdmin();
  const first = DASHBOARDS.find((d) => can(admin, d.perm));
  if (!first) notFound();
  redirect(`/intelligence/${first.kind}`);
}
