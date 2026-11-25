import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { PageHeader } from "@/components/admin-ui";
import { Labelled, Panel, TextArea, TextInput } from "@/components/commerce-ui";
import { KForm } from "@/components/kitchen-form";
import { api, can, requireAdmin } from "@/lib/session";

import { saveChatSettings, saveSavedReply } from "./actions";

export const metadata: Metadata = { title: "Chat settings" };

const DAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

/** Chat availability (business hours, closed days, messages) and saved replies. */
export default async function ChatSettingsPage() {
  const { token, admin } = await requireAdmin();
  if (!can(admin, "support_chat.manage")) notFound();
  const [st, replies] = await Promise.all([api.admin.chatSettings(token), api.admin.savedReplies(token, true)]);
  const hoursFor = (d: number) => st.business_hours.find((h) => h.day === d);

  return (
    <>
      <PageHeader
        title="Chat settings"
        back={{ href: "/support/chat", label: "Support chat" }}
        description="Nothing here is filled in for you. With no hours set, the widget never says the team is online and promises no reply time beyond the expectation text."
      />
      <div className="grid gap-8">
        <Panel title="Availability">
          <KForm action={saveChatSettings} submit="Save settings" pending="Saving…" label="Chat availability">
            <label className="type-body-sm flex items-center gap-3 font-semibold">
              <input type="checkbox" name="enabled" defaultChecked={st.enabled} className="size-5" />
              Chat is on (customers and guests can start conversations)
            </label>
            <div className="grid gap-4 md:grid-cols-2">
              <Labelled label="Time zone" hint="For example America/Port_of_Spain.">
                <TextInput name="timezone" defaultValue={st.timezone} required />
              </Labelled>
              <Labelled label="Reply email cool-down (minutes)" hint="At most one 'you have a reply' email per conversation in this window.">
                <TextInput name="email_cooldown_minutes" type="number" min={5} max={1440} defaultValue={st.email_cooldown_minutes} />
              </Labelled>
            </div>
            <fieldset>
              <legend className="type-body-sm font-semibold">Business hours (leave a day empty when closed)</legend>
              <div className="mt-2 grid gap-2">
                {DAY_NAMES.map((name, d) => (
                  <div key={d} className="grid grid-cols-[7rem_auto_auto] items-center justify-start gap-x-6 gap-y-2">
                    <span className="type-body-sm">{name}</span>
                    <label className="type-caption flex items-center gap-2">
                      Opens
                      <input type="time" name={`open_${d}`} defaultValue={hoursFor(d)?.open ?? ""} className="min-h-10 rounded-md border border-border bg-background px-2" />
                    </label>
                    <label className="type-caption flex items-center gap-2">
                      Closes
                      <input type="time" name={`close_${d}`} defaultValue={hoursFor(d)?.close ?? ""} className="min-h-10 rounded-md border border-border bg-background px-2" />
                    </label>
                  </div>
                ))}
              </div>
            </fieldset>
            <Labelled label="Closed days" hint="One per line: 2026-12-25 Christmas Day">
              <TextArea name="holidays" rows={4} defaultValue={st.holidays.map((h) => `${h.day} ${h.name}`).join("\n")} />
            </Labelled>
            <div className="grid gap-4 md:grid-cols-2">
              <Labelled label="Message while open" hint="Optional.">
                <TextInput name="online_message" maxLength={500} defaultValue={st.online_message} />
              </Labelled>
              <Labelled label="Message while closed" hint="Optional.">
                <TextInput name="offline_message" maxLength={500} defaultValue={st.offline_message} />
              </Labelled>
            </div>
            <Labelled label="Response expectation" hint="Only what the team has agreed to. Empty shows: We'll reply as soon as we can.">
              <TextInput name="response_expectation" maxLength={300} defaultValue={st.response_expectation} />
            </Labelled>
          </KForm>
        </Panel>

        <Panel title="Saved replies" note="Agents can insert these into a reply and edit them before sending.">
          <div className="grid gap-6">
            {replies.items.map((r) => (
              <KForm key={r.id} action={saveSavedReply} submit="Save" pending="Saving…" label={`Saved reply ${r.title}`} variant="secondary">
                <input type="hidden" name="id" value={r.id} />
                <Labelled label="Title">
                  <TextInput name="title" defaultValue={r.title} maxLength={100} required />
                </Labelled>
                <Labelled label="Text">
                  <TextArea name="body" defaultValue={r.body} rows={3} maxLength={4000} required />
                </Labelled>
                <label className="type-body-sm flex items-center gap-2">
                  <input type="checkbox" name="archived" defaultChecked={r.archived} /> Archived (hidden from agents)
                </label>
              </KForm>
            ))}
            <KForm action={saveSavedReply} submit="Add saved reply" pending="Adding…" label="New saved reply" reset>
              <Labelled label="Title">
                <TextInput name="title" maxLength={100} required />
              </Labelled>
              <Labelled label="Text">
                <TextArea name="body" rows={3} maxLength={4000} required />
              </Labelled>
            </KForm>
          </div>
        </Panel>
      </div>
    </>
  );
}
