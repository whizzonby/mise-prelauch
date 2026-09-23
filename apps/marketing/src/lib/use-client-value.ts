import { useSyncExternalStore } from "react";

const subscribe = () => () => {};

/**
 * Reads a value that only exists in the browser (the address bar, storage,
 * navigator). Returns undefined during server rendering and hydration, then
 * the real value, without a hydration mismatch. `read` must return a
 * primitive, or the same object each time.
 */
export function useClientValue<T>(read: () => T): T | undefined {
  return useSyncExternalStore<T | undefined>(subscribe, read, () => undefined);
}
