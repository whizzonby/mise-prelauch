import { Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";

import { api } from "@/lib/session";

export const metadata: Metadata = {
  title: "Help centre",
  description: "Answers about Mise meal kits, subscriptions, billing, delivery and your account.",
  alternates: { canonical: "/help" },
};

/** The public help centre: published articles only, searchable, by category. */
export default async function HelpPage({ searchParams }: { searchParams: Promise<{ q?: string; category?: string }> }) {
  const sp = await searchParams;
  const q = (sp.q ?? "").slice(0, 100);
  const category = sp.category ?? "";
  const [{ items: categories }, { items: articles }] = await Promise.all([api.help.categories(), api.help.articles({ q, category })]);
  const current = categories.find((c) => c.slug === category);
  const withQ = q ? `q=${encodeURIComponent(q)}` : "";

  return (
    <Container className="py-10 md:py-14">
      <p className="type-caption font-semibold uppercase tracking-[0.08em] text-accent">Help centre</p>
      <h1 className="type-h1 mt-2 max-w-[20ch]">How can we help?</h1>
      <form role="search" action="/help" className="mt-8 flex max-w-[40rem] gap-3">
        {category ? <input type="hidden" name="category" value={category} /> : null}
        <label htmlFor="help-q" className="sr-only">
          Search help articles
        </label>
        <input
          id="help-q"
          name="q"
          defaultValue={q}
          maxLength={100}
          placeholder="Search, for example: pause, delivery day, refund"
          className="type-body min-h-12 flex-1 rounded-md border border-border bg-surface px-4"
        />
        <button type="submit" className="min-h-12 rounded-md bg-primary px-5 font-semibold text-primary-foreground">
          Search
        </button>
      </form>

      <div className="mt-10 grid gap-10 lg:grid-cols-[16rem_1fr]">
        <nav aria-label="Help categories">
          <ul className="grid gap-1">
            <li>
              <Link
                href={withQ ? `/help?${withQ}` : "/help"}
                aria-current={!category ? "page" : undefined}
                className={`type-body-sm flex min-h-11 items-center rounded-md px-3 ${!category ? "bg-surface font-semibold" : "hover:bg-surface"}`}
              >
                All topics
              </Link>
            </li>
            {categories.map((c) => (
              <li key={c.slug}>
                <Link
                  href={`/help?category=${c.slug}${withQ ? `&${withQ}` : ""}`}
                  aria-current={category === c.slug ? "page" : undefined}
                  className={`type-body-sm flex min-h-11 items-center justify-between rounded-md px-3 ${category === c.slug ? "bg-surface font-semibold" : "hover:bg-surface"}`}
                >
                  {c.name}
                  <span className="type-caption text-muted">{c.articles}</span>
                </Link>
              </li>
            ))}
          </ul>
        </nav>

        <section aria-labelledby="results">
          <h2 id="results" className="type-h3">
            {q ? `Results for "${q}"` : (current?.name ?? "All articles")}
          </h2>
          {articles.length ? (
            <ul className="mt-5 divide-y divide-border border-y border-border">
              {articles.map((a) => (
                <li key={a.slug}>
                  <Link href={`/help/${a.slug}`} className="block py-4 hover:bg-surface">
                    <span className="type-h4 block">{a.title}</span>
                    {a.summary ? <span className="type-body-sm mt-1 block text-muted">{a.summary}</span> : null}
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <p className="type-body mt-5 text-muted">
              {q || category ? "No articles match yet." : "Help articles are being written."} Use the chat button to ask us directly.
            </p>
          )}
        </section>
      </div>
    </Container>
  );
}
