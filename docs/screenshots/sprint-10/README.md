# Sprint 10 app renders

**These are test renders, not device screenshots.** Each PNG is the real app widget tree (real screens, theme, fonts and navigation)
rendered by `flutter test` at 390×844 points (2× pixels) against fixture API answers (`apps/mobile/test/support/fixtures.dart`:
demo names and prices, stock photographs standing in for meal images). They prove the screens are implemented and how they lay out;
they are not proof of behaviour on an Android or iOS device. Device screenshots: see `docs/testing/MOBILE_TEST_RESULTS.md`.

Regenerate: `cd apps/mobile && flutter test --concurrency=1 --update-goldens test/widget/screens_test.dart`, then copy
`test/widget/goldens/*.png` here.

Not rendered: 01 native splash (a platform screen, not a Flutter widget; assets in `apps/mobile/assets/branding`).
