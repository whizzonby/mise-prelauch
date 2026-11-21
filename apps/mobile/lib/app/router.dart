import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/security/links.dart';
import '../features/account/account_screens.dart';
import '../features/authentication/auth_screens.dart';
import '../features/catalogue/menu_screen.dart';
import '../features/checkout/checkout_screens.dart';
import '../features/cooking/cooking_screen.dart';
import '../features/delivery/delivery_screen.dart';
import '../features/home/home_screen.dart';
import '../features/meal_details/meal_detail_screen.dart';
import '../features/meal_selection/selection_screen.dart';
import '../features/my_box/box_screen.dart';
import '../features/notifications/notifications_screen.dart';
import '../features/onboarding/onboarding_screen.dart';
import '../features/orders/order_screens.dart';
import '../features/plans/plans_screen.dart';
import '../features/subscriptions/subscription_screen.dart';
import '../features/support/support_screens.dart';
import 'providers.dart';
import 'shell.dart';

/// Whether onboarding has been seen; set in bootstrap, updated by onboarding.
class OnboardingSeen extends Notifier<bool> {
  OnboardingSeen([this._initial = false]);

  final bool _initial;

  @override
  bool build() => _initial;

  Future<void> markSeen() async {
    state = true;
    try {
      await ref.read(flagsProvider).setBool('onboarding_seen_v1', true);
    } catch (_) {}
  }
}

final onboardingSeenProvider = NotifierProvider<OnboardingSeen, bool>(OnboardingSeen.new);

final _rootKey = GlobalKey<NavigatorState>(debugLabel: 'root');

/// Routes that need a signed-in customer. Tabs themselves never redirect:
/// My Box and Account explain what signing in gives instead.
bool _needsSession(String path) =>
    path.startsWith('/box/') ||
    path.startsWith('/orders') ||
    path.startsWith('/checkout') ||
    path == '/subscription' ||
    path == '/notifications' ||
    path.startsWith('/support/conversations') ||
    (path.startsWith('/account/') && path != '/account/legal');

final routerProvider = Provider<GoRouter>((ref) {
  final notifier = ValueNotifier<int>(0);
  ref.listen(authControllerProvider, (_, _) => notifier.value++);
  ref.listen(onboardingSeenProvider, (_, _) => notifier.value++);
  ref.onDispose(notifier.dispose);

  return GoRouter(
    navigatorKey: _rootKey,
    initialLocation: '/home',
    refreshListenable: notifier,
    debugLogDiagnostics: false,
    redirect: (context, state) {
      final auth = ref.read(authControllerProvider);
      final path = state.uri.path;
      if (auth is AuthUnknown) return null;
      const authPaths = {'/sign-in', '/register', '/check-email', '/forgot-password', '/reset-password', '/verify-email'};
      if (!ref.read(onboardingSeenProvider) && path != '/onboarding' && !authPaths.contains(path)) {
        return '/onboarding';
      }
      final signedIn = auth is SignedIn;
      if (!signedIn && _needsSession(path)) {
        return Uri(path: '/sign-in', queryParameters: {'from': state.uri.toString()}).toString();
      }
      if (signedIn && (path == '/sign-in' || path == '/register')) {
        return SafeLinks.internal(state.uri.queryParameters['from']) ?? '/home';
      }
      return null;
    },
    errorBuilder: (context, state) => const NotFoundScreen(),
    routes: [
      GoRoute(path: '/', redirect: (_, _) => '/home'),
      GoRoute(path: '/onboarding', builder: (_, _) => const OnboardingScreen()),
      GoRoute(path: '/sign-in', builder: (_, s) => SignInScreen(from: SafeLinks.internal(s.uri.queryParameters['from']))),
      GoRoute(path: '/register', builder: (_, s) => RegisterScreen(from: SafeLinks.internal(s.uri.queryParameters['from']))),
      GoRoute(path: '/check-email', builder: (_, s) => CheckEmailScreen(email: s.uri.queryParameters['email'] ?? '')),
      GoRoute(path: '/forgot-password', builder: (_, _) => const ForgotPasswordScreen()),
      GoRoute(path: '/reset-password', builder: (_, s) => ResetPasswordScreen(token: s.uri.queryParameters['token'] ?? '')),
      GoRoute(path: '/verify-email', builder: (_, s) => VerifyEmailScreen(token: s.uri.queryParameters['token'] ?? '')),
      StatefulShellRoute.indexedStack(
        builder: (context, state, shell) => AppShell(shell: shell),
        branches: [
          StatefulShellBranch(routes: [GoRoute(path: '/home', builder: (_, _) => const HomeScreen())]),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/menu',
                builder: (_, _) => const MenuScreen(),
                routes: [
                  GoRoute(path: 'meal/:slug', builder: (_, s) => MealDetailScreen(slug: s.pathParameters['slug']!)),
                  GoRoute(path: 'week/:week', builder: (_, s) => MenuScreen(week: s.pathParameters['week'])),
                ],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/box',
                builder: (_, _) => const BoxScreen(),
                routes: [GoRoute(path: 'select/:cycleId', builder: (_, s) => MealSelectionScreen(cycleId: s.pathParameters['cycleId']!))],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/support',
                builder: (_, _) => const SupportScreen(),
                routes: [
                  GoRoute(path: 'conversations/:id', builder: (_, s) => ConversationScreen(id: s.pathParameters['id']!)),
                  GoRoute(path: 'new', builder: (_, _) => const NewConversationScreen()),
                  GoRoute(
                    path: 'help',
                    builder: (_, s) => HelpScreen(category: s.uri.queryParameters['category']),
                    routes: [GoRoute(path: ':slug', builder: (_, s) => HelpArticleScreen(slug: s.pathParameters['slug']!))],
                  ),
                ],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: '/account',
                builder: (_, _) => const AccountScreen(),
                routes: [
                  GoRoute(path: 'profile', builder: (_, _) => const ProfileScreen()),
                  GoRoute(path: 'addresses', builder: (_, _) => const AddressesScreen()),
                  GoRoute(path: 'preferences', builder: (_, _) => const PreferencesScreen()),
                  GoRoute(path: 'notifications', builder: (_, _) => const NotificationSettingsScreen()),
                  GoRoute(path: 'devices', builder: (_, _) => const DevicesScreen()),
                  GoRoute(path: 'favorites', builder: (_, _) => const FavoritesScreen()),
                  GoRoute(path: 'privacy', builder: (_, _) => const PrivacyScreen()),
                  GoRoute(path: 'legal', builder: (_, _) => const LegalScreen()),
                ],
              ),
            ],
          ),
        ],
      ),
      // Full-screen flows above the tab bar.
      GoRoute(parentNavigatorKey: _rootKey, path: '/plans', builder: (_, _) => const PlansScreen()),
      GoRoute(parentNavigatorKey: _rootKey, path: '/plans/:slug', builder: (_, s) => SubscribeScreen(planSlug: s.pathParameters['slug']!)),
      GoRoute(parentNavigatorKey: _rootKey, path: '/subscription', builder: (_, _) => const SubscriptionScreen()),
      GoRoute(
        parentNavigatorKey: _rootKey,
        path: '/checkout/pay/:billingCycleId',
        builder: (_, s) => PayWeekScreen(billingCycleId: s.pathParameters['billingCycleId']!),
      ),
      GoRoute(
        parentNavigatorKey: _rootKey,
        path: '/checkout/result/:id',
        builder: (_, s) => CheckoutResultScreen(checkoutId: s.pathParameters['id']!),
      ),
      GoRoute(parentNavigatorKey: _rootKey, path: '/orders', builder: (_, _) => const OrdersScreen()),
      GoRoute(parentNavigatorKey: _rootKey, path: '/orders/:id', builder: (_, s) => OrderDetailScreen(id: s.pathParameters['id']!)),
      GoRoute(
        parentNavigatorKey: _rootKey,
        path: '/orders/:id/delivery',
        builder: (_, s) => DeliveryScreen(orderId: s.pathParameters['id']!),
      ),
      GoRoute(
        parentNavigatorKey: _rootKey,
        path: '/orders/:id/cook/:mealId',
        builder: (_, s) => CookingScreen(orderId: s.pathParameters['id']!, mealId: s.pathParameters['mealId']!),
      ),
      GoRoute(parentNavigatorKey: _rootKey, path: '/notifications', builder: (_, _) => const NotificationsScreen()),
    ],
  );
});
