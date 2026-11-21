import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../app/router.dart';
import '../../design_system/design_system.dart';

class _Page {
  const _Page(this.image, this.title, this.body, this.alt);

  final String image;
  final String title;
  final String body;
  final String alt;
}

// Photographs are licensed stock (docs/branding/IMAGE_SOURCES.md) and say so.
const _pages = [
  _Page(
    'assets/images/onboarding-meals.jpg',
    'Discover Caribbean meals worth cooking',
    'A weekly menu of dishes from home, chosen by our chefs.',
    'Jerk chicken with rice and peas and fried plantain on a white plate',
  ),
  _Page(
    'assets/images/onboarding-kit.jpg',
    'Everything measured, nothing wasted',
    'Your box arrives with the ingredients for each meal, portioned for your household.',
    'Fresh fruit and vegetables in a box',
  ),
  _Page(
    'assets/images/onboarding-cook.jpg',
    'Cook with confidence',
    'Clear steps written for a home kitchen. Dinner on the table, your way.',
    'A red dish served at home',
  ),
];

/// Three photographs, one idea each. Skippable; never asks for an account.
class OnboardingScreen extends ConsumerStatefulWidget {
  const OnboardingScreen({super.key});

  @override
  ConsumerState<OnboardingScreen> createState() => _OnboardingScreenState();
}

class _OnboardingScreenState extends ConsumerState<OnboardingScreen> {
  final _controller = PageController();
  int _index = 0;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _finish(String to) async {
    await ref.read(onboardingSeenProvider.notifier).markSeen();
    if (mounted) context.go(to);
  }

  @override
  Widget build(BuildContext context) {
    final last = _index == _pages.length - 1;
    return Scaffold(
      backgroundColor: MiseColors.background,
      body: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.sm, MiseSpace.xs, 0),
              child: Row(
                children: [
                  const Wordmark(size: 26),
                  const Spacer(),
                  if (!last) MiseButton.quiet(label: 'Skip', onPressed: () => _finish('/home')),
                ],
              ),
            ),
            Expanded(
              child: PageView.builder(
                controller: _controller,
                itemCount: _pages.length,
                onPageChanged: (i) => setState(() => _index = i),
                itemBuilder: (context, i) => _OnboardingPage(page: _pages[i], position: i + 1),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter),
              child: Semantics(
                label: 'Page ${_index + 1} of ${_pages.length}',
                child: Row(
                  children: [
                    for (var i = 0; i < _pages.length; i++)
                      AnimatedContainer(
                        duration: MiseMotion.standard,
                        curve: MiseMotion.settle,
                        margin: const EdgeInsets.only(right: 6),
                        height: 3,
                        width: i == _index ? 28 : 12,
                        color: i == _index ? MiseColors.accent : MiseColors.border,
                      ),
                  ],
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.lg, MiseSpace.gutter, MiseSpace.md),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  MiseButton(
                    label: last ? 'Get started' : 'Next',
                    onPressed: () {
                      if (last) {
                        _finish('/home');
                      } else {
                        final reduce = MediaQuery.disableAnimationsOf(context);
                        if (reduce) {
                          _controller.jumpToPage(_index + 1);
                        } else {
                          _controller.nextPage(duration: MiseMotion.standard, curve: MiseMotion.settle);
                        }
                      }
                    },
                  ),
                  const SizedBox(height: MiseSpace.xs),
                  MiseButton.secondary(label: 'I have an account — sign in', onPressed: () => _finish('/sign-in')),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _OnboardingPage extends StatelessWidget {
  const _OnboardingPage({required this.page, required this.position});

  final _Page page;
  final int position;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, c) {
        final imageHeight = (c.maxHeight * 0.58).clamp(180.0, 520.0);
        return SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.md, MiseSpace.gutter, MiseSpace.md),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(
                image: true,
                label: page.alt,
                child: ClipRRect(
                  borderRadius: const BorderRadius.all(MiseRadii.lg),
                  child: Image.asset(page.image, height: imageHeight, width: double.infinity, fit: BoxFit.cover, cacheWidth: 1200),
                ),
              ),
              const SizedBox(height: MiseSpace.xxs),
              Text('Stock photograph, not a Mise kit.', style: MiseType.caption),
              const SizedBox(height: MiseSpace.lg),
              Semantics(header: true, child: Text(page.title, style: MiseType.h1)),
              const SizedBox(height: MiseSpace.sm),
              Text(page.body, style: MiseType.bodyLarge.copyWith(color: MiseColors.muted)),
            ],
          ),
        );
      },
    );
  }
}
