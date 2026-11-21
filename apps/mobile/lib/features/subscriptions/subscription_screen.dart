import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/models/commerce.dart';
import '../my_box/box_screen.dart';
import 'subscription_api.dart';

const _cancelReasons = [
  ('too_expensive', 'It costs too much'),
  ('not_enough_time', 'I don\'t have time to cook'),
  ('meals_not_for_me', 'The meals aren\'t for me'),
  ('moving', 'I\'m moving'),
  ('temporary', 'Just for a while'),
  ('other', 'Something else'),
];

/// The subscription and exactly the actions the server says are allowed now
/// (`actions` in the API). Each action says what will happen before it runs.
class SubscriptionScreen extends ConsumerStatefulWidget {
  const SubscriptionScreen({super.key});

  @override
  ConsumerState<SubscriptionScreen> createState() => _SubscriptionScreenState();
}

class _SubscriptionScreenState extends ConsumerState<SubscriptionScreen> {
  bool _busy = false;

  CommerceApi get _api => ref.read(commerceApiProvider);

  Future<void> _run(Future<void> Function() action, String done) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await action();
      ref.invalidate(mySubscriptionProvider);
      ref.invalidate(weeksProvider);
      if (mounted) showMessage(context, done);
    } on AppError catch (e) {
      if (mounted) showMessage(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _pause(Subscription s) async {
    final weeks = await showMiseSheet<int>(
      context,
      title: 'Skip weeks',
      builder: (ctx) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Skipping starts from your first week that is not paid yet. Paid weeks are still delivered.', style: MiseType.body),
          const SizedBox(height: MiseSpace.md),
          for (var w = 1; w <= s.actions.maxPauseWeeks; w++)
            MiseListTile(title: 'Skip ${plural(w, 'week')}', onTap: () => Navigator.of(ctx).pop(w)),
        ],
      ),
    );
    if (weeks == null || !mounted) return;
    await _run(() => _api.pause(s.id, weeks), 'Skipped ${plural(weeks, 'week')}.');
  }

  Future<void> _cancel(Subscription s) async {
    final reason = await showMiseSheet<String>(
      context,
      title: 'Cancel your subscription',
      builder: (ctx) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'Cancelling stops your plan from the first week that is not paid yet. Weeks you have paid for are still delivered. '
            'There are no automatic refunds. Your order history is kept.',
            style: MiseType.body,
          ),
          const SizedBox(height: MiseSpace.md),
          Text('Why are you leaving?', style: MiseType.label),
          for (final (k, l) in _cancelReasons) MiseListTile(title: l, onTap: () => Navigator.of(ctx).pop(k)),
        ],
      ),
    );
    if (reason == null || !mounted) return;
    final ok = await confirmAction(
      context,
      title: 'Cancel ${s.planName}?',
      message: 'You can restart later by choosing a plan again.',
      confirmLabel: 'Cancel subscription',
      destructive: true,
    );
    if (ok) await _run(() => _api.cancel(s.id, reason), 'Your subscription will end as described.');
  }

  Future<void> _changePlan(Subscription s) async {
    final plans = await ref.read(plansProvider.future);
    if (!mounted) return;
    final options = plans.where((p) => p.id != s.planId && p.purchasable).toList();
    final chosen = await showMiseSheet<Plan>(
      context,
      title: 'Change plan',
      builder: (ctx) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('The new plan starts from your first week not yet billed. There is no proration.', style: MiseType.body),
          const SizedBox(height: MiseSpace.md),
          if (options.isEmpty) Text('No other plans are available right now.', style: MiseType.bodySmall),
          for (final p in options)
            MiseListTile(
              title: p.name,
              subtitle: '${plural(p.mealsPerWeek, 'meal')} a week · ${p.price?.formatted ?? 'price to be announced'}',
              onTap: () => Navigator.of(ctx).pop(p),
            ),
        ],
      ),
    );
    if (chosen == null || !mounted) return;
    await _run(() => _api.changePlan(s.id, chosen.id), 'Changed to ${chosen.name}.');
  }

  Future<void> _changeAddress(Subscription s) async {
    final addresses = await ref.read(addressesProvider.future);
    if (!mounted) return;
    final chosen = await showMiseSheet<Address>(
      context,
      title: 'Deliver to',
      builder: (ctx) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Changes apply to orders not yet prepared.', style: MiseType.body),
          for (final a in addresses)
            MiseListTile(
              title: a.label ?? a.recipientName,
              subtitle: '${a.oneLine}\n${a.availabilityLabel}',
              trailing: a.id == s.address?.id ? const Icon(Icons.check, color: MiseColors.primary) : null,
              onTap: () => Navigator.of(ctx).pop(a),
            ),
          MiseButton.quiet(label: 'Add or edit addresses', expand: true, onPressed: () => ctx.push('/account/addresses')),
        ],
      ),
    );
    if (chosen == null || chosen.id == s.address?.id || !mounted) return;
    await _run(() => _api.changeAddress(s.id, chosen.id), 'Delivery address updated.');
  }

  @override
  Widget build(BuildContext context) {
    final sub = ref.watch(mySubscriptionProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Subscription')),
      body: sub.when(
        loading: () => const MiseLoading(),
        error: (e, _) => ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(mySubscriptionProvider)),
        data: (s) {
          if (s == null) {
            return EmptyState(title: 'No subscription', actionLabel: 'See plans', onAction: () => context.push('/plans'));
          }
          final a = s.actions;
          return AbsorbPointer(
            absorbing: _busy,
            child: ListView(
              padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.lg),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Semantics(header: true, child: Text(s.planName, style: MiseType.h1)),
                      const SizedBox(height: MiseSpace.xs),
                      Wrap(spacing: 6, children: [MiseTag(s.statusLabel, tone: s.status == 'past_due' ? TagTone.error : TagTone.neutral)]),
                      const SizedBox(height: MiseSpace.md),
                      Text('${s.priceFormatted} a week · ${plural(s.mealsPerWeek, 'meal')}', style: MiseType.figures(MiseType.h4)),
                      if (s.pauseLabel != null) Text(s.pauseLabel!, style: MiseType.bodySmall),
                      if (s.cancelLabel != null) Text(s.cancelLabel!, style: MiseType.bodySmall.copyWith(color: MiseColors.error)),
                      Text(
                        s.autoRenew ? 'Each week is charged automatically before it is delivered.' : 'You pay for each week yourself before its deadline.',
                        style: MiseType.bodySmall,
                      ),
                      if (s.testPayments) ...[
                        const SizedBox(height: MiseSpace.sm),
                        const NoticeBanner(tone: NoticeTone.warning, message: 'Payments are in test mode: no real money moves.'),
                      ],
                    ],
                  ),
                ),
                if (s.outstanding != null)
                  Padding(
                    padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.lg),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        NoticeBanner(
                          tone: NoticeTone.error,
                          message: '${s.outstanding!.weekLabel}: ${formatMoney(s.outstanding!.billing?.totalMinor ?? s.outstanding!.amountMinor, s.outstanding!.currency)} to pay.',
                        ),
                        const SizedBox(height: MiseSpace.sm),
                        MiseButton(
                          label: 'Pay for ${s.outstanding!.weekLabel}',
                          onPressed: s.outstanding!.billing == null ? null : () => context.push('/checkout/pay/${s.outstanding!.billing!.id}'),
                        ),
                      ],
                    ),
                  ),
                const SectionHeader(title: 'Weeks'),
                for (final c in s.cycles)
                  MiseListTile(
                    title: c.weekLabel,
                    subtitle: '${c.stateLabel} · ${billingLabel(c.billing)}',
                    trailing: c.billing == null ? null : Text(formatMoney(c.billing!.totalMinor, c.billing!.currency), style: MiseType.figures(MiseType.bodySmall)),
                  ),
                const SizedBox(height: MiseSpace.lg),
                const SectionHeader(title: 'Change your plan'),
                if (a.changePlan) MiseListTile(leading: Icons.swap_horiz, title: 'Change plan', onTap: () => _changePlan(s)),
                if (a.changeAddress) MiseListTile(leading: Icons.home_outlined, title: 'Change delivery address', subtitle: s.address?.oneLine, onTap: () => _changeAddress(s)),
                if (a.pause) MiseListTile(leading: Icons.pause_circle_outline, title: 'Skip weeks', subtitle: 'Up to ${plural(a.maxPauseWeeks, 'week')}', onTap: () => _pause(s)),
                if (a.resume)
                  MiseListTile(
                    leading: Icons.play_circle_outline,
                    title: a.undoCancel ? 'Keep my subscription' : 'Resume deliveries',
                    onTap: () => _run(() => _api.resume(s.id), 'Your subscription continues.'),
                  ),
                if (a.cancel) MiseListTile(leading: Icons.cancel_outlined, title: 'Cancel subscription', danger: true, onTap: () => _cancel(s)),
                if (!a.changePlan && !a.pause && !a.resume && !a.cancel && !a.changeAddress)
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                    child: Text('No changes can be made right now. Contact us if you need help.', style: MiseType.bodySmall),
                  ),
                if (s.terms.isNotEmpty)
                  Padding(padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.lg, MiseSpace.gutter, 0), child: Text(s.terms, style: MiseType.caption)),
              ],
            ),
          );
        },
      ),
    );
  }
}
