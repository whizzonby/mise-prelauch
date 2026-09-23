import { ImageResponse } from "next/og";

import { site } from "@/content/site";

export const alt = "Mise. Everything in its place. Caribbean meal kits, prepped and ready to cook.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

/** The share card: the wordmark and the line, on the brand's cream. Flat colour only. */
export default function OpenGraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: 72,
          backgroundColor: "#f3ecdd",
          color: "#1e3a2b",
          fontFamily: "Georgia, serif",
        }}
      >
        <div style={{ display: "flex", fontSize: 64 }}>
          {site.name}
          <span style={{ color: "#b4411c" }}>.</span>
        </div>
        <div style={{ display: "flex", flexDirection: "column" }}>
          <div style={{ fontSize: 124, lineHeight: 1, letterSpacing: -3 }}>Everything</div>
          <div style={{ fontSize: 124, lineHeight: 1, letterSpacing: -3 }}>in its place.</div>
          <div style={{ marginTop: 36, fontSize: 34, color: "#22211d", fontFamily: "Helvetica, Arial, sans-serif" }}>
            Caribbean meal kits, prepped in Trinidad &amp; Tobago and ready to cook.
          </div>
        </div>
        <div style={{ display: "flex", height: 12, width: "100%", backgroundColor: "#1e3a2b" }} />
      </div>
    ),
    size,
  );
}
