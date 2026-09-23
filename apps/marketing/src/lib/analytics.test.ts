import { captureAttribution, createTracker, readAttribution, touchFromLocation } from "@mise/analytics";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
  Object.defineProperty(navigator, "globalPrivacyControl", { value: undefined, configurable: true });
});

describe("attribution", () => {
  it("reads campaign parameters and drops the referrer's query string", () => {
    const touch = touchFromLocation(
      "https://mise.tt/?utm_source=instagram&utm_medium=social&utm_campaign=launch&other=1",
      "https://www.google.com/search?q=private+search+terms",
    );
    expect(touch).toEqual({
      landing_page: "/",
      utm_source: "instagram",
      utm_medium: "social",
      utm_campaign: "launch",
      referrer_url: "https://www.google.com/search",
    });
  });

  it("ignores a referrer from the same site", () => {
    expect(touchFromLocation("https://mise.tt/privacy", "https://mise.tt/").referrer_url).toBeUndefined();
  });

  it("keeps the first touch and updates the latest when a new source arrives", () => {
    captureAttribution("https://mise.tt/?utm_source=instagram", "");
    captureAttribution("https://mise.tt/", "");
    captureAttribution("https://mise.tt/?utm_source=newsletter", "");
    const { first, latest } = readAttribution();
    expect(first?.utm_source).toBe("instagram");
    expect(latest?.utm_source).toBe("newsletter");
  });

  it("records nothing when Global Privacy Control is on", () => {
    Object.defineProperty(navigator, "globalPrivacyControl", { value: true, configurable: true });
    captureAttribution("https://mise.tt/?utm_source=instagram", "");
    expect(readAttribution()).toEqual({ first: undefined, latest: undefined });
  });
});

describe("tracker", () => {
  it("batches events and sends them with stable ids", () => {
    vi.useFakeTimers();
    const send = vi.fn().mockResolvedValue(undefined);
    const tracker = createTracker({ send, flushIntervalMs: 1000 });

    tracker.track("page_view", { path: "/" });
    tracker.track("faq_opened", { question: "pricing" });
    expect(send).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1000);
    expect(send).toHaveBeenCalledTimes(1);
    const batch = send.mock.calls[0]![0];
    expect(batch.events).toEqual([
      { type: "page_view", metadata: { path: "/" } },
      { type: "faq_opened", metadata: { question: "pricing" } },
    ]);
    expect(batch.anonymous_id).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(batch.anonymous_id).toBe(tracker.anonymousId());
    expect(window.localStorage.getItem("mise.anonymous_id")).toBe(batch.anonymous_id);
  });

  it("is a no-op when Global Privacy Control is on", () => {
    Object.defineProperty(navigator, "globalPrivacyControl", { value: true, configurable: true });
    const send = vi.fn();
    const tracker = createTracker({ send });
    tracker.track("page_view", { path: "/" });
    tracker.flush();
    expect(tracker.enabled).toBe(false);
    expect(send).not.toHaveBeenCalled();
    expect(window.localStorage.getItem("mise.anonymous_id")).toBeNull();
  });

  it("never lets a failed send surface as an error", async () => {
    vi.useFakeTimers();
    const tracker = createTracker({ send: () => Promise.reject(new Error("offline")), flushIntervalMs: 10 });
    tracker.track("page_view", { path: "/" });
    await vi.advanceTimersByTimeAsync(20);
  });
});
