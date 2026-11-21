import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../core/errors/app_error.dart';
import '../../core/storage/storage.dart';
import '../../shared/models/orders.dart';

/// Orders, delivery and recipes. Account data: cached only in the account
/// area, which is wiped at sign-out.
class OrdersApi {
  OrdersApi(this.api, this.cache);

  final ApiClient api;
  final CacheStore cache;

  Future<Cached<T>> _cached<T>(String key, Future<Object?> Function() fetch, T Function(Object?) parse) async {
    try {
      final data = await fetch();
      final v = parse(data);
      await cache.put(cacheAccount, key, data);
      return Cached(v, DateTime.now().toUtc());
    } on AppError catch (e) {
      if (!e.isNetwork) rethrow;
      final hit = await cache.get(cacheAccount, key);
      if (hit == null) rethrow;
      return Cached(parse(hit.value), hit.fetchedAt);
    }
  }

  Future<Cached<List<OrderSummary>>> orders({int page = 1}) =>
      _cached('orders_p$page', () => api.get('/me/orders', (d) => d, query: {'page': page, 'page_size': 25}), (d) => itemsOf(d, OrderSummary.fromJson));

  Future<Cached<OrderDetail>> order(String id) => _cached('order_$id', () => api.get('/me/orders/$id', (d) => d), (d) => OrderDetail.fromJson(asMap(d)));

  Future<DeliveryStatus> delivery(String orderId) => api.get('/me/orders/$orderId/delivery', (d) => DeliveryStatus.fromJson(asMap(d)));

  /// Approved recipes are kept for cooking offline.
  Future<Cached<CustomerRecipe>> recipe(String orderId, String mealId) => _cached(
    'recipe_${orderId}_$mealId',
    () => api.get('/me/orders/$orderId/meals/$mealId/recipe', (d) => d),
    (d) => CustomerRecipe.fromJson(asMap(d)),
  );
}

final ordersApiProvider = Provider<OrdersApi>((ref) => OrdersApi(ref.watch(apiClientProvider), ref.watch(cacheStoreProvider)));

final ordersProvider = FutureProvider<Cached<List<OrderSummary>>>((ref) => ref.watch(ordersApiProvider).orders());
final orderProvider = FutureProvider.family<Cached<OrderDetail>, String>((ref, id) => ref.watch(ordersApiProvider).order(id));
final deliveryProvider = FutureProvider.family<DeliveryStatus, String>((ref, id) => ref.watch(ordersApiProvider).delivery(id));
final recipeProvider = FutureProvider.family<Cached<CustomerRecipe>, (String, String)>((ref, k) => ref.watch(ordersApiProvider).recipe(k.$1, k.$2));
