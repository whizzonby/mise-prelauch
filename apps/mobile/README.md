# Mise customer app (Flutter)

iOS and Android app for Mise customers. It calls the Go API directly (`/api/v1`); the API is authoritative for every price, status
and rule. Architecture: `docs/mobile/MOBILE_ARCHITECTURE.md`. Plan and progress: `docs/architecture/SPRINT_10_PLAN.md`.

Toolchain: Flutter 3.38.7 stable, Dart 3.10.7 (dependency versions are pinned in `pubspec.yaml`).

## Configuration (`--dart-define`)

| Name | Default | Notes |
|---|---|---|
| `MISE_ENV` | `local` | `local`, `development`, `staging`, `production`. Staging and production require https; production refuses development hosts and never shows test payment methods. |
| `MISE_API_BASE_URL` | `http://10.0.2.2:8090/api/v1` | 10.0.2.2 is the host machine as seen from the Android emulator. |
| `MISE_SITE_URL` | `http://10.0.2.2:3100` | Public website, for terms and privacy. |
| `MISE_APP_SCHEME` | `mise-local` / `mise-staging` / `mise` | Must match the Android flavor's scheme. |
| `MISE_DEEP_LINK_HOST` | — | Approved https link domain (none yet). |
| `MISE_PUSH`, `MISE_CRASH_REPORTING` | `false` | Need a Firebase project (Blocked). |

No secrets are ever passed to the app.

## Commands

```sh
export PUB_CACHE="E:/FULL MISE DEV/.pub-cache"      # keep caches off the nearly full C: drive
flutter pub get
flutter analyze
flutter test --concurrency=1                         # one test file at a time
```

Android flavors: `dev` (`tt.mise.customer.dev`, scheme `mise-local`), `staging`, `prod`.

```sh
export GRADLE_USER_HOME="E:/FULL MISE DEV/.gradle"
flutter build apk --debug --flavor dev --dart-define=MISE_ENV=local
```

### Building on this machine

This PC powers off under heavy load. Never run `flutter doctor`. Gradle is capped in `android/gradle.properties` (1.5 GB heap,
one worker, no daemon); run one heavy job at a time and never alongside Docker builds or Playwright. iOS needs macOS and Xcode:
not possible here.
