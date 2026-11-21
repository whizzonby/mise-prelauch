import 'dart:developer' as developer;

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../app/environment.dart';
import '../../app/providers.dart';

/// Push notifications (Firebase Cloud Messaging; APNs through Firebase on iOS).
///
/// Disabled unless the build was made with `--dart-define=MISE_PUSH=true`
/// *and* a Firebase configuration file. Neither exists yet (no Firebase
/// project), so this does nothing today — see docs/mobile/PUSH_NOTIFICATIONS.md.
/// The in-app notification centre works without it.
class PushService {
  PushService(this.ref);

  final Ref ref;
  bool _started = false;

  bool get available => ref.read(environmentProvider).pushEnabled;

  Future<void> start() async {
    if (_started || !available) return;
    _started = true;
    // Firebase initialisation and token registration are added in Stage 9.
  }
}

final pushServiceProvider = Provider<PushService>(PushService.new);

/// Reports an unexpected error without personal data: the error's type and
/// the top of the stack only, never messages that may contain user input.
/// Crash reporting (Crashlytics) is Blocked until a Firebase project exists.
void reportError(Object error, StackTrace? stack, {required Environment env}) {
  final top = stack?.toString().split('\n').take(6).join('\n') ?? '';
  developer.log('unhandled ${error.runtimeType}', name: 'mise', stackTrace: StackTrace.fromString(top));
}
