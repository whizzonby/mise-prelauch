import type { Metadata } from "next";
import { Instrument_Sans, Young_Serif } from "next/font/google";
import type { ReactNode } from "react";

import "./globals.css";

const display = Young_Serif({ subsets: ["latin"], weight: "400", variable: "--font-young-serif", display: "swap" });
const sans = Instrument_Sans({ subsets: ["latin"], variable: "--font-instrument-sans", display: "swap" });

export const metadata: Metadata = {
  metadataBase: new URL(process.env.CUSTOMER_APP_URL ?? "http://localhost:3102"),
  title: { default: "Your Mise account", template: "%s | Mise" },
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" data-scroll-behavior="smooth" className={`${display.variable} ${sans.variable}`}>
      <body>
        <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:bg-surface focus:p-3">
          Skip to content
        </a>
        {children}
      </body>
    </html>
  );
}
