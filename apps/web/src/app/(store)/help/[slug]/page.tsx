import { ApiError } from "@mise/api-client";
import { Container } from "@mise/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { api } from "@/lib/session";

async function load(slug: string) {
  try {
    return await api.help.article(slug);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const a = await load((await params).slug);
  return a ? { title: a.title, description: a.summary || undefined, alternates: { canonical: `/help/${a.slug}` } } : { title: "Help centre" };
}

const updated = new Intl.DateTimeFormat("en-TT", { dateStyle: "long", timeZone: "America/Port_of_Spain" });

/** One published help article. The body is plain text: paragraphs, never HTML. */
export default async function HelpArticlePage({ params }: { params: Promise<{ slug: string }> }) {
  const a = await load((await params).slug);
  if (!a) notFound();
  return (
    <Container className="py-10 md:py-14">
      <Link href="/help" className="type-body-sm font-semibold underline underline-offset-4">
        Help centre
      </Link>
      <article className="mt-6 max-w-[44rem]">
        <h1 className="type-h1">{a.title}</h1>
        {a.summary ? <p className="type-body-lg mt-4 text-muted">{a.summary}</p> : null}
        <div className="mt-8 grid gap-5">
          {a.body.split(/\n{2,}/).map((p, i) => (
            <p key={i} className="type-body whitespace-pre-line">
              {p}
            </p>
          ))}
        </div>
        <p className="type-caption mt-10 text-muted">Updated {updated.format(new Date(a.updated_at))}</p>
      </article>
      <p className="type-body mt-10 border-t border-border pt-6">Still need help? Use the chat button to send us a message.</p>
    </Container>
  );
}
