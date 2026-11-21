import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/media.dart';
import '../../shared/models/catalogue.dart';
import '../../shared/widgets/stale.dart';
import '../catalogue/catalogue_api.dart';
import '../catalogue/menu_screen.dart';
import '../subscriptions/subscription_api.dart';

/// One meal, verified facts only. Unpublished meals answer 404 from the API
/// and are never shown.
class MealDetailScreen extends ConsumerWidget {
  const MealDetailScreen({super.key, required this.slug});

  final String slug;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(mealDetailProvider(slug));
    return async.when(
      loading: () => Scaffold(appBar: AppBar(), body: const MiseLoading(label: 'Loading meal')),
      error: (e, _) {
        final err = AppError.from(e);
        return Scaffold(
          appBar: AppBar(),
          body: err.kind == ErrorKind.notFound
              ? EmptyState(
                  title: 'This meal is not on the menu',
                  message: 'It may have been retired or not published yet.',
                  actionLabel: 'Browse the menu',
                  onAction: () => context.go('/menu'),
                )
              : ErrorState(message: err.message, onRetry: () => ref.invalidate(mealDetailProvider(slug))),
        );
      },
      data: (cached) => _MealDetailView(detail: cached.value, fetchedAt: cached.fetchedAt),
    );
  }
}

class _MealDetailView extends ConsumerWidget {
  const _MealDetailView({required this.detail, required this.fetchedAt});

  final MealDetail detail;
  final DateTime fetchedAt;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final m = detail.meal;
    final media = ref.watch(mediaUrlProvider);
    final signedIn = ref.watch(isSignedInProvider);
    final favorites = ref.watch(favoritesProvider).value ?? const <String>{};
    final isFavorite = favorites.contains(m.id);
    final menu = ref.watch(currentMenuProvider).value?.value;
    final onMenu = menu?.meals.any((x) => x.id == m.id) ?? false;
    final sub = ref.watch(mySubscriptionProvider).value;
    final hero = detail.images.isNotEmpty ? detail.images.first : m.image;

    return Scaffold(
      body: CustomScrollView(
        slivers: [
          SliverAppBar(
            pinned: true,
            backgroundColor: MiseColors.background,
            actions: [
              IconButton(
                tooltip: isFavorite ? 'Remove from favourites' : 'Save to favourites',
                icon: Icon(isFavorite ? Icons.favorite : Icons.favorite_border, color: isFavorite ? MiseColors.accent : null),
                onPressed: () => toggleFavorite(context, ref, m),
              ),
            ],
          ),
          SliverToBoxAdapter(child: StaleNotice(fetchedAt: fetchedAt)),
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
              child: Hero(
                tag: 'meal-${m.slug}',
                child: MiseNetworkImage(url: media(hero?.src), alt: hero?.alt ?? m.name, aspectRatio: 4 / 3),
              ),
            ),
          ),
          if (hero?.credit != null)
            SliverToBoxAdapter(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 4, MiseSpace.gutter, 0),
                child: Text(hero!.credit!, style: MiseType.caption),
              ),
            ),
          SliverPadding(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.lg, MiseSpace.gutter, MiseSpace.xxl),
            sliver: SliverList.list(
              children: [
                if (m.category != null) Text(m.category!.label, style: MiseType.caption),
                Semantics(header: true, child: Text(m.name, style: MiseType.h1)),
                if (m.isDemo) ...[const SizedBox(height: MiseSpace.xs), const MiseTag('Demo meal — not for sale', tone: TagTone.warning)],
                const SizedBox(height: MiseSpace.md),
                _Facts(meal: m),
                const SizedBox(height: MiseSpace.lg),
                Text(detail.description.isNotEmpty ? detail.description : m.summary, style: MiseType.bodyLarge),
                if (m.dietary.isNotEmpty) ...[
                  const SizedBox(height: MiseSpace.md),
                  Wrap(spacing: 6, runSpacing: 6, children: [for (final d in m.dietary) MiseTag(d.label, tone: TagTone.success)]),
                ],
                const SizedBox(height: MiseSpace.lg),
                _Availability(onMenu: onMenu, menuLabel: menu?.summary.weekLabel),
                _Section(title: 'Allergens', child: _Allergens(info: m.allergens)),
                if (detail.ingredients.isNotEmpty)
                  _Section(
                    title: 'In your box',
                    child: Column(
                      children: [
                        for (final i in detail.ingredients)
                          MergeSemantics(
                            child: Padding(
                              padding: const EdgeInsets.symmetric(vertical: 8),
                              child: Row(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Expanded(child: Text(i.name, style: MiseType.body)),
                                  if (i.quantity != null) Text(i.quantity!, style: MiseType.figures(MiseType.bodySmall)),
                                ],
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                if (detail.cookingOverview.isNotEmpty) _Section(title: 'How it comes together', child: Text(detail.cookingOverview, style: MiseType.body)),
                _Section(
                  title: 'Nutrition per serving',
                  child: detail.nutrition == null
                      ? Text('Nutrition figures are shown once our team has verified them.', style: MiseType.bodySmall)
                      : _NutritionTable(n: detail.nutrition!),
                ),
                if (detail.chef != null)
                  _Section(
                    title: 'From the kitchen of',
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(detail.chef!.name, style: MiseType.h4),
                        if (detail.chef!.roleTitle != null) Text(detail.chef!.roleTitle!, style: MiseType.caption),
                        if (detail.chef!.bio.isNotEmpty) ...[const SizedBox(height: MiseSpace.xs), Text(detail.chef!.bio, style: MiseType.body)],
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
      bottomNavigationBar: m.isDemo
          ? null
          : BottomActionBar(
              child: sub != null && sub.isActiveLike && sub.openCycle != null
                  ? MiseButton(
                      label: onMenu ? 'Choose meals for ${sub.openCycle!.weekLabel}' : 'Go to my box',
                      icon: Icons.inventory_2_outlined,
                      onPressed: () => context.go(onMenu ? '/box/select/${sub.openCycle!.id}' : '/box'),
                    )
                  : MiseButton(
                      label: signedIn && sub != null ? 'Manage my subscription' : 'See plans',
                      onPressed: () => context.push(signedIn && sub != null ? '/subscription' : '/plans'),
                    ),
            ),
    );
  }
}

class _Facts extends StatelessWidget {
  const _Facts({required this.meal});

  final Meal meal;

  @override
  Widget build(BuildContext context) {
    final facts = [
      if (meal.prepMinutes > 0) ('Prep', '${meal.prepMinutes} min'),
      if (meal.cookMinutes > 0) ('Cook', '${meal.cookMinutes} min'),
      if (meal.servings > 0) ('Serves', '${meal.servings}'),
      if (meal.calories != null) ('Calories', '${meal.calories}'),
    ];
    return Row(
      children: [
        for (final (label, value) in facts)
          Expanded(
            child: MergeSemantics(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [Text(label, style: MiseType.caption), Text(value, style: MiseType.figures(MiseType.h4))],
              ),
            ),
          ),
      ],
    );
  }
}

class _Availability extends StatelessWidget {
  const _Availability({required this.onMenu, this.menuLabel});

  final bool onMenu;
  final String? menuLabel;

  @override
  Widget build(BuildContext context) {
    // Favourites and this page never promise availability.
    return NoticeBanner(
      tone: onMenu ? NoticeTone.success : NoticeTone.info,
      icon: onMenu ? Icons.event_available_outlined : Icons.event_busy_outlined,
      message: onMenu ? 'On the menu for ${menuLabel ?? 'this week'}.' : 'Not on this week\'s menu. Meals return on later menus.',
    );
  }
}

class _Section extends StatelessWidget {
  const _Section({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: MiseSpace.xl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Divider(color: MiseColors.foreground),
          const SizedBox(height: MiseSpace.sm),
          Semantics(header: true, child: Text(title, style: MiseType.h3)),
          const SizedBox(height: MiseSpace.sm),
          child,
        ],
      ),
    );
  }
}

class _Allergens extends StatelessWidget {
  const _Allergens({required this.info});

  final AllergenInfo info;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (!info.declared)
          const NoticeBanner(
            tone: NoticeTone.warning,
            message: 'Allergens for this meal have not been confirmed yet. If you have an allergy, do not order it until they are.',
          )
        else if (info.contains.isEmpty)
          Text('No allergens declared from its ingredients.', style: MiseType.body)
        else ...[
          Text('Contains', style: MiseType.label),
          const SizedBox(height: MiseSpace.xs),
          Wrap(spacing: 6, runSpacing: 6, children: [for (final a in info.contains) MiseTag(a.label, tone: TagTone.error)]),
        ],
        if (info.notice.isNotEmpty) ...[const SizedBox(height: MiseSpace.sm), Text(info.notice, style: MiseType.caption)],
      ],
    );
  }
}

class _NutritionTable extends StatelessWidget {
  const _NutritionTable({required this.n});

  final Nutrition n;

  @override
  Widget build(BuildContext context) {
    String g(double v) => v == v.roundToDouble() ? '${v.round()} g' : '${v.toStringAsFixed(1)} g';
    final rows = [
      ('Energy', '${n.calories} kcal'),
      ('Protein', g(n.proteinG)),
      ('Carbohydrate', g(n.carbsG)),
      ('Fat', g(n.fatG)),
      if (n.fibreG != null) ('Fibre', g(n.fibreG!)),
      if (n.sodiumMg != null) ('Sodium', '${n.sodiumMg} mg'),
    ];
    return Column(
      children: [
        for (final (k, v) in rows)
          MergeSemantics(
            child: Container(
              padding: const EdgeInsets.symmetric(vertical: 8),
              decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: MiseColors.border))),
              child: Row(children: [Expanded(child: Text(k, style: MiseType.body)), Text(v, style: MiseType.figures(MiseType.body))]),
            ),
          ),
      ],
    );
  }
}
