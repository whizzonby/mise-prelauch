import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_native_splash/flutter_native_splash.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/notifications/push.dart';
import '../core/storage/storage.dart';
import 'app.dart';
import 'environment.dart';
import 'providers.dart';
import 'router.dart';

/// Start-up: everything here is local and fast. Nothing waits for the
/// network, analytics or push; those start after the first frame.
Future<void> bootstrap() async {
  final binding = WidgetsFlutterBinding.ensureInitialized();
  FlutterNativeSplash.preserve(widgetsBinding: binding);

  Environment env;
  try {
    env = Environment.fromDefines();
  } catch (e) {
    FlutterNativeSplash.remove();
    runApp(_ConfigError(message: '$e'));
    return;
  }

  // Errors are reported without personal data; details stay in debug logs.
  FlutterError.onError = (details) {
    if (kDebugMode) FlutterError.presentError(details);
    reportError(details.exception, details.stack, env: env);
  };
  PlatformDispatcher.instance.onError = (error, stack) {
    reportError(error, stack, env: env);
    return true;
  };

  await initializeDateFormatting('en_US');
  final flags = Flags(SharedPreferencesAsync());
  final seen = await flags.getBool(Flags.onboardingSeen);

  final container = ProviderContainer(
    // No silent automatic retries: screens show the error and a Try again.
    retry: (_, _) => null,
    overrides: [
      environmentProvider.overrideWithValue(env),
      flagsProvider.overrideWithValue(flags),
      onboardingSeenProvider.overrideWith(() => OnboardingSeen(seen)),
    ],
  );
  await container.read(authControllerProvider.notifier).restore();

  runApp(UncontrolledProviderScope(container: container, child: const MiseApp()));
  FlutterNativeSplash.remove();

  // After the first frame: optional services that must never block start-up.
  unawaited(Future<void>.delayed(const Duration(milliseconds: 300), () => container.read(pushServiceProvider).start()));
}

class _ConfigError extends StatelessWidget {
  const _ConfigError({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: Scaffold(
        body: Padding(
          padding: const EdgeInsets.all(24),
          child: Center(child: Text('This build of Mise is misconfigured.\n\n$message', textAlign: TextAlign.center)),
        ),
      ),
    );
  }
}
