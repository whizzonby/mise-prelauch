import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:wakelock_plus/wakelock_plus.dart';

import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/models/orders.dart';
import '../../shared/widgets/stale.dart';
import '../orders/orders_api.dart';

/// Cooking mode for a meal in a box the customer bought. Only the approved,
/// published customer instructions are shown — never generated text.
class CookingScreen extends ConsumerWidget {
  const CookingScreen({super.key, required this.orderId, required this.mealId});

  final String orderId;
  final String mealId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(recipeProvider((orderId, mealId)));
    return async.when(
      loading: () => Scaffold(appBar: AppBar(), body: const MiseLoading(label: 'Loading recipe')),
      error: (e, _) {
        final err = AppError.from(e);
        return Scaffold(
          appBar: AppBar(),
          body: switch (err.code) {
            'recipe_unavailable' => const EmptyState(
              icon: Icons.menu_book_outlined,
              title: 'Cooking steps are not published yet',
              message: 'The recipe card in your box has everything you need.',
            ),
            'recipe_not_available_yet' => const EmptyState(
              icon: Icons.lock_clock_outlined,
              title: 'Not yet',
              message: 'Cooking steps open once your box is confirmed.',
            ),
            _ => ErrorState(message: err.message, onRetry: () => ref.invalidate(recipeProvider((orderId, mealId)))),
          },
        );
      },
      data: (cached) => _CookingView(recipe: cached.value, fetchedAt: cached.fetchedAt),
    );
  }
}

class _CookingView extends StatefulWidget {
  const _CookingView({required this.recipe, required this.fetchedAt});

  final CustomerRecipe recipe;
  final DateTime fetchedAt;

  @override
  State<_CookingView> createState() => _CookingViewState();
}

class _CookingViewState extends State<_CookingView> {
  bool _cooking = false;
  int _step = 0;
  final Set<int> _checked = {};
  final _pages = PageController();

  @override
  void dispose() {
    WakelockPlus.disable();
    _pages.dispose();
    super.dispose();
  }

  Future<void> _setCooking(bool on) async {
    setState(() => _cooking = on);
    try {
      // Keep the screen on only while the customer has cooking mode on.
      on ? await WakelockPlus.enable() : await WakelockPlus.disable();
    } catch (_) {}
  }

  void _go(int i) {
    final steps = widget.recipe.steps.length;
    if (i < 0 || i >= steps) return;
    HapticFeedback.selectionClick();
    if (MediaQuery.disableAnimationsOf(context)) {
      _pages.jumpToPage(i);
    } else {
      _pages.animateToPage(i, duration: MiseMotion.standard, curve: MiseMotion.settle);
    }
  }

  @override
  Widget build(BuildContext context) {
    final r = widget.recipe;
    if (_cooking) return _stepsView(r);
    return Scaffold(
      appBar: AppBar(title: Text(r.mealName)),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.xxl),
        children: [
          StaleNotice(fetchedAt: widget.fetchedAt, threshold: const Duration(days: 1)),
          Semantics(header: true, child: Text(r.mealName, style: MiseType.h1)),
          const SizedBox(height: MiseSpace.md),
          Row(
            children: [
              if (r.cookMinutes != null) Expanded(child: FactRow(label: 'Cooking time', value: '${r.cookMinutes} min')),
              if (r.servings > 0) Expanded(child: FactRow(label: 'Serves', value: '${r.servings}${r.servingSize.isNotEmpty ? ' · ${r.servingSize}' : ''}')),
            ],
          ),
          if (r.allergens.isNotEmpty || r.mayContain.isNotEmpty) ...[
            const SizedBox(height: MiseSpace.sm),
            NoticeBanner(
              tone: NoticeTone.warning,
              message: [
                if (r.allergens.isNotEmpty) 'Contains ${r.allergens.join(', ')}.',
                if (r.mayContain.isNotEmpty) 'May contain ${r.mayContain.join(', ')}.',
                if (r.crossContact.isNotEmpty) r.crossContact,
              ].join(' '),
            ),
          ],
          if (r.ingredients.isNotEmpty) ...[
            const SizedBox(height: MiseSpace.xl),
            const SectionHeader(title: 'Check your box', padding: EdgeInsets.zero),
            const SizedBox(height: MiseSpace.xs),
            for (var i = 0; i < r.ingredients.length; i++)
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                controlAffinity: ListTileControlAffinity.leading,
                value: _checked.contains(i),
                onChanged: (v) => setState(() => v == true ? _checked.add(i) : _checked.remove(i)),
                title: Text(r.ingredients[i], style: MiseType.body),
              ),
          ],
          const SizedBox(height: MiseSpace.xl),
          const SectionHeader(title: 'Steps', padding: EdgeInsets.zero),
          const SizedBox(height: MiseSpace.md),
          for (var i = 0; i < r.steps.length; i++) ...[
            RecipeStep(number: i + 1, total: r.steps.length, body: r.steps[i]),
            const SizedBox(height: MiseSpace.lg),
          ],
          if (r.storage.isNotEmpty) ...[const SectionHeader(title: 'Storage', padding: EdgeInsets.zero), const SizedBox(height: MiseSpace.xs), Text(r.storage, style: MiseType.body)],
        ],
      ),
      bottomNavigationBar: BottomActionBar(
        child: MiseButton(label: 'Start cooking mode', icon: Icons.soup_kitchen_outlined, onPressed: () => _setCooking(true)),
      ),
    );
  }

  Widget _stepsView(CustomerRecipe r) {
    final total = r.steps.length;
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _setCooking(false);
      },
      child: Scaffold(
        backgroundColor: MiseColors.surface,
        appBar: AppBar(
          backgroundColor: MiseColors.surface,
          leading: IconButton(tooltip: 'Leave cooking mode', icon: const Icon(Icons.close), onPressed: () => _setCooking(false)),
          title: Text('Step ${_step + 1} of $total'),
          actions: [IconButton(tooltip: 'Timer', icon: const Icon(Icons.timer_outlined), onPressed: () => _openTimer(context))],
        ),
        body: Column(
          children: [
            LinearProgressIndicator(value: (_step + 1) / total, minHeight: 3),
            Expanded(
              child: PageView.builder(
                controller: _pages,
                itemCount: total,
                onPageChanged: (i) => setState(() => _step = i),
                itemBuilder: (context, i) => SingleChildScrollView(
                  padding: const EdgeInsets.all(MiseSpace.gutter),
                  child: RecipeStep(number: i + 1, total: total, body: r.steps[i], large: true),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
              child: Text('The screen stays on while cooking mode is open.', style: MiseType.caption),
            ),
          ],
        ),
        bottomNavigationBar: BottomActionBar(
          child: Row(
            children: [
              Expanded(child: MiseButton.secondary(label: 'Back', onPressed: _step == 0 ? null : () => _go(_step - 1))),
              const SizedBox(width: MiseSpace.sm),
              Expanded(
                child: _step == total - 1
                    ? MiseButton(label: 'Done', onPressed: () => _setCooking(false))
                    : MiseButton(label: 'Next step', onPressed: () => _go(_step + 1)),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _openTimer(BuildContext context) =>
      showMiseSheet<void>(context, title: 'Timer', builder: (_) => const _KitchenTimer());
}

/// A plain countdown the customer sets. It does not read times out of the
/// recipe text, so it never suggests a cooking time of its own.
class _KitchenTimer extends StatefulWidget {
  const _KitchenTimer();

  @override
  State<_KitchenTimer> createState() => _KitchenTimerState();
}

class _KitchenTimerState extends State<_KitchenTimer> {
  int _minutes = 5;
  Duration? _left;
  Timer? _tick;

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  void _start() {
    _tick?.cancel();
    setState(() => _left = Duration(minutes: _minutes));
    _tick = Timer.periodic(const Duration(seconds: 1), (t) {
      final next = _left! - const Duration(seconds: 1);
      if (next <= Duration.zero) {
        t.cancel();
        HapticFeedback.heavyImpact();
        SystemSound.play(SystemSoundType.alert);
        setState(() => _left = Duration.zero);
      } else {
        setState(() => _left = next);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final left = _left;
    String two(int n) => n.toString().padLeft(2, '0');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (left == null) ...[
          Center(child: QuantitySelector(value: _minutes, min: 1, max: 180, itemLabel: 'Minutes', onChanged: (v) => setState(() => _minutes = v))),
          const SizedBox(height: MiseSpace.xs),
          Center(child: Text('minutes', style: MiseType.caption)),
          const SizedBox(height: MiseSpace.lg),
          MiseButton(label: 'Start timer', onPressed: _start),
        ] else ...[
          Semantics(
            liveRegion: left == Duration.zero,
            label: left == Duration.zero ? 'Time is up' : '${left.inMinutes} minutes ${left.inSeconds % 60} seconds left',
            child: Text(
              left == Duration.zero ? 'Time is up' : '${two(left.inMinutes)}:${two(left.inSeconds % 60)}',
              textAlign: TextAlign.center,
              style: MiseType.figures(MiseType.displayLarge.copyWith(fontSize: 64)),
            ),
          ),
          const SizedBox(height: MiseSpace.lg),
          MiseButton.secondary(
            label: left == Duration.zero ? 'Set another' : 'Stop',
            onPressed: () {
              _tick?.cancel();
              setState(() => _left = null);
            },
          ),
          Text('Keep the app open: the timer stops if Mise is closed.', style: MiseType.caption, textAlign: TextAlign.center),
        ],
      ],
    );
  }
}
