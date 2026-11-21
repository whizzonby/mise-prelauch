import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/providers.dart';
import '../../core/api/api_client.dart';

/// One entry of the in-app notification centre (`GET /me/notifications`).
class AppNotification {
  const AppNotification({required this.id, required this.type, required this.title, required this.body, required this.link, required this.readAt, required this.createdAt});

  factory AppNotification.fromJson(Json j) => AppNotification(
    id: str(j, 'id'),
    type: str(j, 'type'),
    title: str(j, 'title'),
    body: str(j, 'body'),
    link: strOrNull(j, 'link'),
    readAt: time(j, 'read_at'),
    createdAt: time(j, 'created_at') ?? DateTime.now().toUtc(),
  );

  final String id;
  final String type;
  final String title;
  final String body;

  /// In-app path, validated before use.
  final String? link;
  final DateTime? readAt;
  final DateTime createdAt;

  bool get unread => readAt == null;
}

final notificationsProvider = FutureProvider<List<AppNotification>>((ref) async {
  if (!ref.watch(isSignedInProvider)) return const [];
  return ref.watch(apiClientProvider).get('/me/notifications', (d) => itemsOf(d, AppNotification.fromJson));
});

final unreadNotificationsProvider = Provider<int>((ref) {
  final list = ref.watch(notificationsProvider).value ?? const [];
  return list.where((n) => n.unread).length;
});
