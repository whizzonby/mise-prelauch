#!/usr/bin/env node
// LCP and CLS of one URL, median (LCP) and worst (CLS) of N cold loads, desktop and phone.
//   node e2e/tools/vitals.mjs <url> [runs=5]
import { chromium } from "@playwright/test";

const [url, runsArg] = process.argv.slice(2);
const runs = Number(runsArg ?? 5);
const browser = await chromium.launch();
const out = {};
for (const [name, viewport] of [["desktop 1366", { width: 1366, height: 850 }], ["phone 390", { width: 390, height: 844 }]]) {
  const lcps = [];
  const clss = [];
  for (let i = 0; i < runs; i++) {
    const ctx = await browser.newContext({ viewport });
    const page = await ctx.newPage();
    await page.goto(url, { waitUntil: "load" });
    const v = await page.evaluate(
      () =>
        new Promise((resolve) => {
          let lcp = null;
          let cls = 0;
          new PerformanceObserver((l) => {
            const last = l.getEntries().at(-1);
            if (last) lcp = { t: last.startTime, el: last.element ? last.element.tagName + (last.element.getAttribute("alt") ? "[alt]" : "") : "?" };
          }).observe({ type: "largest-contentful-paint", buffered: true });
          new PerformanceObserver((l) => {
            for (const e of l.getEntries()) if (!e.hadRecentInput) cls += e.value;
          }).observe({ type: "layout-shift", buffered: true });
          setTimeout(() => resolve({ lcp, cls }), 2000);
        }),
    );
    lcps.push(v.lcp);
    clss.push(v.cls);
    await ctx.close();
  }
  const sorted = lcps.filter(Boolean).sort((a, b) => a.t - b.t);
  const mid = sorted[Math.floor(sorted.length / 2)];
  out[name] = { lcp_ms: Math.round(mid.t), lcp_element: mid.el, cls_worst: Number(Math.max(...clss).toFixed(4)) };
}
console.log(JSON.stringify({ url, runs, ...out }));
await browser.close();
