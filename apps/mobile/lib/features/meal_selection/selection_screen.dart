import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/errors/app_error.dart';
import '../../design_system/design_system.dart';
import '../../shared/formatting.dart';
import '../../shared/media.dart';
import '../../shared/models/catalogue.dart';
import '../../shared/models/commerce.dart';
import '../subscriptions/subscription_api.dart';
import 'selection_state.dart';

/// Choosing the meals for one week. The server checks the menu, allowance,
/// per-meal limits, the cutoff and concurrent edits; a choice is shown as
/// saved only after it accepts it.
class MealSelectionScreen extends ConsumerStatefulWidget {
  const MealSelectionScreen({super.key, required this.cycleId});

  final String cycleId;

  @override
  ConsumerState<MealSelectionScreen> createState() => _MealSelectionScreenState();
}

class _MealSelectionScreenState extends ConsumerState<MealSelectionScreen> {
  SelectionDraft? _state;
  AppError? _loadError;
  bool _busy = false;
  String? _notice;
  NoticeTone _noticeTone = NoticeTone.info;

  CommerceApi get _api => ref.read(commerceApiProvider);

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loadError = null);
    try {
      final page = await _api.selection(widget.cycleId);
      if (mounted) setState(() => _state = SelectionDraft(page));
    } on AppError catch (e) {
      if (mounted) setState(() => _loadError = e);
    }
  }

  void _say(String message, NoticeTone tone) => setState(() {
    _notice = message;
    _noticeTone = tone;
  });

  Future<void> _submit({required bool confirm}) async {
    final s = _state;
    if (s == null || _busy) return;
    setState(() {
      _busy = true;
      _notice = null;
    });
    try {
      final page = confirm
          ? await _api.finalizeSelection(widget.cycleId, s.draft, s.server.version)
          : await _api.saveSelection(widget.cycleId, s.draft, s.server.version);
      setState(() => _state = s.accepted(page));
      ref.invalidate(mySubscriptionProvider);
      ref.invalidate(weeksProvider);
      _say(
        confirm ? 'Your meals for ${page.cycle.weekLabel} are confirmed. You can change them until the cutoff.' : 'Saved. Confirm when you have chosen them all.',
        NoticeTone.success,
      );
    } on AppError catch (e) {
      switch (e.code) {
        case 'version_conflict':
          await _reloadAfter('Your choices were changed on another device. We have loaded the latest ones; please check them.');
        case 'selection_closed':
          await _reloadAfter('Choices for this week have closed. What was saved before the cutoff stands.');
        default:
          if (e.isNetwork) {
            _say('Not saved: you seem to be offline. Your choices are still here; try again when you are back online.', NoticeTone.error);
          } else {
            _say(e.message, NoticeTone.error);
          }
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _reloadAfter(String message) async {
    try {
      final page = await _api.selection(widget.cycleId);
      setState(() => _state = _state!.reset(page));
    } on AppError {
      // Keep the screen; the notice explains.
    }
    _say(message, NoticeTone.warning);
  }

  Future<bool> _confirmLeave() async {
    if (_state == null || !_state!.dirty || !_state!.editable) return true;
    return confirmAction(
      context,
      title: 'Leave without saving?',
      message: 'Your changes have not been sent to Mise yet.',
      confirmLabel: 'Leave',
      cancelLabel: 'Stay and save',
      destructive: true,
    );
  }

  @override
  Widget build(BuildContext context) {
    final s = _state;
    return PopScope(
      canPop: s == null || !s.dirty || !s.editable,
      onPopInvokedWithResult: (didPop, _) async {
        if (didPop) return;
        if (await _confirmLeave() && context.mounted) context.pop();
      },
      child: Scaffold(
        appBar: AppBar(title: Text(s?.server.cycle.weekLabel ?? 'Choose meals')),
        body: s == null
            ? (_loadError != null ? ErrorState(message: _loadError!.message, onRetry: _load) : const MiseLoading(label: 'Loading your week'))
            : RefreshIndicator(onRefresh: _load, child: _body(s)),
        bottomNavigationBar: s == null || !s.editable
            ? null
            : BottomActionBar(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    SelectionProgress(chosen: s.chosen, required: s.allowance),
                    const SizedBox(height: MiseSpace.sm),
                    MiseButton(
                      label: s.complete ? (s.server.confirmed && !s.dirty ? 'Confirmed' : 'Confirm my meals') : 'Choose ${plural(s.remaining, 'more meal')}',
                      busy: _busy,
                      onPressed: s.complete && (s.dirty || !s.server.confirmed) ? () => _submit(confirm: true) : null,
                    ),
                    if (s.dirty && !s.complete)
                      MiseButton.quiet(label: 'Save for later', expand: true, onPressed: _busy ? null : () => _submit(confirm: false)),
                  ],
                ),
              ),
      ),
    );
  }

  Widget _body(SelectionDraft s) {
    final page = s.server;
    final media = ref.watch(mediaUrlProvider);
    final cutoff = page.cycle.cutoffAt;
    return ListView(
      padding: const EdgeInsets.only(bottom: MiseSpace.xxl),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.xs, MiseSpace.gutter, MiseSpace.md),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('${page.cycle.planName} · ${plural(page.allowance, 'meal')}', style: MiseType.bodySmall),
              if (cutoff != null) ...[
                const SizedBox(height: 4),
                Text(
                  page.open ? 'Choose by ${formatInstant(cutoff)} · ${timeLeft(cutoff, page.serverTime)}' : 'Closed on ${formatInstant(cutoff)}',
                  style: MiseType.label.copyWith(color: page.open ? MiseColors.warning : MiseColors.muted),
                ),
              ],
            ],
          ),
        ),
        if (_notice != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
            child: NoticeBanner(message: _notice!, tone: _noticeTone),
          ),
        if (s.dirty && s.editable)
          const Padding(
            padding: EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
            child: NoticeBanner(message: 'Changes not saved yet.', tone: NoticeTone.warning, icon: Icons.edit_note),
          ),
        if (!page.open)
          const Padding(
            padding: EdgeInsets.fromLTRB(MiseSpace.gutter, 0, MiseSpace.gutter, MiseSpace.md),
            child: NoticeBanner(message: 'This week can no longer be changed.', tone: NoticeTone.info, icon: Icons.lock_outline),
          ),
        if (page.menu == null)
          const EmptyState(
            icon: Icons.event_note_outlined,
            title: 'The menu for this week is not out yet',
            message: 'You can choose as soon as the kitchen publishes it.',
          )
        else
          for (final meal in page.menu!.meals)
            _SelectableMeal(
              meal: meal,
              note: page.notes[meal.id],
              quantity: s.quantity(meal.id),
              max: s.maxFor(meal.id),
              editable: s.editable && !_busy,
              imageUrl: media(meal.image?.src),
              onChanged: (q) => setState(() => _state = s.set(meal.id, q)),
            ),
        if (page.crossContactNotice.isNotEmpty)
          Padding(padding: const EdgeInsets.fromLTRB(MiseSpace.gutter, MiseSpace.md, MiseSpace.gutter, 0), child: Text(page.crossContactNotice, style: MiseType.caption)),
      ],
    );
  }
}

class _SelectableMeal extends StatelessWidget {
  const _SelectableMeal({
    required this.meal,
    required this.note,
    required this.quantity,
    required this.max,
    required this.editable,
    required this.imageUrl,
    required this.onChanged,
  });

  final Meal meal;
  final MealNote? note;
  final int quantity;
  final int max;
  final bool editable;
  final String? imageUrl;
  final ValueChanged<int> onChanged;

  @override
  Widget build(BuildContext context) {
    final warn = note?.containsYourAllergens ?? const [];
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: MiseSpace.gutter, vertical: MiseSpace.md),
      decoration: BoxDecoration(
        color: quantity > 0 ? MiseColors.surface : null,
        border: const Border(bottom: BorderSide(color: MiseColors.border)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          GestureDetector(
            onTap: () => context.push('/menu/meal/${meal.slug}'),
            child: SizedBox(width: 96, child: MiseNetworkImage(url: imageUrl, alt: meal.image?.alt ?? '', aspectRatio: 1, radius: MiseRadii.md)),
          ),
          const SizedBox(width: MiseSpace.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(meal.name, style: MiseType.h4.copyWith(fontFamily: MiseType.display, fontWeight: FontWeight.w400)),
                Text(meal.facts, style: MiseType.caption),
                if (warn.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  Text('Contains your allergens: ${warn.map((t) => t.label).join(', ')}', style: MiseType.caption.copyWith(color: MiseColors.error, fontWeight: FontWeight.w600)),
                ] else if (note?.allergensUnconfirmed == true) ...[
                  const SizedBox(height: 4),
                  Text('Allergens not confirmed yet', style: MiseType.caption.copyWith(color: MiseColors.warning)),
                ],
                if ((note?.matchesPreferences ?? const []).isNotEmpty)
                  Text('Suits: ${note!.matchesPreferences.map((t) => t.label).join(', ')}', style: MiseType.caption.copyWith(color: MiseColors.secondary)),
                const SizedBox(height: MiseSpace.xs),
                QuantitySelector(value: quantity, max: max < quantity ? quantity : max, itemLabel: meal.name, enabled: editable, onChanged: onChanged),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
