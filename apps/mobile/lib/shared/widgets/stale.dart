import 'package:flutter/material.dart';

import '../../design_system/design_system.dart';
import '../formatting.dart';

/// Says that what is on screen is a saved copy, when it is more than a
/// couple of minutes old (brief §42: stale information is labelled).
class StaleNotice extends StatelessWidget {
  const StaleNotice({super.key, required this.fetchedAt, this.threshold = const Duration(minutes: 2)});

  final DateTime fetchedAt;
  final Duration threshold;

  @override
  Widget build(BuildContext context) {
    if (DateTime.now().toUtc().difference(fetchedAt) < threshold) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.sm),
      child: NoticeBanner(
        tone: NoticeTone.warning,
        icon: Icons.cloud_off_outlined,
        message: "You're seeing a saved copy from ${formatInstant(fetchedAt)}. Pull down to refresh.",
      ),
    );
  }
}
