// Runs a Go command from services/api with the repository's .env loaded.
// Usage: node infrastructure/scripts/run-go.mjs api | worker | misectl <args…> | test
import { spawn } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const env = { ...process.env };

const envFile = join(root, ".env");
if (existsSync(envFile)) {
  for (const line of readFileSync(envFile, "utf8").split(/\r?\n/)) {
    const match = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*)\s*$/);
    // Real environment variables win over the file.
    if (match && !(match[1] in process.env)) env[match[1]] = match[2];
  }
} else {
  console.warn("No .env found. Copy .env.example to .env first.");
}

const [target, ...rest] = process.argv.slice(2);
const goArgs =
  target === "test"
    ? ["test", "./...", "-count=1", ...rest]
    : target
      ? ["run", `./cmd/${target}`, ...rest]
      : null;

if (!goArgs) {
  console.error("usage: run-go.mjs api | worker | misectl <args…> | test");
  process.exit(2);
}

const child = spawn("go", goArgs, { cwd: join(root, "services", "api"), env, stdio: "inherit" });
child.on("error", (err) => {
  console.error(err.code === "ENOENT" ? "Go is not installed or not on PATH." : err.message);
  process.exit(1);
});
child.on("exit", (code) => process.exit(code ?? 1));
