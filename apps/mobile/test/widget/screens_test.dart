@Timeout(Duration(minutes: 2))
library;

import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mise_mobile/app/providers.dart';
import 'package:mise_mobile/app/router.dart';
import 'package:mise_mobile/features/catalogue/catalogue_api.dart';

import '../support/fixtures.dart';
import '../support/harness.dart';

/// Widget tests for every main screen, against fixture API answers. Each
/// also writes a render to test/goldens/ (run with --update-goldens):
/// real widgets and fonts, demo data — test renders, not device screenshots.
void main() {
  testWidgets('01 onboarding: three photographs, skip and sign in, no account needed', (t) async {
    final app = await makeApp(signedIn: false, onboardingSeen: false);
    await pumpRoute(t, app, '/home');
    expect(find.text('Discover Caribbean meals worth cooking'), findsOneWidget);
    expect(find.text('Skip'), findsOneWidget);
    expect(find.text('Stock photograph, not a Mise kit.'), findsWidgets);
    await snap(t, '02-onboarding');
  });

  testWidgets('02 sign in: errors never say whether the account exists', (t) async {
    final app = await makeApp(signedIn: false, extra: {
      'POST /auth/mobile/login': (_) => (401, {'error': {'code': 'invalid_credentials', 'message': 'Email or password is incorrect.'}}),
    });
    await pumpRoute(t, app, '/sign-in');
    await t.enterText(find.byType(TextFormField).at(0), 'someone@example.com');
    await t.enterText(find.byType(TextFormField).at(1), 'wrong password here');
    await t.tap(find.widgetWithText(TextButton, 'Sign in').last);
    await settle(t);
    expect(find.text('Email or password is incorrect.'), findsOneWidget);
    expect(app.container.read(authControllerProvider), isA<SignedOut>());
    await snap(t, '03-login');
  });

  testWidgets('03 registration form', (t) async {
    final app = await makeApp(signedIn: false);
    await pumpRoute(t, app, '/register');
    expect(find.text('Create your account'), findsOneWidget);
    await t.tap(find.widgetWithText(TextButton, 'Create account'));
    await settle(t);
    expect(find.text('Please accept the terms to continue.'), findsOneWidget);
    await snap(t, '04-registration');
  });

  testWidgets('04 home for a subscriber puts the next box first', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/home');
    expect(find.text('Good to see you, Kavita.'), findsOneWidget);
    expect(find.text('Your next box'), findsOneWidget);
    expect(find.textContaining('Choose by'), findsWidgets);
    await snap(t, '05-home');
  });

  testWidgets('05 home for a guest shows discovery and plans', (t) async {
    final app = await makeApp(signedIn: false);
    await pumpRoute(t, app, '/home');
    expect(find.text('See plans and prices'), findsOneWidget);
    expect(find.text('Your next box'), findsNothing);
  });

  testWidgets('06 menu: this week, then all meals with search and filters', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/menu');
    expect(find.text('Curry chicken with roti'), findsWidgets);
    await snap(t, '06-meal-catalogue');
    await t.tap(find.text('This week'));
    await settle(t);
    expect(find.text('Comfort from home'), findsOneWidget);
    await snap(t, '06b-weekly-menu');
  });

  testWidgets('07 filters run on the server', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/menu');
    await t.tap(find.byIcon(Icons.tune));
    await settle(t);
    expect(find.text('Leave out meals that contain'), findsOneWidget);
    await snap(t, '07-search-and-filters');
    await t.tap(find.widgetWithText(FilterChip, 'Gluten'));
    await t.tap(find.text('Show meals'));
    await settle(t);
    expect(app.api.calls.where((c) => c == 'GET /meals').length, greaterThanOrEqualTo(2));
  });

  testWidgets('08 meal detail shows verified facts only', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/menu/meal/curry-chicken');
    expect(find.text('Curry chicken with roti'), findsWidgets);
    await snap(t, '08-meal-details');
    await t.scrollUntilVisible(find.text('Allergens'), 300, scrollable: find.byType(Scrollable).first);
    await settle(t);
    expect(find.text('Gluten'), findsWidgets);
    await snap(t, '08b-meal-details-allergens');
  });

  testWidgets('09 plans show configured prices', (t) async {
    final app = await makeApp(signedIn: false);
    await pumpRoute(t, app, '/plans');
    expect(find.text('TTD 540.00 a week'), findsOneWidget);
    await t.tap(find.text('Classic for two'));
    await settle(t);
    await t.scrollUntilVisible(find.text('Continue with Classic for two'), 300, scrollable: find.byType(Scrollable).first);
    expect(find.text('Continue with Classic for two'), findsOneWidget);
    await snap(t, '09-subscription-plans');
  });

  testWidgets('10 weekly selection: allergy warning, allowance, unsaved state', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/box/select/$cycleId');
    expect(find.text('Contains your allergens: Gluten'), findsOneWidget);
    expect(find.text('1 of 3 chosen'), findsOneWidget);
    await t.tap(find.byIcon(Icons.add).at(1));
    await settle(t);
    expect(find.text('Changes not saved yet.'), findsOneWidget);
    await snap(t, '10-weekly-meal-selection');
  });

  testWidgets('11 my box', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/box');
    expect(find.text('Next box'), findsOneWidget);
    expect(find.text('Track delivery'), findsOneWidget);
    expect(find.text('Payments are in test mode: no real money moves.'), findsOneWidget);
    await snap(t, '11-my-box');
  });

  testWidgets('12 checkout shows the server total and test methods only outside production', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/plans/classic');
    expect(find.text('Pay TTD 540.00'), findsOneWidget);
    expect(find.text('Card (test)'), findsOneWidget);
    expect(find.text('No real money moves.', findRichText: true), findsNothing);
    await snap(t, '12-checkout');
    await t.drag(find.byType(ListView).first, const Offset(0, -600));
    await settle(t);
    await snap(t, '13-payment-selection');
  });

  testWidgets('13 payment result comes from the server', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/checkout/result/$checkoutId');
    expect(find.text('Payment received'), findsOneWidget);
    expect(find.text('Test payment — no real money moved'), findsOneWidget);
    await snap(t, '14-order-confirmation');
  });

  testWidgets('14 order history and detail', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/orders');
    expect(find.text('Current'), findsOneWidget);
    expect(find.text('Previous'), findsOneWidget);
    await snap(t, '15-order-history');
    await pumpRoute(t, app, '/orders/$orderId');
    expect(find.text('MS-10482'), findsOneWidget);
    expect(find.text('Cook'), findsWidgets);
    await snap(t, '15b-order-detail');
  });

  testWidgets('15 delivery tracking: real milestones, no live map', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/orders/$orderId/delivery');
    expect(find.text('Your box is on its way'), findsOneWidget);
    expect(find.text('2 pm – 6 pm'), findsOneWidget);
    expect(find.textContaining('There is no live map'), findsOneWidget);
    await snap(t, '16-delivery-tracking');
  });

  testWidgets('16 cooking mode with approved steps', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/orders/$orderId/cook/${meal(0)['id']}');
    expect(find.text('Check your box'), findsOneWidget);
    await t.tap(find.text('Start cooking mode'));
    await settle(t);
    expect(find.text('Step 1 of 4'), findsOneWidget);
    await snap(t, '17-recipe-cooking-mode');
    await t.tap(find.byIcon(Icons.close));
    await settle(t);
  });

  testWidgets('17 support home and a live conversation', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/support');
    expect(find.text('Talk to the Mise team'), findsOneWidget);
    expect(find.text('Missing roti skins'), findsOneWidget);
    await snap(t, '18-support-home');
    await pumpRoute(t, app, '/support/conversations/$convId');
    expect(find.text('Sorry about that! Which order is it?'), findsOneWidget);
    expect(find.text('Ria'), findsWidgets);
    await snap(t, '19-live-conversation');
  });

  testWidgets('18 help centre', (t) async {
    final app = await makeApp(signedIn: false);
    await pumpRoute(t, app, '/support/help');
    expect(find.text('When does my box arrive?'), findsOneWidget);
    await snap(t, '20-help-centre');
  });

  testWidgets('19 notification centre', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/notifications');
    expect(find.text('Your box is on its way'), findsOneWidget);
    expect(find.text('Mark all read'), findsOneWidget);
    await snap(t, '21-notifications');
  });

  testWidgets('20 account and settings', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/account');
    expect(find.text('Kavita Ramdass'), findsOneWidget);
    await snap(t, '22-customer-account');
    await pumpRoute(t, app, '/account/notifications');
    expect(find.text('Delivery updates'), findsOneWidget);
    await snap(t, '23-settings');
  });

  testWidgets('21 offline: saved copy is labelled, nothing can be confirmed', (t) async {
    final dir = Directory.systemTemp.createTempSync('mise_offline');
    final app = await makeApp(cacheDir: dir);
    // A copy saved earlier (real file I/O, so outside fake async).
    await t.runAsync(() async {
      await app.container.read(catalogueApiProvider).menus();
      await app.container.read(catalogueApiProvider).menu('2026-11-30');
      final f = File('${dir.path}/cache/public/menu_2026-11-30.json');
      f.writeAsStringSync(f.readAsStringSync().replaceFirst(RegExp(r'"at":"[^"]+"'), '"at":"2026-11-20T08:00:00.000Z"'));
    });
    app.api.offline = true;
    await pumpRoute(t, app, '/menu');
    await t.tap(find.text('This week'));
    await settle(t);
    expect(find.textContaining("You're seeing a saved copy"), findsOneWidget);
    await snap(t, '24-offline-state');
  });

  testWidgets('22 error state offers a retry and hides internals', (t) async {
    final app = await makeApp(extra: {
      'GET /me/orders': (_) => (500, {'error': {'code': 'internal_error', 'message': 'pq: relation "orders" does not exist'}}),
    });
    await pumpRoute(t, app, '/orders');
    expect(find.text('Try again'), findsOneWidget);
    expect(find.textContaining('relation'), findsNothing);
    await snap(t, '25-error-state');
  });

  testWidgets('23 guests are sent to sign in for account routes', (t) async {
    final app = await makeApp(signedIn: false);
    await pumpRoute(t, app, '/orders/$orderId');
    expect(find.text('Sign in'), findsWidgets);
    expect(app.container.read(routerProvider).routerDelegate.currentConfiguration.uri.path, '/sign-in');
  });

  testWidgets('24 sign-out clears cached personal data', (t) async {
    final dir = Directory.systemTemp.createTempSync('mise_logout');
    final app = await makeApp(cacheDir: dir, extra: {'POST /auth/mobile/logout': (_) => {'signed_out': true}});
    await pumpRoute(t, app, '/orders');
    expect(Directory('${dir.path}/cache/account').existsSync(), isTrue);
    await t.runAsync(() => app.container.read(authControllerProvider.notifier).signOut());
    await settle(t);
    expect(Directory('${dir.path}/cache/account').existsSync(), isFalse);
    expect(app.container.read(authControllerProvider), isA<SignedOut>());
  });

  testWidgets('25 large text stays usable on a small phone', (t) async {
    final app = await makeApp();
    await pumpRoute(t, app, '/box', size: const Size(360, 640), textScale: 1.6);
    expect(t.takeException(), isNull);
    await snap(t, '26-large-text-small-phone');
  });
}
