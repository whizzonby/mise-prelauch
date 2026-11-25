import { ApiError } from "@mise/api-client";
import { buttonClasses, Container } from "@mise/ui";
import { ArrowLeft } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { TestModeNotice } from "@/components/commerce-bits";
import { PlanCard } from "@/components/plan-card";
import { api } from "@/lib/session";

async function load(slug: string) {
  try {
    return await api.shop.plan(slug);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const plan = await load((await params).slug);
  return { title: `${plan.name} plan`, description: plan.summary, alternates: { canonical: `/plans/${plan.slug}` } };
}

export default async function PlanPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const [plan, info] = await Promise.all([load(slug), api.shop.info()]);
  return (
    <Container className="py-10 md:py-14">
      <Link href="/plans" className="type-body-sm inline-flex min-h-11 items-center gap-2 font-semibold underline-offset-4 hover:underline">
        <ArrowLeft aria-hidden="true" className="size-4" strokeWidth={1.75} /> All plans
      </Link>
      <div className="mt-6 grid gap-10 lg:grid-cols-[1fr_24rem] lg:gap-16">
        <div>
          <h1 className="type-h1">{plan.name}</h1>
          <p className="type-body-lg mt-4 max-w-[40rem] text-muted">{plan.summary}</p>
          {plan.description ? <p className="type-body mt-6 max-w-[40rem] whitespace-pre-line">{plan.description}</p> : null}
          <h2 className="type-h3 mt-10">How a week works</h2>
          <ul className="type-body mt-4 grid max-w-[40rem] gap-3">
            <li>
              Each week you choose {plan.meals_per_week} meal kits from that week&apos;s menu. {info.cutoff_rule}
            </li>
            <li>Each week is paid before its menu closes. If a week isn&apos;t paid, it isn&apos;t delivered and nothing is owed for it.</li>
            <li>Pause for up to {info.max_pause_weeks} weeks, change plan or cancel from your account. Weeks already paid are still delivered.</li>
          </ul>
          {info.payments.test ? <TestModeNotice className="mt-8 max-w-[40rem]" /> : null}
        </div>
        <div>
          <PlanCard plan={plan} />
          {plan.purchasable ? null : (
            <Link href="/plans" className={buttonClasses("secondary", "md", "mt-4 w-full")}>
              See other plans
            </Link>
          )}
        </div>
      </div>
    </Container>
  );
}
