#!/usr/bin/env node
// Small HTTP load generator for local performance evidence (Sprint 9).
// Never point it at production.
//
//   node e2e/tools/load.mjs <name> <concurrency> <seconds> <url> [<url> ...]
//   HEADERS='{"Authorization":"Bearer …"}' node e2e/tools/load.mjs admin-dash 10 20 http://localhost:8090/api/v1/admin/analytics/overview
//
// Each virtual user requests the URLs round-robin, as fast as responses
// return. Prints one JSON line: requests, throughput, error rate and the
// latency distribution (ms).

const [name, conc, secs, ...targets] = process.argv.slice(2);
if (!name || !conc || !secs || targets.length === 0) {
  console.error("usage: load.mjs <name> <concurrency> <seconds> <url>...");
  process.exit(2);
}
const headers = process.env.HEADERS ? JSON.parse(process.env.HEADERS) : {};
const concurrency = Number(conc);
const until = Date.now() + Number(secs) * 1000;
const latencies = [];
let errors = 0;
const statuses = {};

async function user(offset) {
  let i = offset;
  while (Date.now() < until) {
    const url = targets[i++ % targets.length];
    const t0 = performance.now();
    try {
      const res = await fetch(url, { headers, signal: AbortSignal.timeout(30_000) });
      await res.arrayBuffer();
      statuses[res.status] = (statuses[res.status] ?? 0) + 1;
      if (res.status >= 400) errors++;
    } catch {
      errors++;
      statuses.network = (statuses.network ?? 0) + 1;
    }
    latencies.push(performance.now() - t0);
  }
}

const started = performance.now();
await Promise.all(Array.from({ length: concurrency }, (_, k) => user(k)));
const elapsed = (performance.now() - started) / 1000;
latencies.sort((a, b) => a - b);
const pct = (p) => (latencies.length ? Math.round(latencies[Math.min(latencies.length - 1, Math.floor((p / 100) * latencies.length))]) : null);
console.log(
  JSON.stringify({
    name,
    concurrency,
    seconds: Math.round(elapsed * 10) / 10,
    requests: latencies.length,
    rps: Math.round((latencies.length / elapsed) * 10) / 10,
    error_rate: latencies.length ? Math.round((errors / latencies.length) * 10000) / 100 : null,
    statuses,
    p50: pct(50),
    p90: pct(90),
    p95: pct(95),
    p99: pct(99),
    max: latencies.length ? Math.round(latencies[latencies.length - 1]) : null,
  }),
);
