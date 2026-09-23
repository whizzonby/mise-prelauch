// Shared ESLint config for the Next.js apps and TypeScript packages.
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypescript from "eslint-config-next/typescript";

/** @type {import("eslint").Linter.Config[]} */
const config = [
  ...nextVitals,
  ...nextTypescript,
  {
    // Stated rather than detected: not every package depends on React.
    settings: { react: { version: "19" } },
    rules: {
      // The apps live in a monorepo; there is no single `pages` directory to resolve.
      "@next/next/no-html-link-for-pages": "off",
    },
  },
  { ignores: [".next/**", "out/**", "next-env.d.ts", "coverage/**"] },
];

export default config;
