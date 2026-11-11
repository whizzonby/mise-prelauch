import { redirect } from "next/navigation";

/** /login is a common guess; sign-in lives at /sign-in. */
export default async function LoginAlias({ searchParams }: { searchParams: Promise<{ next?: string }> }) {
  const { next } = await searchParams;
  redirect(next ? `/sign-in?next=${encodeURIComponent(next)}` : "/sign-in");
}
