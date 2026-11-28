# Android release checklist (Mise app)

Nothing has been published. Publishing needs the owner's explicit approval.

| # | Item | Status |
|---|---|---|
| 1 | Google Play developer account (organisation) | **Blocked**: none known |
| 2 | Application id approved (`tt.mise.customer`; dev `.dev`, beta `.staging`) | **Blocked**: proposed, not approved |
| 3 | Upload key and Play App Signing; `android/key.properties` + keystore outside git (`.gitignore` covers `*.jks`, `*.keystore`, `key.properties`) | **Blocked**: no keystore exists |
| 4 | Version: `pubspec.yaml` `version: 0.10.0+1` (build number must rise with every upload) | Ready |
| 5 | Target SDK meets Play's current requirement (Flutter 3.38 default `targetSdk`; check at submission) | Check at submission |
| 6 | Release build: `flutter build appbundle --release --flavor prod --dart-define=MISE_ENV=production --dart-define=MISE_API_BASE_URL=https://<api>/api/v1 --dart-define=MISE_SITE_URL=https://<site> --dart-define=MISE_MEDIA_BASE_URL=https://<app>` | **Blocked**: no production domain or signing |
| 7 | Icon, adaptive icon, splash, notification icon | Proposed assets in `assets/branding`: need brand approval |
| 8 | Store listing: name, short and full description, screenshots, feature graphic (1024×500) | Draft in `docs/launch/MOBILE_BETA_RUNBOOK.md` §Store text; feature graphic not made |
| 9 | Data safety form (from actual collection; see below) | Draft below; needs legal review |
| 10 | Privacy policy URL; account deletion URL/in-app path | In-app request built; public deletion web page and approved policy: **Blocked** (legal) |
| 11 | Content rating questionnaire | To do at submission |
| 12 | Payments: physical goods outside Play Billing | Documented (PAYMENTS.md) |
| 13 | App Links verification (`assetlinks.json`) | **Blocked**: no domain |
| 14 | Firebase (push, crash reporting) | **Blocked**: no project |
| 15 | Internal testing track with reviewer test account | **Blocked**: 1, 3 |
| 16 | Pre-launch report reviewed (Play's automated devices) | After 15 |

## Data safety (draft, from what the app actually does)

| Data | Collected | Purpose | Shared |
|---|---|---|---|
| Name, email, phone | Yes (account) | Account, orders, support | No (processors only: email provider) |
| Address | Yes | Delivery | Delivery partner (when contracted) |
| Purchase history | Yes | Orders and billing | Payment provider for the payment itself |
| Payment info | No — entered on the provider's page | — | — |
| Messages (support chat) | Yes | Customer support | No |
| Device id | App installation id (random), push token when enabled | Sign-in management, notifications | Google (FCM) when push is on |
| Diagnostics | None sent today (crash reporting blocked) | — | — |
| Location, photos, contacts, microphone | Not collected | — | — |

Encrypted in transit (HTTPS). Deletion: in-app request (Account → Privacy and your data).
