import 'package:flutter/material.dart';

import '../../design_system/design_system.dart';

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class OrdersScreen extends StatelessWidget {
  const OrdersScreen({super.key});


  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class OrderDetailScreen extends StatelessWidget {
  const OrderDetailScreen({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}
