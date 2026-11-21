import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../shared/models/commerce.dart';

/// Subscriptions, weeks, selections and checkout. The API validates and
/// prices everything; these calls only carry the customer's choices.
class CommerceApi {
  CommerceApi(this.api);

  final ApiClient api;

  Future<List<Plan>> plans() => api.get('/plans', (d) => itemsOf(d, Plan.fromJson), options: publicOptions);

  Future<Plan> plan(String slug) => api.get('/plans/$slug', (d) => Plan.fromJson(asMap(d)), options: publicOptions);

  Future<Quote> quote(String planId, {String? promotionCode}) =>
      api.post('/me/subscriptions/quote', (d) => Quote.fromJson(asMap(d)), body: {'plan_id': planId, 'promotion_code': ?promotionCode});

  Future<Subscription?> mySubscription() => api.get('/me/subscription', (d) {
    final s = asMap(d)['subscription'];
    return s is Map ? Subscription.fromJson(asMap(s)) : null;
  });

  Future<List<Cycle>> weeks() => api.get('/me/subscription-cycles', (d) => itemsOf(d, Cycle.fromJson));

  Future<SelectionPage> selection(String cycleId) => api.get('/me/meal-selections/$cycleId', (d) => SelectionPage.fromJson(asMap(d)));

  /// Saves a draft. 409 `version_conflict` when another device saved first;
  /// 409 `selection_closed` after the cutoff.
  Future<SelectionPage> saveSelection(String cycleId, Map<String, int> items, int? version) => api.put(
    '/me/meal-selections/$cycleId',
    (d) => SelectionPage.fromJson(asMap(d)),
    body: {
      'items': [
        for (final e in items.entries)
          if (e.value > 0) {'meal_id': e.key, 'quantity': e.value},
      ],
      'version': version,
    },
  );

  Future<SelectionPage> finalizeSelection(String cycleId, Map<String, int> items, int? version) => api.post(
    '/me/meal-selections/$cycleId/finalize',
    (d) => SelectionPage.fromJson(asMap(d)),
    body: {
      'items': [
        for (final e in items.entries)
          if (e.value > 0) {'meal_id': e.key, 'quantity': e.value},
      ],
      'version': version,
    },
  );

  Future<List<PaymentOption>> paymentOptions(String currency) =>
      api.get('/payment-options', (d) => itemsOf(d, PaymentOption.fromJson), query: {'currency': currency}, options: publicOptions);

  Future<void> pause(String id, int weeks) => api.post<void>('/me/subscriptions/$id/pause', (_) {}, body: {'weeks': weeks});
  Future<void> resume(String id) => api.post<void>('/me/subscriptions/$id/resume', (_) {});
  Future<void> cancel(String id, String reason) => api.post<void>('/me/subscriptions/$id/cancel', (_) {}, body: {'reason': reason});
  Future<void> changePlan(String id, String planId) => api.post<void>('/me/subscriptions/$id/change-plan', (_) {}, body: {'plan_id': planId});
  Future<void> changeAddress(String id, String addressId) =>
      api.post<void>('/me/subscriptions/$id/address', (_) {}, body: {'address_id': addressId});

  Future<List<Address>> addresses() => api.get('/me/addresses', (d) => itemsOf(d, Address.fromJson));
}

final commerceApiProvider = Provider<CommerceApi>((ref) => CommerceApi(ref.watch(apiClientProvider)));

final plansProvider = FutureProvider<List<Plan>>((ref) => ref.watch(commerceApiProvider).plans());

/// The signed-in customer's subscription, or null (none, or a guest).
final mySubscriptionProvider = FutureProvider<Subscription?>((ref) async {
  if (!ref.watch(isSignedInProvider)) return null;
  return ref.watch(commerceApiProvider).mySubscription();
});

final weeksProvider = FutureProvider<List<Cycle>>((ref) async {
  if (!ref.watch(isSignedInProvider)) return const [];
  return ref.watch(commerceApiProvider).weeks();
});

final addressesProvider = FutureProvider<List<Address>>((ref) async {
  if (!ref.watch(isSignedInProvider)) return const [];
  return ref.watch(commerceApiProvider).addresses();
});

final subscriptionInfoProvider = FutureProvider<Json>((ref) => ref.watch(apiClientProvider).get('/subscription-info', asMap, options: publicOptions));
