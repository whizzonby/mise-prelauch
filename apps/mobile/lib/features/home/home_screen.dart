import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/media.dart';
import '../../shared/models/commerce.dart';
import '../../shared/widgets/stale.dart';
import '../catalogue/catalogue_api.dart';
import '../notifications/notifications_api.dart';
import '../subscriptions/subscription_api.dart';

/// Home. Subscribers see their next box and its deadline first; everyone
/// else sees this week's menu and how plans work. Everything is real API
/// data: no popularity figures, no recommendations from AI.
class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authControllerProvider);
    final sub = ref.watch(mySubscriptionProvider);
    final unread = ref.watch(unreadNotificationsProvider);
    final firstName = auth is SignedIn ? auth.customer?.firstName : null;

    Future<void> refresh() async {
      ref.invalidate(menusProvider);
      ref.invalidate(currentMenuProvider);
      ref.invalidate(categoriesProvider);
      ref.invalidate(mySubscriptionProvider);
      ref.invalidate(notificationsProvider);
      await ref.read(currentMenuProvider.future).catchError((_) => null);
    }

    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: RefreshIndicator(
          onRefresh: refresh,
          child: ListView(
            padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.md, MiseSpace.xs, 0),
                child: Row(
                  children: [
                    const Wordmark(size: 30),
                    const Spacer(),
                    if (auth is SignedIn)
                      IconButton(
                        tooltip: unread > 0 ? 'Notifications, $unread unread' : 'Notifications',
                        icon: CountBadge(count: unread, child: const Icon(Icons.notifications_none)),
                        onPressed: () => context.push('/notifications'),
                      ),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.lg, MiseSpace.gutter, MiseSpace.lg),
                child: Semantics(
                  header: true,
                  child: Text(firstName != null && firstName.isNotEmpty ? 'Good to see you, $firstName.' : 'Caribbean cooking, made simple.', style: MiseType.h1),
                ),
              ),
              if (auth is SignedIn)
                sub.when(
                  loading: () => const Padding(padding: EdgeInsets.symmetric(horizontal: MiseSpace.gutter), child: Skeleton(height: 140, radius: MiseRadii.md)),
                  error: (e, _) => Padding(
                    padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                    child: NoticeBanner(tone: NoticeTone.warning, message: AppError.from(e).message),
                  ),
                  data: (s) => s == null ? const _StartCard() : _UpcomingBoxCard(sub: s),
                )
              else
                const _StartCard(),
              const SizedBox(height: MiseSpace.xl),
              const _ThisWeek(),
              const SizedBox(height: MiseSpace.xl),
              const _Categories(),
              const SizedBox(height: MiseSpace.xl),
              const SectionHeader(title: 'Need a hand?'),
              const SizedBox(height: MiseSpace.xs),
              MiseListTile(leading: Icons.help_outline, title: 'Help centre', subtitle: 'Delivery, your box, payments', onTap: () => context.go('/support/help')),
              MiseListTile(leading: Icons.chat_bubble_outline, title: 'Message the Mise team', onTap: () => context.go('/support')),
            ],
          ),
        ),
      ),
    );
  }
}

/// For guests and customers without a subscription: what Mise is and how to start.
class _StartCard extends StatelessWidget {
  const _StartCard();

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
      child: Container(
        padding: const EdgeInsets.all(MiseSpace.lg),
        decoration: const BoxDecoration(color: MiseColors.primary, borderRadius: BorderRadius.all(MiseRadii.md)),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('A box of Caribbean dinners, every week.', style: MiseType.h3.copyWith(color: MiseColors.onPrimary)),
            const SizedBox(height: MiseSpace.xs),
            Text(
              'Choose a plan, pick your meals from the weekly menu, and cook with everything measured.',
              style: MiseType.body.copyWith(color: MiseColors.onPrimary.withValues(alpha: 0.85)),
            ),
            const SizedBox(height: MiseSpace.lg),
            TextButton(
              onPressed: () => context.push('/plans'),
              style: TextButton.styleFrom(
                backgroundColor: MiseColors.surface,
                foregroundColor: MiseColors.primary,
                minimumSize: const Size(double.infinity, 52),
                shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(MiseRadii.md)),
              ),
              child: Text('See plans and prices', style: MiseType.button.copyWith(color: MiseColors.primary)),
            ),
          ],
        ),
      ),
    );
  }
}

/// For subscribers: the next box, its deadline and the one thing to do now.
class _UpcomingBoxCard extends StatelessWidget {
  const _UpcomingBoxCard({required this.sub});

  final Subscription sub;

  @override
  Widget build(BuildContext context) {
    final open = sub.openCycle;
    final next = open ?? sub.upcoming;
    final outstanding = sub.outstanding;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
      child: Container(
        padding: const EdgeInsets.all(MiseSpace.lg),
        decoration: BoxDecoration(
          color: MiseColors.surface,
          border: Border.all(color: MiseColors.border),
          borderRadius: const BorderRadius.all(MiseRadii.md),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(child: Text('Your next box', style: MiseType.caption)),
                MiseTag(sub.statusLabel, tone: sub.status == 'past_due' ? TagTone.error : TagTone.neutral),
              ],
            ),
            const SizedBox(height: 4),
            Text(next?.weekLabel ?? sub.planName, style: MiseType.h3),
            Text('${sub.planName} · ${plural(sub.mealsPerWeek, 'meal')}', style: MiseType.bodySmall),
            const SizedBox(height: MiseSpace.md),
            if (outstanding != null) ...[
              NoticeBanner(tone: NoticeTone.error, message: '${outstanding.weekLabel} is not paid yet. Pay to keep your delivery.'),
              const SizedBox(height: MiseSpace.md),
              MiseButton(label: 'Pay for ${outstanding.weekLabel}', onPressed: () => context.push('/subscription')),
            ] else if (open != null) ...[
              SelectionProgress(chosen: open.selectedCount, required: open.mealsPerWeek),
              if (open.cutoffAt != null) ...[
                const SizedBox(height: MiseSpace.sm),
                Text(
                  'Choose by ${formatInstant(open.cutoffAt!)} · ${timeLeft(open.cutoffAt!, DateTime.now().toUtc())}',
                  style: MiseType.label.copyWith(color: MiseColors.warning),
                ),
              ],
              const SizedBox(height: MiseSpace.md),
              MiseButton(
                label: open.confirmed ? 'Change my meals' : 'Choose my meals',
                onPressed: () => context.go('/box/select/${open.id}'),
              ),
            ] else
              MiseButton.secondary(label: 'Open My Box', onPressed: () => context.go('/box')),
          ],
        ),
      ),
    );
  }
}

class _ThisWeek extends ConsumerWidget {
  const _ThisWeek();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final menu = ref.watch(currentMenuProvider);
    final media = ref.watch(mediaUrlProvider);
    return menu.when(
      loading: () => const Padding(padding: EdgeInsets.symmetric(horizontal: MiseSpace.gutter), child: Skeleton(height: 240, radius: MiseRadii.lg)),
      error: (e, _) => Padding(
        padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
        child: NoticeBanner(tone: NoticeTone.warning, message: AppError.from(e).message),
      ),
      data: (cached) {
        if (cached == null || cached.value.meals.isEmpty) {
          return const SizedBox.shrink();
        }
        final m = cached.value;
        final featured = m.meals.first;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            SectionHeader(title: m.summary.title ?? 'On the menu', action: 'See all', onAction: () => context.go('/menu')),
            StaleNotice(fetchedAt: cached.fetchedAt),
            Padding(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
              child: Text(m.summary.weekLabel, style: MiseType.caption),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
              child: MealCard(
                name: featured.name,
                imageUrl: media(featured.image?.src),
                imageAlt: featured.image?.alt ?? '',
                meta: featured.facts,
                onTap: () => context.push('/menu/meal/${featured.slug}'),
              ),
            ),
            if (m.meals.length > 1) ...[
              const SizedBox(height: MiseSpace.lg),
              SizedBox(
                height: 236,
                child: ListView.separated(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                  itemCount: m.meals.length - 1,
                  separatorBuilder: (_, _) => const SizedBox(width: MiseSpace.md),
                  itemBuilder: (context, i) {
                    final meal = m.meals[i + 1];
                    return SizedBox(
                      width: 160,
                      child: MealCard(
                        compact: true,
                        name: meal.name,
                        imageUrl: media(meal.image?.src),
                        imageAlt: meal.image?.alt ?? '',
                        meta: meal.totalMinutes > 0 ? '${meal.totalMinutes} min' : null,
                        onTap: () => context.push('/menu/meal/${meal.slug}'),
                      ),
                    );
                  },
                ),
              ),
            ],
          ],
        );
      },
    );
  }
}

class _Categories extends ConsumerWidget {
  const _Categories();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cats = ref.watch(categoriesProvider).value ?? const [];
    final shown = cats.where((c) => c.mealCount > 0).toList();
    if (shown.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SectionHeader(title: 'Browse by kind'),
        for (final c in shown)
          MiseListTile(title: c.name, subtitle: plural(c.mealCount, 'meal'), onTap: () => context.go('/menu')),
      ],
    );
  }
}
