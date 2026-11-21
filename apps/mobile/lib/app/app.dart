import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/notifications/deep_links.dart';
import '../design_system/design_system.dart';
import 'router.dart';

class MiseApp extends ConsumerWidget {
  const MiseApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);
    ref.watch(deepLinkListenerProvider);
    return MaterialApp.router(
      title: 'Mise',
      debugShowCheckedModeBanner: false,
      theme: buildMiseTheme(),
      // Light only until a dark theme is designed and approved (brief §9).
      themeMode: ThemeMode.light,
      routerConfig: router,
      builder: (context, child) {
        // Honour the system text size, but keep layouts usable at the largest
        // accessibility sizes.
        final mq = MediaQuery.of(context);
        final scaler = mq.textScaler.clamp(minScaleFactor: 0.85, maxScaleFactor: 2.0);
        return MediaQuery(data: mq.copyWith(textScaler: scaler), child: child ?? const SizedBox());
      },
    );
  }
}
