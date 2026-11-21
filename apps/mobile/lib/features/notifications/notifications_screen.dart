import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/providers.dart';
import '../../core/errors/app_error.dart';
import '../../core/security/links.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import 'notifications_api.dart';

/// The notification centre. Authoritative: push messages may never arrive,
/// so everything is here.
class NotificationsScreen extends ConsumerWidget {
  const NotificationsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(notificationsProvider);
    final unread = ref.watch(unreadNotificationsProvider);
    final api = ref.read(apiClientProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Notifications'),
        actions: [
          if (unread > 0)
            TextButton(
              onPressed: () async {
                await api.post<void>('/me/notifications/read-all', (_) {});
                ref.invalidate(notificationsProvider);
              },
              child: const Text('Mark all read'),
            ),
          IconButton(tooltip: 'Notification settings', icon: const Icon(Icons.settings_outlined), onPressed: () => context.push('/account/notifications')),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () async => ref.invalidate(notificationsProvider),
        child: async.when(
          loading: () => const MiseLoading(),
          error: (e, _) => ListView(children: [ErrorState(message: AppError.from(e).message, onRetry: () => ref.invalidate(notificationsProvider))]),
          data: (list) => list.isEmpty
              ? ListView(children: const [EmptyState(icon: Icons.notifications_none, title: 'Nothing new', message: 'Updates about payments, your meals, deliveries and replies appear here.')])
              : ListView.separated(
                  itemCount: list.length,
                  separatorBuilder: (_, _) => const Divider(indent: MiseSpace.gutter, endIndent: MiseSpace.gutter),
                  itemBuilder: (context, i) {
                    final n = list[i];
                    return Semantics(
                      button: true,
                      label: '${n.unread ? 'Unread. ' : ''}${n.title}. ${n.body}',
                      excludeSemantics: true,
                      child: InkWell(
                        onTap: () async {
                          if (n.unread) {
                            try {
                              await api.post<void>('/me/notifications/${n.id}/read', (_) {});
                            } catch (_) {}
                            ref.invalidate(notificationsProvider);
                          }
                          final to = SafeLinks.internal(n.link);
                          if (to != null && context.mounted) await context.push<void>(to);
                        },
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter, vertical: MiseSpace.md),
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Container(
                                margin: const EdgeInsets.only(top: 7, right: MiseSpace.sm),
                                width: 8,
                                height: 8,
                                decoration: BoxDecoration(shape: BoxShape.circle, color: n.unread ? MiseColors.accent : Colors.transparent),
                              ),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(n.title, style: MiseType.body.copyWith(fontWeight: n.unread ? FontWeight.w600 : FontWeight.w400)),
                                    if (n.body.isNotEmpty) Text(n.body, style: MiseType.bodySmall),
                                    const SizedBox(height: 4),
                                    Text(formatInstant(n.createdAt), style: MiseType.caption),
                                  ],
                                ),
                              ),
                            ],
                          ),
                        ),
                      ),
                    );
                  },
                ),
        ),
      ),
    );
  }
}
