import 'package:flutter/material.dart';

import '../../design_system/design_system.dart';

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class SubscribeScreen extends StatelessWidget {
  const SubscribeScreen({super.key, required this.planSlug});

  final String planSlug;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class CheckoutResultScreen extends StatelessWidget {
  const CheckoutResultScreen({super.key, required this.checkoutId});

  final String checkoutId;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}
