import 'dart:async';
import 'dart:developer' as developer;
import 'dart:io' show Platform;

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../app/environment.dart';
import '../../app/providers.dart';
import '../../app/router.dart';
import '../../features/notifications/notifications_api.dart';
import 'deep_links.dart';

/// Push notifications through Firebase Cloud Messaging (APNs via Firebase on
/// iOS). Off unless the build sets MISE_PUSH=true *and* ships a Firebase
/// configuration; neither exists yet (no Firebase project — Blocked).
///
/// - Permission is asked only when the customer turns push on.
/// - The token is sent to `POST /me/devices`; rotation re-registers it.
/// - A tapped notification routes through the same allow-list as links.
/// - The notification centre stays the authority; push only points at it.
class PushService {
  PushService(this.ref);

  final Ref ref;
  bool _started = false;
  bool _firebaseReady = false;
  bool registered = false;
  final List<StreamSubscription<Object?>> _subs = [];

  bool get available => ref.read(environmentProvider).pushEnabled && _firebaseReady;

  Future<void> start() async {
    if (_started || !ref.read(environmentProvider).pushEnabled) return;
    _started = true;
    try {
      await Firebase.initializeApp();
      _firebaseReady = true;
    } catch (e) {
      developer.log('push unavailable: ${e.runtimeType}', name: 'push');
      return;
    }
    final m = FirebaseMessaging.instance;
    _subs.add(FirebaseMessaging.onMessageOpenedApp.listen(_open));
    _subs.add(FirebaseMessaging.onMessage.listen((_) => ref.invalidate(notificationsProvider)));
    // The server sends with FCM HTTP v1 to registration tokens. firebase_messaging 17
    // deprecates these calls for a new installation-id ("direct-send") model that
    // needs a different server API; migrating both together is tracked in
    // docs/mobile/PUSH_NOTIFICATIONS.md.
    // ignore: deprecated_member_use
    _subs.add(m.onTokenRefresh.listen((t) => unawaited(_register(t))));
    final initial = await m.getInitialMessage();
    if (initial != null) _open(initial);
    // Already allowed earlier: keep the registration fresh.
    final settings = await m.getNotificationSettings();
    if (settings.authorizationStatus == AuthorizationStatus.authorized && ref.read(isSignedInProvider)) {
      // ignore: deprecated_member_use
      final t = await m.getToken();
      if (t != null) await _register(t);
    }
  }

  /// Asks the system for permission, then registers this installation.
  Future<bool> enable() async {
    if (!available) return false;
    final s = await FirebaseMessaging.instance.requestPermission(alert: true, badge: true, sound: true);
    if (s.authorizationStatus != AuthorizationStatus.authorized && s.authorizationStatus != AuthorizationStatus.provisional) return false;
    // ignore: deprecated_member_use
    final t = await FirebaseMessaging.instance.getToken();
    if (t == null) return false;
    await _register(t);
    return registered;
  }

  Future<void> _register(String token) async {
    if (!ref.read(isSignedInProvider)) return;
    try {
      final info = await PackageInfo.fromPlatform();
      await ref.read(apiClientProvider).post<void>('/me/devices', (_) {}, body: {
        'installation_id': await ref.read(sessionManagerProvider).installationId(),
        'platform': Platform.isIOS ? 'ios' : 'android',
        'push_token': token,
        'app_version': '${info.version}+${info.buildNumber}',
      });
      registered = true;
    } catch (_) {
      // Retried on the next start or token refresh.
    }
  }

  /// Called before sign-out (the server also forgets devices of a session
  /// that ends) and when the customer turns push off.
  Future<void> forgetDevice() async {
    registered = false;
    if (!_firebaseReady) return;
    try {
      final id = await ref.read(sessionManagerProvider).installationId();
      await ref.read(apiClientProvider).delete('/me/devices/$id');
      // ignore: deprecated_member_use
      await FirebaseMessaging.instance.deleteToken();
    } catch (_) {}
  }

  void _open(RemoteMessage m) {
    final path = routeForNotification(Map<String, dynamic>.from(m.data));
    ref.read(routerProvider).go(path ?? '/notifications');
  }
}

final pushServiceProvider = Provider<PushService>((ref) {
  final s = PushService(ref);
  ref.onDispose(() {
    for (final sub in s._subs) {
      unawaited(sub.cancel());
    }
  });
  return s;
});

/// Reports an unexpected error without personal data: the error's type and
/// the top of the stack only, never messages that may contain user input.
/// Crash reporting (Crashlytics) is Blocked until a Firebase project exists.
void reportError(Object error, StackTrace? stack, {required Environment env}) {
  final top = stack?.toString().split('\n').take(6).join('\n') ?? '';
  developer.log('unhandled ${error.runtimeType}', name: 'mise', stackTrace: StackTrace.fromString(top));
}
