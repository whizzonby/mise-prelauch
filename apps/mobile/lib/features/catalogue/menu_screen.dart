import 'package:flutter/material.dart';

import '../../design_system/design_system.dart';

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class MenuScreen extends StatelessWidget {
  const MenuScreen({super.key, this.week});

  final String? week;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}
