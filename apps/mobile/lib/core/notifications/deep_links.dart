import 'package:app_links/app_links.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/providers.dart';
import '../../app/router.dart';
import '../security/links.dart';

/// Receives app-scheme and (once a domain is approved) https links, validates
/// them with [SafeLinks], and routes. Flutter's built-in deep linking is
/// switched off in the Android manifest and iOS Info.plist so that every
/// link passes through here.
final deepLinkListenerProvider = Provider<void>((ref) {
  final env = ref.watch(environmentProvider);
  final links = AppLinks();
  final sub = links.uriLinkStream.listen((uri) {
    final path = SafeLinks.fromExternal(uri, appScheme: env.appScheme, host: env.deepLinkHost);
    ref.read(routerProvider).go(path ?? '/home');
  }, onError: (_) {});
  ref.onDispose(sub.cancel);
});

/// Maps a push notification's data (ids only) to an in-app path. Unknown
/// types and malformed ids go nowhere (null).
String? routeForNotification(Map<String, dynamic> data) {
  final type = data['type'];
  String? path;
  switch (type) {
    case 'chat.reply':
    case 'chat.resolved':
      path = '/support/conversations/${data['conversation_id']}';
    case 'payment.succeeded':
    case 'payment.failed':
    case 'renewal.requires_action':
    case 'subscription.renewal':
      path = '/subscription';
    case 'selection.reminder':
    case 'selection.deadline':
      path = data['cycle_id'] != null ? '/box/select/${data['cycle_id']}' : '/box';
    case 'order.confirmed':
      path = '/orders/${data['order_id']}';
    case 'delivery.update':
    case 'delivery.delivered':
      path = '/orders/${data['order_id']}/delivery';
    default:
      final link = data['link'];
      if (link is String) path = link;
  }
  return SafeLinks.internal(path);
}
