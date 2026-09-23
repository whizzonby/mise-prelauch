import type { Metadata } from "next";
import { Instrument_Sans, Young_Serif } from "next/font/google";
import type { ReactNode } from "react";

import "./globals.css";

const display = Young_Serif({ subsets: ["latin"], weight: "400", variable: "--font-young-serif", display: "swap" });
const sans = Instrument_Sans({ subsets: ["latin"], variable: "--font-instrument-sans", display: "swap" });

export const metadata: Metadata = {
  title: { default: "Mise admin", template: "%s | Mise admin" },
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" data-scroll-behavior="smooth" className={`${display.variable} ${sans.variable}`}>
      <body>{children}</body>
    </html>
  );
}
