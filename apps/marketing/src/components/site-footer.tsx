import { Container, Wordmark } from "@mise/ui";
import Link from "next/link";

import { footer, navigation, site } from "@/content/site";

export function SiteFooter() {
  return (
    <footer className="on-dark bg-foreground text-surface">
      <Container className="py-16 md:py-20">
        <div className="grid gap-12 md:grid-cols-12">
          <div className="md:col-span-6">
            <p className="text-[clamp(4.5rem,14vw,11rem)]">
              <Wordmark />
            </p>
            <p className="type-body-lg mt-4 max-w-[28ch] text-surface/75">{footer.line}</p>
          </div>

          <nav aria-label="Footer" className="md:col-span-3 md:col-start-8">
            <ul className="space-y-3">
              {navigation.map((item) => (
                <li key={item.href}>
                  <Link href={item.href} className="type-body underline decoration-transparent underline-offset-[0.35em] hover:decoration-current">
                    {item.label}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>

          <div className="md:col-span-2">
            <ul className="space-y-3">
              {footer.legal.map((item) => (
                <li key={item.href}>
                  <Link href={item.href} className="type-body underline decoration-transparent underline-offset-[0.35em] hover:decoration-current">
                    {item.label}
                  </Link>
                </li>
              ))}
              <li>
                <a href={`mailto:${site.contactEmail}`} className="type-body underline decoration-transparent underline-offset-[0.35em] hover:decoration-current">
                  {site.contactEmail}
                </a>
              </li>
            </ul>
          </div>
        </div>

        <p className="type-caption mt-16 border-t border-surface/20 pt-6 text-surface/75">
          © {new Date().getFullYear()} {site.name}. Made in {site.origin}.
        </p>
      </Container>
    </footer>
  );
}
