import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/models/commerce.dart';
import '../subscriptions/subscription_api.dart';

/// Plans exactly as configured on the server: names, prices, meals a week,
/// benefits. A plan without a price, or one staff marked not self-serve,
/// is shown as such and cannot be chosen here.
class PlansScreen extends ConsumerStatefulWidget {
  const PlansScreen({super.key});

  @override
  ConsumerState<PlansScreen> createState() => _PlansScreenState();
}

class _PlansScreenState extends ConsumerState<PlansScreen> {
  String? _selected;

  @override
  Widget build(BuildContext context) {
    final plans = ref.watch(plansProvider);
    final info = ref.watch(subscriptionInfoProvider).value;
    final sub = ref.watch(mySubscriptionProvider).value;
    return Scaffold(
      appBar: AppBar(title: const Text('Plans')),
      body: plans.when(
        loading: () => const MiseLoading(label: 'Loading plans'),
        error: (e, _) => ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(plansProvider)),
        data: (list) {
          final shown = list.where((p) => !p.isDemo || !ref.read(environmentProvider).isProduction).toList();
          if (shown.isEmpty) {
            return const EmptyState(
              icon: Icons.inventory_2_outlined,
              title: 'Plans are not open yet',
              message: 'Mise is preparing its first plans. You can browse the menu meanwhile.',
            );
          }
          final selected = shown.where((p) => p.slug == _selected).firstOrNull;
          return ListView(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xxl),
            children: [
              Semantics(header: true, child: Text('Choose your plan', style: MiseType.h1)),
              const SizedBox(height: MiseSpace.xs),
              Text('Prices are per week. You choose your meals from each weekly menu.', style: MiseType.body.copyWith(color: MiseColors.muted)),
              if (sub != null && sub.isActiveLike) ...[
                const SizedBox(height: MiseSpace.md),
                NoticeBanner(message: 'You already have the ${sub.planName} plan. Change it from your subscription.'),
              ],
              const SizedBox(height: MiseSpace.lg),
              for (final p in shown) ...[
                PlanCard(
                  name: p.name,
                  priceLabel: _priceLabel(p),
                  facts: [
                    '${plural(p.mealsPerWeek, 'meal')} a week',
                    if (p.perMealMinor > 0 && p.price != null) '${formatMoney(p.perMealMinor, p.price!.currency)} a meal',
                    ...p.benefits,
                    if (p.nextPrice?.effectiveFrom != null) 'From ${formatInstant(p.nextPrice!.effectiveFrom!)}: ${p.nextPrice!.formatted} a week',
                    if (!p.purchasable) p.unavailableReason ?? 'Not available to buy in the app yet',
                  ],
                  badge: p.isDemo ? 'Demo' : null,
                  selected: p.slug == _selected,
                  onTap: () => setState(() => _selected = p.slug),
                ),
                const SizedBox(height: MiseSpace.md),
              ],
              if (info != null) ...[
                const SizedBox(height: MiseSpace.md),
                _Terms(info: info),
              ],
              const SizedBox(height: MiseSpace.lg),
              MiseButton(
                label: selected == null ? 'Choose a plan' : 'Continue with ${selected.name}',
                onPressed: selected == null || !selected.purchasable || (sub != null && sub.isActiveLike)
                    ? null
                    : () => context.push('/plans/${selected.slug}'),
              ),
              if (selected != null && !selected.purchasable) ...[
                const SizedBox(height: MiseSpace.xs),
                Text(selected.unavailableReason ?? 'This plan cannot be bought in the app.', style: MiseType.caption),
              ],
            ],
          );
        },
      ),
    );
  }

  String _priceLabel(Plan p) => p.price == null ? 'Price to be announced' : '${p.price!.formatted} a week';
}

/// The renewal, cutoff and cancellation rules, as the server states them.
class _Terms extends StatelessWidget {
  const _Terms({required this.info});

  final Map<String, dynamic> info;

  @override
  Widget build(BuildContext context) {
    final cutoff = info['cutoff_rule'] as String?;
    final maxPause = (info['max_pause_weeks'] as num?)?.toInt();
    final payments = info['payments'] is Map ? info['payments'] as Map : const {};
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Divider(color: MiseColors.foreground),
        const SizedBox(height: MiseSpace.sm),
        Text('How it works', style: MiseType.h4),
        const SizedBox(height: MiseSpace.xs),
        for (final line in [
          'You pay for each week before it is delivered.',
          if (cutoff != null && cutoff.isNotEmpty) cutoff,
          if (maxPause != null && maxPause > 0) 'Skip up to ${plural(maxPause, 'week')} at a time.',
          'Cancel from the app before the next unpaid week. Paid weeks are still delivered; there are no automatic refunds.',
          if (payments['test'] == true) 'Payments are in test mode: no real money moves.',
        ])
          Padding(
            padding: const EdgeInsets.only(bottom: MiseSpace.xs),
            child: Text('• $line', style: MiseType.bodySmall.copyWith(color: MiseColors.foreground)),
          ),
      ],
    );
  }
}
