import { redirect } from "next/navigation";

import { sessionToken } from "@/lib/session";

/** The app's front door: home for a signed-in customer, the meal kits for anyone else. */
export default async function Root() {
  redirect((await sessionToken()) ? "/dashboard" : "/meals");
}
