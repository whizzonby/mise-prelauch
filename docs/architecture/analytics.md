# Analytics and attribution

First-party only. Events go from the browser to the Mise API and are stored in
`lead_events`. There are no third-party scripts, no cookies and no advertising identifiers.

## Events

The taxonomy is defined once in `packages/analytics/src/index.ts` (with typed metadata per
event) and mirrored by the allow-list in `services/api/internal/events/events.go`. A test
fails if the two lists differ. The API rejects any event type not listed here.

| Event | When | Metadata |
|---|---|---|
| `page_view` | A page is shown. | `path` (no query string) |
| `section_viewed` | A home-page section is 30% on screen, once per page view. | `section` |
| `hero_cta_clicked` | A hero button is pressed. | `cta`: `primary` or `secondary` |
| `waitlist_started` | First focus in a waitlist form. | `placement` |
| `waitlist_completed` | The API accepted a signup. | `placement`, `outcome`, `referred` |
| `preferences_started` | First focus in the profiling form. | none |
| `preferences_completed` | Profiling answers saved. | `answered` (a count) |
| `referral_link_copied` | "Copy link" pressed. | none |
| `referral_shared` | A share button pressed. | `channel` |
| `faq_opened` | A question opened. | `question` (its id) |
| `meal_viewed` | A meal is 60% on screen, once. | `meal`, `category` |
| `plan_viewed` | A plan row is 80% on screen, once. | `plan` |

Adding an event means adding it to both lists and to this table.

## Rules

- **Identifiers only.** Metadata is a flat object of at most 8 short values. Form values
  (names, emails, phone numbers, answers) never go into an event; the types make that hard
  to do by accident and the API enforces the shape.
- **Respecting the visitor.** If the browser sends Global Privacy Control or Do Not Track,
  the tracker is a no-op: nothing is stored in the browser and nothing is sent.
- **Identity.** A random `anonymous_id` (local storage) and `session_id` (session storage).
  At signup the `anonymous_id` is sent with the form and the visitor's earlier events are
  attached to the new lead. After that, events carry the profile token and attach directly.
- **Never in the way.** Events are batched (every 4 seconds, on tab hide, or at 20 events)
  and sent with `keepalive`. A failed send is dropped silently.

## Attribution

Captured in the browser (`captureAttribution`) and sent with the signup:

- **First touch**: written the first time someone lands, never changed. UTM parameters,
  landing path, and the referring site without its query string.
- **Latest touch**: replaced whenever a later visit arrives with a source (a campaign link
  or an external referrer).
- **Invitation code**: kept from a `/join/CODE` visit so it still counts if the visitor
  browses and signs up from the home page. It is kept even when tracking is declined,
  because following a friend's link is a request for it to count.

Stored in `lead_attribution` (one `first` and one `latest` row per lead) and shown on the
lead detail page. The dashboard's "top acquisition sources" uses first touch: `utm_source`
if present, otherwise `referral` for invited leads, otherwise `direct`.

## Conversion questions the data can answer

- Signup rate by source: `page_view` sessions per `utm_source` against leads per first-touch source.
- Form drop-off: `waitlist_started` against `waitlist_completed`, by `placement`.
- Verification rate: leads `PENDING` against `VERIFIED`, by source.
- Referral loop: `referral_link_copied` / `referral_shared` against converted referrals.
- Content interest: `meal_viewed`, `plan_viewed`, `faq_opened` counts.

The admin dashboard shows the lead-level breakdowns today. Event-level funnels are a query
away and are a natural next addition to the dashboard.
