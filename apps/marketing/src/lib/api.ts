import { createClient } from "@mise/api-client";

import { env } from "./env";

/** Browser-side API client. */
export const api = createClient({ baseUrl: env.apiUrl });

/** Server-side API client. Inside Docker and AWS the API has a private address. */
export function serverApi() {
  return createClient({ baseUrl: process.env.API_INTERNAL_URL ?? env.apiUrl });
}
