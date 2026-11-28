# iOS release checklist (Mise app)

**iOS cannot be built, signed or run on the development machine (Windows; no macOS or Xcode).** Nothing below has been verified on
iOS. Android results do not count as iOS verification.

| # | Item | Status |
|---|---|---|
| 1 | Apple Developer Program membership (organisation) and App Store Connect app record | **Blocked**: none known |
| 2 | Bundle id approved (`tt.mise.customer`, set in `ios/Runner.xcodeproj`); dev/staging bundle ids and URL schemes per build configuration | **Blocked**: proposed; configurations must be added on a Mac |
| 3 | Signing: team, certificates, provisioning profiles (never committed) | **Blocked** |
| 4 | macOS build machine or CI runner with Xcode | **Blocked** |
| 5 | `pod install`, build, run on simulator and an iPhone | **Blocked** (4) |
| 6 | Info.plist: display name "Mise", URL scheme, `FlutterDeepLinkingEnabled=false`, `ITSAppUsesNonExemptEncryption=false`, remote-notification background mode | Done in the project; unverified |
| 7 | Push: APNs key in Firebase, Push Notifications capability, `GoogleService-Info.plist` | **Blocked** (no Firebase project, no Apple account) |
| 8 | Associated Domains + `apple-app-site-association` | **Blocked** (no domain) |
| 9 | Privacy manifest (`PrivacyInfo.xcprivacy`) for the app and check of each SDK's manifest | To do on a Mac; required-reason APIs used by plugins (UserDefaults via shared_preferences, file timestamps) must be declared |
| 10 | App Privacy "nutrition label" (same facts as the Android data safety draft) | Draft; legal review |
| 11 | Account deletion inside the app (Guideline 5.1.1(v)) | In-app request built; completing deletion needs the approved retention policy (**Blocked**, legal) |
| 12 | Physical goods paid outside IAP (Guideline 3.1.3(e)) | Documented |
| 13 | Screenshots (6.9" and 6.5" iPhone) | **Blocked** (no simulator); test renders exist for review only |
| 14 | TestFlight internal build, review notes and reviewer account | **Blocked** (1–5) |
