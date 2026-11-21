import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';
import '../../core/errors/app_error.dart';
import '../../core/storage/storage.dart';
import '../../shared/models/catalogue.dart';

/// Filters for `GET /meals`. Every filter is applied by the server.
class MealQuery {
  const MealQuery({this.q = '', this.category, this.diet = const {}, this.excludeAllergens = const {}, this.maxMinutes, this.sort = 'newest'});

  final String q;
  final String? category;
  final Set<String> diet;
  final Set<String> excludeAllergens;
  final int? maxMinutes;
  final String sort;

  bool get isEmpty => q.isEmpty && category == null && diet.isEmpty && excludeAllergens.isEmpty && maxMinutes == null;

  MealQuery copyWith({String? q, Object? category = _keep, Set<String>? diet, Set<String>? excludeAllergens, Object? maxMinutes = _keep, String? sort}) =>
      MealQuery(
        q: q ?? this.q,
        category: identical(category, _keep) ? this.category : category as String?,
        diet: diet ?? this.diet,
        excludeAllergens: excludeAllergens ?? this.excludeAllergens,
        maxMinutes: identical(maxMinutes, _keep) ? this.maxMinutes : maxMinutes as int?,
        sort: sort ?? this.sort,
      );

  Map<String, dynamic> params(int page, int pageSize) => {
    'q': q.trim(),
    'category': category,
    'diet': diet.isEmpty ? null : (diet.toList()..sort()).join(','),
    'exclude_allergens': excludeAllergens.isEmpty ? null : (excludeAllergens.toList()..sort()).join(','),
    'max_minutes': maxMinutes,
    'sort': sort,
    'page': page,
    'page_size': pageSize,
  };

  @override
  bool operator ==(Object other) =>
      other is MealQuery &&
      other.q == q &&
      other.category == category &&
      other.maxMinutes == maxMinutes &&
      other.sort == sort &&
      other.diet.length == diet.length &&
      other.diet.containsAll(diet) &&
      other.excludeAllergens.length == excludeAllergens.length &&
      other.excludeAllergens.containsAll(excludeAllergens);

  @override
  int get hashCode => Object.hash(q, category, maxMinutes, sort, Object.hashAllUnordered(diet), Object.hashAllUnordered(excludeAllergens));
}

const _keep = Object();

/// Public catalogue endpoints. Results are cached for offline reading and
/// served from the cache (marked stale) when the network fails.
class CatalogueApi {
  CatalogueApi(this.api, this.cache);

  final ApiClient api;
  final CacheStore cache;

  static const pageSize = 12;

  Future<Cached<T>> _cached<T>(String key, Future<Object?> Function() fetch, T Function(Object?) parse) async {
    try {
      final data = await fetch();
      final value = parse(data);
      await cache.put(cachePublic, key, data);
      return Cached(value, DateTime.now().toUtc());
    } on AppError catch (e) {
      if (!e.isNetwork) rethrow;
      final hit = await cache.get(cachePublic, key);
      if (hit == null) rethrow;
      return Cached(parse(hit.value), hit.fetchedAt);
    }
  }

  Future<MealPage> meals(MealQuery q, int page) async {
    final first = page == 1 && q.isEmpty;
    Future<Object?> fetch() => api.get('/meals', (d) => d, query: q.params(page, pageSize), options: publicOptions);
    if (!first) return MealPage.fromJson(asMap(await fetch()));
    return (await _cached('meals_p1', fetch, (d) => MealPage.fromJson(asMap(d)))).value;
  }

  Future<Cached<MealDetail>> meal(String slug) =>
      _cached('meal_$slug', () => api.get('/meals/$slug', (d) => d, options: publicOptions), (d) => MealDetail.fromJson(asMap(d)));

  Future<List<MealCategory>> categories() async =>
      (await _cached('categories', () => api.get('/meal-categories', (d) => d, options: publicOptions), (d) => itemsOf(d, MealCategory.fromJson)))
          .value;

  Future<Reference> reference() async =>
      (await _cached('reference', () => api.get('/reference', (d) => d, options: publicOptions), (d) => Reference.fromJson(asMap(d)))).value;

  Future<List<MenuSummary>> menus() async =>
      (await _cached('menus', () => api.get('/weekly-menus', (d) => d, options: publicOptions), (d) => itemsOf(d, MenuSummary.fromJson))).value;

  Future<Cached<WeeklyMenu>> menu(String week) =>
      _cached('menu_$week', () => api.get('/weekly-menus/$week', (d) => d, options: publicOptions), (d) => WeeklyMenu.fromJson(asMap(d)));
}

final catalogueApiProvider = Provider<CatalogueApi>((ref) => CatalogueApi(ref.watch(apiClientProvider), ref.watch(cacheStoreProvider)));

final categoriesProvider = FutureProvider<List<MealCategory>>((ref) => ref.watch(catalogueApiProvider).categories());
final referenceProvider = FutureProvider<Reference>((ref) => ref.watch(catalogueApiProvider).reference());
final menusProvider = FutureProvider<List<MenuSummary>>((ref) => ref.watch(catalogueApiProvider).menus());

/// The first menu still open for choices, or the latest published one.
final currentMenuProvider = FutureProvider<Cached<WeeklyMenu>?>((ref) async {
  final menus = await ref.watch(menusProvider.future);
  if (menus.isEmpty) return null;
  final pick = menus.firstWhere((m) => m.isOpen, orElse: () => menus.first);
  return ref.watch(catalogueApiProvider).menu(pick.week);
});

/// A given week's menu, or the current one when [week] is null.
final menuForWeekProvider = FutureProvider.family<Cached<WeeklyMenu>?, String?>((ref, week) {
  if (week == null) return ref.watch(currentMenuProvider.future);
  return ref.watch(catalogueApiProvider).menu(week);
});

final mealDetailProvider = FutureProvider.family<Cached<MealDetail>, String>((ref, slug) => ref.watch(catalogueApiProvider).meal(slug));

// ---------------------------------------------------------------- favourites

/// Server-backed favourites (`/me/favorites`), shared with the website.
/// Changes are sent first and shown after the server accepts them.
class FavoritesController extends AsyncNotifier<Set<String>> {
  @override
  Future<Set<String>> build() async {
    if (!ref.watch(isSignedInProvider)) return <String>{};
    final api = ref.read(apiClientProvider);
    return api.get('/me/favorites', (d) => strings(asMap(d), 'meal_ids').toSet());
  }

  Future<void> toggle(String mealId) async {
    final api = ref.read(apiClientProvider);
    final current = state.value ?? <String>{};
    if (current.contains(mealId)) {
      await api.delete('/me/favorites/$mealId');
      state = AsyncData({...current}..remove(mealId));
    } else {
      await api.post<void>('/me/favorites', (_) {}, body: {'meal_id': mealId});
      state = AsyncData({...current, mealId});
    }
    ref.invalidate(favoriteMealsProvider);
  }
}

final favoritesProvider = AsyncNotifierProvider<FavoritesController, Set<String>>(FavoritesController.new);

final favoriteMealsProvider = FutureProvider<List<Meal>>((ref) async {
  if (!ref.watch(isSignedInProvider)) return const [];
  return ref.read(apiClientProvider).get('/me/favorites', (d) => itemsOf(d, Meal.fromJson));
});
