import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/media.dart';
import '../../shared/models/catalogue.dart';
import '../../shared/widgets/stale.dart';
import 'catalogue_api.dart';

/// Saves or removes a favourite. Guests are asked to sign in instead; the
/// change is shown only after the server accepts it.
Future<void> toggleFavorite(BuildContext context, WidgetRef ref, Meal meal) async {
  if (!ref.read(isSignedInProvider)) {
    final go = await confirmAction(
      context,
      title: 'Save meals you love',
      message: 'Sign in to keep favourites on this phone and on mise.tt.',
      confirmLabel: 'Sign in',
      cancelLabel: 'Not now',
    );
    if (go && context.mounted) await context.push<void>(Uri(path: '/sign-in', queryParameters: {'from': '/menu/meal/${meal.slug}'}).toString());
    return;
  }
  try {
    await ref.read(favoritesProvider.notifier).toggle(meal.id);
  } on AppError catch (e) {
    if (context.mounted) showMessage(context, e.message);
  }
}

/// The catalogue: this week's menu, or every published meal with search and
/// filters. Everything shown comes from the API; filters run on the server.
class MenuScreen extends ConsumerStatefulWidget {
  const MenuScreen({super.key, this.week});

  /// Open on a given week's menu (deep link `/menu/week/<date>`).
  final String? week;

  @override
  ConsumerState<MenuScreen> createState() => _MenuScreenState();
}

enum _View { week, all }

class _MenuScreenState extends ConsumerState<MenuScreen> {
  late _View _view = widget.week != null ? _View.week : _View.all;
  final _search = TextEditingController();
  final _scroll = ScrollController();
  Timer? _debounce;

  MealQuery _query = const MealQuery();
  final List<Meal> _meals = [];
  int _page = 0;
  bool _hasMore = true;
  bool _loading = false;
  AppError? _error;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(() {
      if (_scroll.position.pixels > _scroll.position.maxScrollExtent - 600) _loadMore();
    });
    _reload();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _reload() async {
    setState(() {
      _meals.clear();
      _page = 0;
      _hasMore = true;
      _error = null;
    });
    await _loadMore();
  }

  Future<void> _loadMore() async {
    if (_loading || !_hasMore || _view != _View.all) return;
    setState(() => _loading = true);
    final query = _query;
    try {
      final page = await ref.read(catalogueApiProvider).meals(query, _page + 1);
      if (!mounted || query != _query) return;
      setState(() {
        _meals.addAll(page.items);
        _page = page.page;
        _hasMore = page.hasMore;
      });
    } on AppError catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _setQuery(MealQuery q) {
    if (q == _query) return;
    _query = q;
    _reload();
  }

  void _onSearch(String text) {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 350), () => _setQuery(_query.copyWith(q: text)));
  }

  Future<void> _openFilters() async {
    final result = await showMiseSheet<MealQuery>(context, title: 'Filter meals', builder: (_) => _FilterSheet(initial: _query));
    if (result != null) _setQuery(result);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('Menu', style: MiseType.h2),
        actions: [
          IconButton(tooltip: 'Favourites', icon: const Icon(Icons.favorite_border), onPressed: () => context.push('/account/favorites')),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.sm),
            child: SegmentedButton<_View>(
              showSelectedIcon: false,
              segments: const [
                ButtonSegment(value: _View.week, label: Text('This week')),
                ButtonSegment(value: _View.all, label: Text('All meals')),
              ],
              selected: {_view},
              onSelectionChanged: (s) {
                setState(() => _view = s.first);
                if (_view == _View.all && _meals.isEmpty) _reload();
              },
            ),
          ),
          Expanded(child: _view == _View.week ? _WeekView(week: widget.week) : _allMeals()),
        ],
      ),
    );
  }

  Widget _allMeals() {
    final favorites = ref.watch(favoritesProvider).value ?? const <String>{};
    final signedIn = ref.watch(isSignedInProvider);
    final media = ref.watch(mediaUrlProvider);
    final categories = ref.watch(categoriesProvider).value ?? const [];
    final active = _query.diet.length + _query.excludeAllergens.length + (_query.maxMinutes == null ? 0 : 1);

    return RefreshIndicator(
      onRefresh: _reload,
      child: CustomScrollView(
        controller: _scroll,
        physics: const AlwaysScrollableScrollPhysics(),
        slivers: [
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.sm),
              child: Row(
                children: [
                  Expanded(child: MiseSearchField(controller: _search, onChanged: _onSearch)),
                  const SizedBox(width: MiseSpace.xs),
                  Semantics(
                    button: true,
                    label: active == 0 ? 'Filters' : 'Filters, $active on',
                    child: CountBadge(
                      count: active,
                      child: IconButton.outlined(
                        onPressed: _openFilters,
                        icon: const Icon(Icons.tune),
                        style: IconButton.styleFrom(
                          minimumSize: const Size(52, 52),
                          shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(MiseRadii.md)),
                          side: const BorderSide(color: MiseColors.borderStrong),
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (categories.isNotEmpty)
            SliverToBoxAdapter(
              child: Padding(
                padding: const EdgeInsets.only(bottom: MiseSpace.md),
                child: FilterChips<String?>(
                  label: 'Categories',
                  options: [(null, 'All'), for (final c in categories) (c.slug, c.name)],
                  selected: {_query.category},
                  onToggle: (slug) => _setQuery(_query.copyWith(category: slug)),
                ),
              ),
            ),
          if (_error != null && _meals.isEmpty)
            SliverToBoxAdapter(child: ErrorState(message: _error!.message, onRetry: _reload))
          else if (_meals.isEmpty && _loading)
            const SliverToBoxAdapter(child: MealListSkeleton())
          else if (_meals.isEmpty)
            SliverToBoxAdapter(
              child: EmptyState(
                title: _query.isEmpty ? 'No meals published yet' : 'No meals match',
                message: _query.isEmpty ? 'The kitchen is preparing the menu. Check back soon.' : 'Try fewer filters or another search.',
                actionLabel: _query.isEmpty ? null : 'Clear filters',
                onAction: () {
                  _search.clear();
                  _setQuery(const MealQuery());
                },
              ),
            )
          else
            SliverPadding(
              padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
              sliver: SliverList.separated(
                itemCount: _meals.length,
                separatorBuilder: (_, _) => const SizedBox(height: MiseSpace.xl),
                itemBuilder: (context, i) {
                  final m = _meals[i];
                  return MealCard(
                    name: m.name,
                    imageUrl: media(m.image?.src),
                    imageAlt: m.image?.alt ?? '',
                    meta: m.facts,
                    tags: [for (final d in m.dietary.take(3)) d.label, if (m.isDemo) 'Demo'],
                    heroTag: 'meal-${m.slug}',
                    favorite: signedIn ? favorites.contains(m.id) : false,
                    onFavorite: () => toggleFavorite(context, ref, m),
                    onTap: () => context.push('/menu/meal/${m.slug}'),
                  );
                },
              ),
            ),
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: MiseSpace.xl),
              child: _loading && _meals.isNotEmpty
                  ? const MiseLoading(label: 'Loading more meals')
                  : (_error != null && _meals.isNotEmpty)
                  ? Center(child: MiseButton.quiet(label: 'Could not load more. Try again', onPressed: _loadMore))
                  : const SizedBox.shrink(),
            ),
          ),
        ],
      ),
    );
  }
}

/// This week's menu: the meals the kitchen is cooking for one delivery week.
class _WeekView extends ConsumerWidget {
  const _WeekView({this.week});

  final String? week;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(menuForWeekProvider(week));
    final media = ref.watch(mediaUrlProvider);
    final favorites = ref.watch(favoritesProvider).value ?? const <String>{};
    final signedIn = ref.watch(isSignedInProvider);

    Future<void> refresh() async {
      ref.invalidate(menusProvider);
      ref.invalidate(currentMenuProvider);
      ref.invalidate(menuForWeekProvider(week));
    }

    return RefreshIndicator(
      onRefresh: refresh,
      child: async.when(
        loading: () => ListView(children: const [MealListSkeleton()]),
        error: (e, _) => ListView(children: [ErrorState(message: AppError.from(e).message, onRetry: refresh)]),
        data: (cached) {
          if (cached == null) {
            return ListView(
              children: const [
                EmptyState(
                  icon: Icons.event_note_outlined,
                  title: 'No menu published yet',
                  message: "The next week's menu appears here as soon as the kitchen publishes it. Browse all meals meanwhile.",
                ),
              ],
            );
          }
          final menu = cached.value;
          final s = menu.summary;
          return ListView(
            padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
            children: [
              StaleNotice(fetchedAt: cached.fetchedAt),
              Padding(
                padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.sm, MiseSpace.gutter, MiseSpace.lg),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(s.weekLabel, style: MiseType.caption),
                    const SizedBox(height: 4),
                    Semantics(header: true, child: Text(s.title ?? 'This week at Mise', style: MiseType.h1)),
                    if (menu.note != null && menu.note!.isNotEmpty) ...[const SizedBox(height: MiseSpace.xs), Text(menu.note!, style: MiseType.body)],
                    const SizedBox(height: MiseSpace.sm),
                    if (s.cutoffAt != null)
                      Text(
                        s.isOpen ? 'Choose by ${formatInstant(s.cutoffAt!)}' : 'Choices for this week have closed',
                        style: MiseType.label.copyWith(color: s.isOpen ? MiseColors.secondary : MiseColors.muted),
                      ),
                  ],
                ),
              ),
              if (menu.meals.isEmpty)
                const EmptyState(title: 'No meals on this menu yet')
              else
                for (final m in menu.meals)
                  Padding(
                    padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xl),
                    child: MealCard(
                      name: m.name,
                      imageUrl: media(m.image?.src),
                      imageAlt: m.image?.alt ?? '',
                      meta: m.facts,
                      tags: [for (final d in m.dietary.take(3)) d.label, if (m.isDemo) 'Demo'],
                      heroTag: 'week-meal-${m.slug}',
                      favorite: signedIn ? favorites.contains(m.id) : false,
                      onFavorite: () => toggleFavorite(context, ref, m),
                      onTap: () => context.push('/menu/meal/${m.slug}'),
                    ),
                  ),
            ],
          );
        },
      ),
    );
  }
}

class _FilterSheet extends ConsumerStatefulWidget {
  const _FilterSheet({required this.initial});

  final MealQuery initial;

  @override
  ConsumerState<_FilterSheet> createState() => _FilterSheetState();
}

class _FilterSheetState extends ConsumerState<_FilterSheet> {
  late final Set<String> _diet = {...widget.initial.diet};
  late final Set<String> _exclude = {...widget.initial.excludeAllergens};
  late int? _max = widget.initial.maxMinutes;
  late String _sort = widget.initial.sort;

  @override
  Widget build(BuildContext context) {
    final ref0 = ref.watch(referenceProvider);
    return ref0.when(
      loading: () => const Padding(padding: EdgeInsets.all(MiseSpace.xl), child: MiseLoading()),
      error: (e, _) => ErrorState(message: AppError.from(e).message),
      data: (reference) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Sort', style: MiseType.label),
          const SizedBox(height: MiseSpace.xs),
          Wrap(
            spacing: MiseSpace.xs,
            children: [
              for (final (k, l) in const [('newest', 'Newest'), ('quickest', 'Quickest'), ('name', 'A to Z')])
                ChoiceChip(label: Text(l), selected: _sort == k, onSelected: (_) => setState(() => _sort = k)),
            ],
          ),
          const SizedBox(height: MiseSpace.lg),
          Text('Ready in', style: MiseType.label),
          const SizedBox(height: MiseSpace.xs),
          Wrap(
            spacing: MiseSpace.xs,
            children: [
              for (final m in const [null, 30, 45, 60])
                ChoiceChip(label: Text(m == null ? 'Any time' : '$m min or less'), selected: _max == m, onSelected: (_) => setState(() => _max = m)),
            ],
          ),
          if (reference.dietary.isNotEmpty) ...[
            const SizedBox(height: MiseSpace.lg),
            Text('Dietary', style: MiseType.label),
            const SizedBox(height: MiseSpace.xs),
            Wrap(
              spacing: MiseSpace.xs,
              runSpacing: MiseSpace.xs,
              children: [
                for (final t in reference.dietary)
                  FilterChip(
                    label: Text(t.label),
                    selected: _diet.contains(t.key),
                    onSelected: (on) => setState(() => on ? _diet.add(t.key) : _diet.remove(t.key)),
                  ),
              ],
            ),
          ],
          if (reference.mealAllergens.isNotEmpty) ...[
            const SizedBox(height: MiseSpace.lg),
            Text('Leave out meals that contain', style: MiseType.label),
            const SizedBox(height: MiseSpace.xs),
            Wrap(
              spacing: MiseSpace.xs,
              runSpacing: MiseSpace.xs,
              children: [
                for (final t in reference.mealAllergens)
                  FilterChip(
                    label: Text(t.label),
                    selected: _exclude.contains(t.key),
                    onSelected: (on) => setState(() => on ? _exclude.add(t.key) : _exclude.remove(t.key)),
                  ),
              ],
            ),
            const SizedBox(height: MiseSpace.xs),
            // The server also leaves out meals whose allergens are not declared.
            Text(
              'Meals whose allergens are not yet declared are left out too. ${reference.crossContactNotice}',
              style: MiseType.caption,
            ),
          ],
          const SizedBox(height: MiseSpace.xl),
          MiseButton(
            label: 'Show meals',
            onPressed: () => Navigator.of(context).pop(
              widget.initial.copyWith(diet: _diet, excludeAllergens: _exclude, maxMinutes: _max, sort: _sort),
            ),
          ),
          const SizedBox(height: MiseSpace.xs),
          MiseButton.quiet(
            label: 'Clear all',
            expand: true,
            onPressed: () => Navigator.of(context).pop(MealQuery(q: widget.initial.q, category: widget.initial.category)),
          ),
        ],
      ),
    );
  }
}
