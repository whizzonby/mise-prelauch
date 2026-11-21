import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_web_auth_2/flutter_web_auth_2.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../shared/models/commerce.dart';

/// Hosted checkout for the app (brief §22–28):
///
/// 1. The server creates the checkout and decides the amount and the return URL.
/// 2. The provider's own page opens in the system browser session
///    (ASWebAuthenticationSession / Custom Tabs) — never a WebView, so the
///    app cannot see card fields.
/// 3. Whatever the browser returns (or if the customer closes it), the app
///    asks the server for the outcome. The return is never proof of payment.
class PaymentFlow {
  PaymentFlow(this.ref);

  final Ref ref;

  ApiClient get _api => ref.read(apiClientProvider);

  /// Only the options the server offers; never test methods in a production build.
  Future<List<PaymentOption>> options(String currency) async {
    final env = ref.read(environmentProvider);
    final all = await _api.get('/payment-options', (d) => itemsOf(d, PaymentOption.fromJson), query: {'currency': currency}, options: publicOptions);
    return all.where((o) => env.allowsTestPayments || !o.test).toList();
  }

  /// Starts a subscription with its first payment.
  Future<(String subscriptionId, Checkout? checkout, bool activated)> subscribe({
    required String planId,
    required String addressId,
    required String? provider,
    String? promotionCode,
    required String idempotencyKey,
  }) => _api.post(
    '/me/subscriptions',
    (d) {
      final j = asMap(d);
      return (str(j, 'subscription_id'), j['checkout'] is Map ? Checkout.fromJson(asMap(j['checkout'])) : null, boolean(j, 'activated'));
    },
    body: {'plan_id': planId, 'address_id': addressId, 'provider': ?provider, 'promotion_code': ?promotionCode, 'client': 'app'},
    idempotencyKey: idempotencyKey,
  );

  /// Pays one week that is due (or failed).
  Future<Checkout> payWeek({required String billingCycleId, required String? provider, required String idempotencyKey}) => _api.post(
    '/checkout/sessions',
    (d) => Checkout.fromJson(asMap(d)),
    body: {'billing_cycle_id': billingCycleId, 'provider': ?provider, 'client': 'app'},
    idempotencyKey: idempotencyKey,
  );

  /// Opens the provider's page and waits for the customer to come back or
  /// close it. Returns normally in both cases.
  Future<void> authorize(Checkout c) async {
    final url = c.redirectUrl;
    if (url == null) return;
    final uri = Uri.tryParse(url);
    final env = ref.read(environmentProvider);
    // Only web pages: http only for the local test provider.
    if (uri == null || !(uri.scheme == 'https' || (uri.scheme == 'http' && !env.isProduction))) {
      throw const FormatException('unexpected checkout address');
    }
    try {
      await FlutterWebAuth2.authenticate(
        url: url,
        callbackUrlScheme: env.appScheme,
        options: const FlutterWebAuth2Options(preferEphemeral: false),
      );
    } on PlatformException {
      // Cancelled or closed: the server still decides what happened.
    }
  }

  Future<CheckoutResult> result(String checkoutId) =>
      _api.get('/checkout/sessions/$checkoutId', (d) => CheckoutResult.fromJson(asMap(d)));
}

final paymentFlowProvider = Provider<PaymentFlow>(PaymentFlow.new);

/// What to tell the customer for each server outcome.
({String title, String body}) outcomeCopy(PaymentOutcome o, {required bool timedOut}) => switch (o) {
  PaymentOutcome.succeeded => (title: 'Payment received', body: 'Thank you. Your box is booked.'),
  PaymentOutcome.failed => (title: 'Payment did not go through', body: 'Nothing was charged. You can try again, with the same or another method.'),
  PaymentOutcome.expired => (title: 'Checkout expired', body: 'The payment page timed out before it was completed. Nothing was charged.'),
  PaymentOutcome.pending when timedOut => (
    title: 'We are still confirming your payment',
    body: 'This can take a few minutes. Do not pay again: we will update your subscription as soon as the provider confirms.',
  ),
  PaymentOutcome.pending => (title: 'Confirming your payment…', body: 'Checking with the payment provider. Please keep the app open.'),
};

/// Polls the server every [interval] for up to [limit]. Stops at the first
/// final outcome. Never moves money; read-only.
Stream<CheckoutResult> pollOutcome(
  Future<CheckoutResult> Function() fetch, {
  Duration interval = const Duration(seconds: 2),
  Duration limit = const Duration(minutes: 2),
}) async* {
  final end = DateTime.now().add(limit);
  while (true) {
    CheckoutResult? r;
    try {
      r = await fetch();
    } catch (_) {
      // Offline for a moment: keep trying until the limit.
    }
    if (r != null) {
      yield r;
      if (r.outcome != PaymentOutcome.pending) return;
    }
    if (DateTime.now().isAfter(end)) return;
    await Future<void>.delayed(interval);
  }
}
