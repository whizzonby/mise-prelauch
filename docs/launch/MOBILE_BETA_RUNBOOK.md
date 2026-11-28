# Mise app: beta runbook

The beta is not public. Store publication needs the owner's explicit approval.

## Prerequisites (all currently Blocked unless noted)

Staging API on https with test payment providers only; approved app ids; Google Play internal testing track; Apple TestFlight;
Firebase project; signing keys stored outside git (a password manager or CI secret store); reviewer accounts with test data only.

## Builds

```sh
# Android internal testing (staging, test payments clearly labelled)
flutter build appbundle --release --flavor staging \
  --dart-define=MISE_ENV=staging --dart-define=MISE_API_BASE_URL=https://<staging-api>/api/v1 \
  --dart-define=MISE_SITE_URL=https://<staging-site> --dart-define=MISE_MEDIA_BASE_URL=https://<staging-app> \
  --dart-define=MISE_APP_SCHEME=mise-staging
# iOS TestFlight (on a Mac): flutter build ipa --release with the staging configuration
```

The staging API must set `MOBILE_PAYMENT_RETURN_URL=mise-staging://checkout/return`. Staging build is named "Mise Beta" and installs
beside the store app.

## Testers and data

Invite testers by email in Play Console and TestFlight. Testers use their own accounts on staging; never production data, never demo
operational data from other customers. Payments are in test mode ("Test" labels everywhere; no real money moves).

## Feedback to collect

Onboarding clarity, meal discovery, plan choice, checkout and the return from the payment page, weekly selection, delivery tracking,
support messaging, performance on their phone, anything confusing. Collect through Play/TestFlight feedback and the in-app support chat
(tag conversations "beta").

## Store text (draft)

- **Name:** Mise — Caribbean meal kits
- **Short description:** Weekly Caribbean meal kits for Trinidad and Tobago. Choose, cook, enjoy.
- **Full description:** Choose a plan, pick your meals from each week's menu, and cook from measured ingredients with clear, approved
  steps. Track your box, manage your subscription and talk to the Mise team — all in one place. (To be approved by the owner; no claims
  about prices, delivery areas or nutrition until they are confirmed.)
- **Keywords (iOS):** meal kit, Caribbean, Trinidad, recipes, cooking

## Review instructions (draft)

"Sign in with the reviewer account below. Payments run in test mode with the Mise test provider; use the 'Approve' button on its page.
Physical goods: payments are taken outside in-app purchase. Account deletion: Account → Privacy and your data."
