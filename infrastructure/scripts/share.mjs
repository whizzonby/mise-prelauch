// Shares the local marketing site through a temporary Cloudflare quick tunnel.
//   node infrastructure/scripts/share.mjs start | stop | url
// See docker-compose.share.yml for what is and is not exposed.
import { execFileSync, spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const files = ["-f", "docker-compose.yml", "-f", "docker-compose.share.yml"];
const PLACEHOLDER = "http://localhost:3100";

function compose(args, { env = {}, quiet = false, files: f = files } = {}) {
  const result = spawnSync("docker", ["compose", ...f, ...args], {
    cwd: root,
    env: { ...process.env, ...env },
    stdio: quiet ? ["ignore", "pipe", "pipe"] : "inherit",
    encoding: "utf8",
  });
  if (result.status !== 0 && !quiet) process.exit(result.status ?? 1);
  return (result.stdout ?? "") + (result.stderr ?? "");
}

/** Reads the public address out of the tunnel container's log. */
function tunnelUrl() {
  const logs = compose(["logs", "--no-color", "tunnel"], { env: { SHARE_URL: PLACEHOLDER }, quiet: true });
  const matches = logs.match(/https:\/\/[a-z0-9-]+\.trycloudflare\.com/g);
  return matches ? matches[matches.length - 1] : null;
}

const sleep = (ms) => execFileSync(process.execPath, ["-e", `setTimeout(() => {}, ${ms})`]);

const command = process.argv[2];

if (command === "start") {
  // The tunnel starts first: its address is random, and the site and API have
  // to be told what it is.
  compose(["up", "-d", "tunnel"], { env: { SHARE_URL: PLACEHOLDER } });
  let url = null;
  for (let i = 0; i < 40 && !url; i++) {
    sleep(1500);
    url = tunnelUrl();
  }
  if (!url) {
    console.error("The tunnel did not report an address. Check: docker compose logs tunnel");
    process.exit(1);
  }
  console.log(`\nTunnel address: ${url}\nRebuilding the site for that address…\n`);
  compose(["up", "-d", "--build", "--wait", "api", "worker", "marketing"], { env: { SHARE_URL: url } });
  console.log(`\nShared at: ${url}\nStop sharing with: pnpm share:stop\n`);
} else if (command === "url") {
  console.log(tunnelUrl() ?? "Not sharing.");
} else if (command === "stop") {
  compose(["rm", "--stop", "--force", "tunnel"], { env: { SHARE_URL: PLACEHOLDER } });
  console.log("\nTunnel stopped. Restoring the normal local setup…\n");
  compose(["up", "-d", "--build", "--wait", "api", "worker", "marketing"], { files: ["-f", "docker-compose.yml"] });
  console.log("\nBack to local only: http://localhost:3100\n");
} else {
  console.error("usage: share.mjs start | stop | url");
  process.exit(2);
}
