import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/models/orders.dart';
import '../orders/orders_api.dart';

/// Maps the server's real milestones to the timeline. The current step is
/// the first not done; an issue marks it as needing attention. No GPS, no
/// arrival time unless the server gives a window.
List<Milestone> milestonesFor(DeliveryStatus d) {
  final firstOpen = d.timeline.indexWhere((m) => !m.done);
  return [
    for (var i = 0; i < d.timeline.length; i++)
      Milestone(
        title: d.timeline[i].label,
        state: d.timeline[i].done
            ? MilestoneState.done
            : i == firstOpen
            ? (d.stage == 'issue' ? MilestoneState.problem : MilestoneState.current)
            : MilestoneState.upcoming,
        detail: d.timeline[i].at == null ? null : formatInstant(d.timeline[i].at!),
      ),
  ];
}

class DeliveryScreen extends ConsumerWidget {
  const DeliveryScreen({super.key, required this.orderId});

  final String orderId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(deliveryProvider(orderId));
    return Scaffold(
      appBar: AppBar(title: const Text('Delivery')),
      body: RefreshIndicator(
        onRefresh: () async => ref.invalidate(deliveryProvider(orderId)),
        child: async.when(
          loading: () => const MiseLoading(label: 'Loading delivery'),
          error: (e, _) => ListView(children: [ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(deliveryProvider(orderId)))]),
          data: (d) => ListView(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xxl),
            children: [
              Semantics(liveRegion: true, header: true, child: Text(d.headline, style: MiseType.h1)),
              if (d.detail.isNotEmpty) ...[const SizedBox(height: MiseSpace.xs), Text(d.detail, style: MiseType.bodyLarge)],
              if (d.issue != null) ...[const SizedBox(height: MiseSpace.md), NoticeBanner(tone: NoticeTone.warning, message: d.issue!)],
              const SizedBox(height: MiseSpace.lg),
              if (d.serviceDate != null) FactRow(label: 'Delivery day', value: formatDate(d.serviceDate!)),
              if (d.window != null) FactRow(label: 'Delivery window', value: d.window!),
              if (d.serviceDate == null) const FactRow(label: 'Delivery day', value: 'Confirmed once your box is scheduled'),
              if (d.confirmationCode != null)
                FactRow(
                  label: 'Delivery code',
                  value: d.confirmationCode!,
                  trailing: Tooltip(message: d.codeExplanation, child: const Icon(Icons.info_outline, color: MiseColors.muted)),
                ),
              if (d.addressLines.isNotEmpty) FactRow(label: 'To', value: d.addressLines.join(', ')),
              const SizedBox(height: MiseSpace.lg),
              const Divider(color: MiseColors.foreground),
              const SizedBox(height: MiseSpace.md),
              MilestoneTimeline(milestones: milestonesFor(d)),
              Text('Updates come from our kitchen and delivery records. There is no live map.', style: MiseType.caption),
              const SizedBox(height: MiseSpace.lg),
              MiseButton.secondary(label: 'Ask about this delivery', icon: Icons.chat_bubble_outline, onPressed: () => context.push('/support/new')),
            ],
          ),
        ),
      ),
    );
  }
}
