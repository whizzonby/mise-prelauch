import 'package:flutter/material.dart';

import '../../design_system/design_system.dart';

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class CookingScreen extends StatelessWidget {
  const CookingScreen({super.key, required this.orderId, required this.mealId});

  final String orderId;
  final String mealId;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}
