/** Mise's market. Times shown to customers are in Trinidad and Tobago time. */
export const BUSINESS_TIME_ZONE = "America/Port_of_Spain";

/** The hour of the day (0 to 23) in Trinidad and Tobago. */
export function localHour(now: Date, timeZone = BUSINESS_TIME_ZONE): number {
  const hour = new Intl.DateTimeFormat("en-GB", { hour: "numeric", hourCycle: "h23", timeZone }).format(now);
  return Number(hour);
}

/** "Good morning" until noon, "Good afternoon" until six, then "Good evening". */
export function greeting(now: Date, timeZone = BUSINESS_TIME_ZONE): string {
  const hour = localHour(now, timeZone);
  if (hour >= 5 && hour < 12) return "Good morning";
  if (hour >= 12 && hour < 18) return "Good afternoon";
  return "Good evening";
}

/** "Wednesday 11 November", in Trinidad and Tobago. */
export function longDate(now: Date, timeZone = BUSINESS_TIME_ZONE): string {
  return new Intl.DateTimeFormat("en-TT", { weekday: "long", day: "numeric", month: "long", timeZone }).format(now);
}

/** "11 Nov 2026, 4:12 pm" for saved-at and last-used times. */
export function shortDateTime(iso: string, timeZone = BUSINESS_TIME_ZONE): string {
  return new Intl.DateTimeFormat("en-TT", { dateStyle: "medium", timeStyle: "short", timeZone }).format(new Date(iso));
}
