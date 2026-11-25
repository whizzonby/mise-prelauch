import type { HelpStaffArticle } from "@mise/api-client";

import { Labelled, SelectInput, TextArea, TextInput } from "@/components/commerce-ui";

export const CATEGORIES = [
  ["getting-started", "Getting started"],
  ["subscriptions-and-billing", "Subscriptions and billing"],
  ["meals-and-menus", "Meals and menus"],
  ["delivery", "Delivery"],
  ["account-and-privacy", "Account and privacy"],
] as const;

/** The editable fields of a help article (plain text only). */
export function ArticleFields({ a, disabled = false }: { a?: HelpStaffArticle; disabled?: boolean }) {
  return (
    <fieldset disabled={disabled} className="grid gap-4">
      <div className="grid gap-4 md:grid-cols-2">
        <Labelled label="Title">
          <TextInput name="title" defaultValue={a?.title} minLength={3} maxLength={160} required />
        </Labelled>
        <Labelled label="Address" hint="Lowercase words joined by hyphens, for example: pausing-your-plan">
          <TextInput name="slug" defaultValue={a?.slug} maxLength={100} pattern="[a-z0-9]+(-[a-z0-9]+)*" required />
        </Labelled>
      </div>
      <Labelled label="Category">
        <SelectInput name="category" defaultValue={a?.category ?? "getting-started"}>
          {CATEGORIES.map(([slug, name]) => (
            <option key={slug} value={slug}>
              {name}
            </option>
          ))}
        </SelectInput>
      </Labelled>
      <Labelled label="Summary" hint="One sentence shown in search results.">
        <TextInput name="summary" defaultValue={a?.summary} maxLength={300} />
      </Labelled>
      <Labelled label="Article" hint="Plain text. Leave a blank line between paragraphs. No HTML.">
        <TextArea name="body" defaultValue={a?.body} rows={14} maxLength={20000} />
      </Labelled>
      <Labelled label="Note for the history (optional)">
        <TextInput name="note" maxLength={1000} />
      </Labelled>
    </fieldset>
  );
}
