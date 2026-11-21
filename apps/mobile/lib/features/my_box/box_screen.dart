import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/models/commerce.dart';
import '../subscriptions/subscription_api.dart';

String billingLabel(BillingRef? b) => switch (b?.status) {
  'paid' => 'Paid',
  'due' || 'pending' => 'Payment due',
  'failed' => 'Payment failed',
  'void' => 'Not charged',
  _ => 'Not billed yet',
};

TagTone billingTone(BillingRef? b) => switch (b?.status) {
  'paid' => TagTone.success,
  'failed' => TagTone.error,
  'due' || 'pending' => TagTone.warning,
  _ => TagTone.neutral,
};

/// My Box: the next box and everything about it on one screen, then the
/// weeks after it and the boxes already delivered.
class BoxScreen extends ConsumerWidget {
  const BoxScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!ref.watch(isSignedInProvider)) {
      return Scaffold(
        appBar: AppBar(title: Text('My Box', style: MiseType.h2)),
        body: EmptyState(
          icon: Icons.inventory_2_outlined,
          title: 'Your box lives here',
          message: 'Sign in to see your next delivery, choose your meals and track your box.',
          actionLabel: 'Sign in',
          onAction: () => context.push(Uri(path: '/sign-in', queryParameters: {'from': '/box'}).toString()),
        ),
      );
    }
    final sub = ref.watch(mySubscriptionProvider);
    Future<void> refresh() async {
      ref.invalidate(mySubscriptionProvider);
      await ref.read(mySubscriptionProvider.future).catchError((_) => null);
    }

    return Scaffold(
      appBar: AppBar(title: Text('My Box', style: MiseType.h2)),
      body: RefreshIndicator(
        onRefresh: refresh,
        child: sub.when(
          loading: () => const MiseLoading(label: 'Loading your box'),
          error: (e, _) => ListView(children: [ErrorState(message: AppError.from(e).message, onRetry: refresh)]),
          data: (s) {
            if (s == null) {
              return ListView(
                children: [
                  EmptyState(
                    icon: Icons.inventory_2_outlined,
                    title: 'No box yet',
                    message: 'Choose a plan to start your first delivery.',
                    actionLabel: 'See plans',
                    onAction: () => context.push('/plans'),
                  ),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                    child: MiseButton.quiet(label: 'Order history', onPressed: () => context.push('/orders')),
                  ),
                ],
              );
            }
            return _BoxView(sub: s);
          },
        ),
      ),
    );
  }
}

class _BoxView extends StatelessWidget {
  const _BoxView({required this.sub});

  final Subscription sub;

  @override
  Widget build(BuildContext context) {
    final next = sub.openCycle ?? sub.upcoming;
    final later = sub.cycles.where((c) => c.id != next?.id).toList();
    return ListView(
      padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
      children: [
        if (sub.testPayments)
          const Padding(
            padding: EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
            child: NoticeBanner(tone: NoticeTone.warning, message: 'Payments are in test mode: no real money moves.'),
          ),
        if (sub.outstanding != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
            child: NoticeBanner(tone: NoticeTone.error, message: '${sub.outstanding!.weekLabel} is not paid. Pay from your subscription to keep this delivery.'),
          ),
        if (next != null) _WeekPanel(cycle: next, sub: sub, primary: true) else const EmptyState(title: 'No upcoming weeks'),
        const SizedBox(height: MiseSpace.lg),
        const SectionHeader(title: 'Delivery address'),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
          child: sub.address == null
              ? Text('No address on your subscription.', style: MiseType.bodySmall)
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const SizedBox(height: MiseSpace.xs),
                    Text(sub.address!.recipientName, style: MiseType.body),
                    Text(sub.address!.oneLine, style: MiseType.bodySmall),
                    const SizedBox(height: 4),
                    Text(sub.address!.availabilityLabel, style: MiseType.caption),
                  ],
                ),
        ),
        if (later.isNotEmpty) ...[
          const SizedBox(height: MiseSpace.lg),
          const SectionHeader(title: 'Other weeks'),
          for (final c in later) _WeekRow(cycle: c),
        ],
        const SizedBox(height: MiseSpace.lg),
        const SectionHeader(title: 'More'),
        MiseListTile(leading: Icons.receipt_long_outlined, title: 'Previous boxes and orders', onTap: () => context.push('/orders')),
        MiseListTile(leading: Icons.tune, title: 'Manage subscription', subtitle: '${sub.planName} · ${sub.statusLabel}', onTap: () => context.push('/subscription')),
        MiseListTile(leading: Icons.chat_bubble_outline, title: 'Something wrong with a box?', onTap: () => context.go('/support')),
      ],
    );
  }
}

class _WeekPanel extends StatelessWidget {
  const _WeekPanel({required this.cycle, required this.sub, this.primary = false});

  final Cycle cycle;
  final Subscription sub;
  final bool primary;

  @override
  Widget build(BuildContext context) {
    final c = cycle;
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
            Text('Next box', style: MiseType.caption),
            Semantics(header: true, child: Text(c.weekLabel, style: MiseType.h2)),
            const SizedBox(height: MiseSpace.xs),
            Wrap(spacing: 6, runSpacing: 6, children: [MiseTag(c.stateLabel), MiseTag(billingLabel(c.billing), tone: billingTone(c.billing))]),
            const SizedBox(height: MiseSpace.md),
            if (c.selectionOpen || c.selectedCount > 0) SelectionProgress(chosen: c.selectedCount, required: c.mealsPerWeek),
            if (c.cutoffAt != null) ...[
              const SizedBox(height: MiseSpace.xs),
              Text(
                c.selectionOpen ? 'Choose by ${formatInstant(c.cutoffAt!)}' : 'Choices closed ${formatInstant(c.cutoffAt!)}',
                style: MiseType.bodySmall,
              ),
            ],
            if (c.billing != null) ...[
              const SizedBox(height: MiseSpace.xs),
              Text('Total ${formatMoney(c.billing!.totalMinor, c.billing!.currency)}', style: MiseType.figures(MiseType.bodySmall)),
            ],
            const SizedBox(height: MiseSpace.lg),
            if (c.selectionOpen && c.menuPublished)
              MiseButton(label: c.confirmed ? 'Change my meals' : 'Choose my meals', onPressed: () => context.push('/box/select/${c.id}'))
            else if (c.selectionOpen)
              Text('The menu for this week is not out yet.', style: MiseType.bodySmall)
            else if (c.selectedCount > 0)
              MiseButton.secondary(label: 'See my meals', onPressed: () => context.push('/box/select/${c.id}')),
            if (c.orderId != null) ...[
              const SizedBox(height: MiseSpace.xs),
              MiseButton.secondary(label: 'Track delivery', icon: Icons.local_shipping_outlined, onPressed: () => context.push('/orders/${c.orderId}/delivery')),
              MiseButton.quiet(label: 'Order details', expand: true, onPressed: () => context.push('/orders/${c.orderId}')),
            ],
          ],
        ),
      ),
    );
  }
}

class _WeekRow extends StatelessWidget {
  const _WeekRow({required this.cycle});

  final Cycle cycle;

  @override
  Widget build(BuildContext context) {
    final c = cycle;
    final target = c.orderId != null ? '/orders/${c.orderId}' : (c.selectionOpen || c.selectedCount > 0 ? '/box/select/${c.id}' : null);
    return MiseListTile(
      title: c.weekLabel,
      subtitle: '${c.stateLabel} · ${billingLabel(c.billing)}${c.selectedCount > 0 ? ' · ${plural(c.selectedCount, 'meal')} chosen' : ''}',
      onTap: target == null ? null : () => context.push(target),
    );
  }
}
