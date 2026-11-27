/**
 * Compile-time check that the hand-written response types in index.ts agree
 * with the OpenAPI contract (schema.d.ts, generated). Nothing here runs: if a
 * field is renamed, retyped or dropped on either side, `tsc` fails here.
 *
 * Each pair is checked in both directions where the shapes should be equal.
 */
import type { components } from "./schema";
import type { CreateLeadInput, LeadDetail, LeadPage, LeadSummary, OpBody, OpData, Overview, PreferencesInput } from "./index";

type S = components["schemas"];

type Assignable<From, To> = From extends To ? true : false;
type Both<A, B> = [Assignable<A, B>, Assignable<B, A>] extends [true, true] ? true : false;
const ok = <T extends true>(): T => true as T;

type Real<T> = [T] extends [never] ? false : true;

export const contract = [
  // Sprint 9.2 operation types resolve (a typo or missing response would make them never).
  ok<Real<OpData<"financeSummary">>>(),
  ok<Both<OpData<"financeSummary">, S["FinanceSummary"]>>(),
  ok<Real<OpData<"intelligenceAsk">>>(),
  ok<Real<OpBody<"intelligenceAsk">>>(),
  ok<Real<OpData<"scenarioCreate">>>(),
  ok<Real<OpData<"intelligenceGenerateBriefing">>>(),
  ok<Real<OpBody<"financeCreateExpense">>>(),
  ok<Both<LeadSummary, S["LeadSummary"]>>(),
  ok<Both<Overview, S["Overview"]>>(),
  ok<Assignable<LeadPage, S["LeadPage"]>>(),
  ok<Assignable<LeadDetail, S["LeadDetail"]>>(),
  ok<Assignable<CreateLeadInput, S["CreateLeadInput"]>>(),
  ok<Assignable<PreferencesInput, S["PreferencesInput"]>>(),
];
