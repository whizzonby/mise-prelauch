import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../design_system/design_system.dart';
import '../features/support/support_state.dart';

/// Bottom navigation: Home, Menu, My Box, Support, Account. Each tab keeps its
/// own stack; tapping the current tab returns it to its first screen.
class AppShell extends ConsumerWidget {
  const AppShell({super.key, required this.shell});

  final StatefulNavigationShell shell;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final unread = ref.watch(supportUnreadProvider);
    return Scaffold(
      body: shell,
      bottomNavigationBar: DecoratedBox(
        decoration: const BoxDecoration(border: Border(top: BorderSide(color: MiseColors.border))),
        child: NavigationBar(
          selectedIndex: shell.currentIndex,
          onDestinationSelected: (i) {
            HapticFeedback.selectionClick();
            shell.goBranch(i, initialLocation: i == shell.currentIndex);
          },
          destinations: [
            const NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Home'),
            const NavigationDestination(icon: Icon(Icons.restaurant_menu_outlined), selectedIcon: Icon(Icons.restaurant_menu), label: 'Menu'),
            const NavigationDestination(icon: Icon(Icons.inventory_2_outlined), selectedIcon: Icon(Icons.inventory_2), label: 'My Box'),
            NavigationDestination(
              icon: CountBadge(count: unread, child: const Icon(Icons.chat_bubble_outline)),
              selectedIcon: CountBadge(count: unread, child: const Icon(Icons.chat_bubble)),
              label: 'Support',
              tooltip: unread > 0 ? 'Support, $unread unread' : 'Support',
            ),
            const NavigationDestination(icon: Icon(Icons.person_outline), selectedIcon: Icon(Icons.person), label: 'Account'),
          ],
        ),
      ),
    );
  }
}

class NotFoundScreen extends StatelessWidget {
  const NotFoundScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(),
      body: EmptyState(
        icon: Icons.explore_off_outlined,
        title: "That page isn't here",
        message: 'The link may be old, or it was not meant for this app.',
        actionLabel: 'Go home',
        onAction: () => context.go('/home'),
      ),
    );
  }
}
