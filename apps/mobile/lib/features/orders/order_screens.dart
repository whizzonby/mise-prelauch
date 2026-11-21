import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/models/orders.dart';
import '../../shared/widgets/stale.dart';
import 'orders_api.dart';

TagTone _tone(String status) => switch (status) {
  'delivered' => TagTone.success,
  'cancelled' => TagTone.error,
  'pending' => TagTone.warning,
  _ => TagTone.neutral,
};

class OrdersScreen extends ConsumerWidget {
  const OrdersScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(ordersProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Orders')),
      body: RefreshIndicator(
        onRefresh: () async => ref.invalidate(ordersProvider),
        child: async.when(
          loading: () => const MiseLoading(label: 'Loading orders'),
          error: (e, _) => ListView(children: [ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(ordersProvider))]),
          data: (cached) {
            final list = cached.value;
            if (list.isEmpty) {
              return ListView(
                children: const [EmptyState(icon: Icons.receipt_long_outlined, title: 'No orders yet', message: 'Your boxes appear here once they are paid.')],
              );
            }
            final current = list.where((o) => !{'delivered', 'cancelled'}.contains(o.status)).toList();
            final past = list.where((o) => {'delivered', 'cancelled'}.contains(o.status)).toList();
            return ListView(
              padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
              children: [
                StaleNotice(fetchedAt: cached.fetchedAt),
                if (current.isNotEmpty) ...[const SectionHeader(title: 'Current'), for (final o in current) _OrderRow(order: o)],
                if (past.isNotEmpty) ...[const SizedBox(height: MiseSpace.lg), const SectionHeader(title: 'Previous'), for (final o in past) _OrderRow(order: o)],
              ],
            );
          },
        ),
      ),
    );
  }
}

class _OrderRow extends StatelessWidget {
  const _OrderRow({required this.order});

  final OrderSummary order;

  @override
  Widget build(BuildContext context) {
    final o = order;
    return MiseListTile(
      title: 'Week of ${formatShortDate(o.week)}',
      subtitle: '${o.number} · ${plural(o.mealsCount, 'meal')} · ${formatMoney(o.totalMinor, o.currency)}',
      trailing: Row(mainAxisSize: MainAxisSize.min, children: [MiseTag(orderStatusLabel(o.status), tone: _tone(o.status)), const Icon(Icons.chevron_right, color: MiseColors.muted)]),
      onTap: () => context.push('/orders/${o.id}'),
    );
  }
}

class OrderDetailScreen extends ConsumerWidget {
  const OrderDetailScreen({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(orderProvider(id));
    return Scaffold(
      appBar: AppBar(title: const Text('Order')),
      body: async.when(
        loading: () => const MiseLoading(),
        error: (e, _) {
          final err = AppError.from(e);
          return err.kind == ErrorKind.notFound
              ? const EmptyState(title: 'Order not found')
              : ErrorState(message: err.message, onRetry: () => ref.invalidate(orderProvider(id)));
        },
        data: (cached) {
          final o = cached.value;
          final s = o.summary;
          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(orderProvider(id)),
            child: ListView(
              padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
              children: [
                StaleNotice(fetchedAt: cached.fetchedAt),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(s.number, style: MiseType.caption),
                      Semantics(header: true, child: Text('Week of ${formatDate(s.week)}', style: MiseType.h2)),
                      const SizedBox(height: MiseSpace.xs),
                      Wrap(spacing: 6, children: [
                        MiseTag(orderStatusLabel(s.status), tone: _tone(s.status)),
                        MiseTag(o.paid ? 'Paid' : 'Not paid', tone: o.paid ? TagTone.success : TagTone.warning),
                      ]),
                      if (o.deliveryOutcomeState == 'disputed') ...[
                        const SizedBox(height: MiseSpace.md),
                        NoticeBanner(tone: NoticeTone.warning, message: 'The delivery of this box is being reviewed. ${o.deliveryOutcomeReason ?? ''}'),
                      ],
                      const SizedBox(height: MiseSpace.lg),
                      if (s.status != 'cancelled' && s.status != 'pending')
                        MiseButton(label: 'Track delivery', icon: Icons.local_shipping_outlined, onPressed: () => context.push('/orders/$id/delivery')),
                    ],
                  ),
                ),
                const SizedBox(height: MiseSpace.lg),
                const SectionHeader(title: 'In this box'),
                for (final it in o.items)
                  MiseListTile(
                    title: '${it.quantity} × ${it.name}',
                    subtitle: [if (it.servings > 0) 'Serves ${it.servings}', if (it.allergens.isNotEmpty) 'Contains ${it.allergens.join(', ')}'].join(' · '),
                    trailing: o.cookable
                        ? TextButton.icon(
                            onPressed: () => context.push('/orders/$id/cook/${it.mealId}'),
                            icon: const Icon(Icons.soup_kitchen_outlined),
                            label: const Text('Cook'),
                          )
                        : null,
                  ),
                const SizedBox(height: MiseSpace.lg),
                const SectionHeader(title: 'Payment'),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
                  child: Column(
                    children: [
                      _MoneyRow('Meals', formatMoney(o.subtotalMinor, s.currency)),
                      if (o.discountMinor != 0) _MoneyRow('Discount', formatMoney(-o.discountMinor, s.currency)),
                      _MoneyRow('Delivery', formatMoney(o.deliveryFeeMinor, s.currency)),
                      if (o.taxMinor != 0) _MoneyRow('Tax', formatMoney(o.taxMinor, s.currency)),
                      const Divider(),
                      _MoneyRow('Total', formatMoney(s.totalMinor, s.currency), strong: true),
                    ],
                  ),
                ),
                const SizedBox(height: MiseSpace.lg),
                const SectionHeader(title: 'Delivery address'),
                Padding(
                  padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.xs, MiseSpace.gutter, 0),
                  child: Text(o.addressLines.join('\n'), style: MiseType.body),
                ),
                const SizedBox(height: MiseSpace.lg),
                MiseListTile(leading: Icons.chat_bubble_outline, title: 'Help with this order', onTap: () => context.push('/support/new?order=${s.number}')),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _MoneyRow extends StatelessWidget {
  const _MoneyRow(this.label, this.value, {this.strong = false});

  final String label;
  final String value;
  final bool strong;

  @override
  Widget build(BuildContext context) {
    final style = strong ? MiseType.h4 : MiseType.body;
    return MergeSemantics(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Row(children: [Expanded(child: Text(label, style: style)), Text(value, style: MiseType.figures(style))]),
      ),
    );
  }
}
