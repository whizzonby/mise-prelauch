import 'package:flutter/material.dart';

import '../../design_system/design_system.dart';

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class SupportScreen extends StatelessWidget {
  const SupportScreen({super.key});


  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class ConversationScreen extends StatelessWidget {
  const ConversationScreen({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class NewConversationScreen extends StatelessWidget {
  const NewConversationScreen({super.key});


  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class HelpScreen extends StatelessWidget {
  const HelpScreen({super.key, this.category});

  final String? category;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}

// Placeholder until its stage is built (docs/architecture/SPRINT_10_PLAN.md).
class HelpArticleScreen extends StatelessWidget {
  const HelpArticleScreen({super.key, required this.slug});

  final String slug;

  @override
  Widget build(BuildContext context) => Scaffold(appBar: AppBar(), body: const EmptyState(title: 'Not built yet'));
}
