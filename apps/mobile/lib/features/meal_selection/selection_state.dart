import '../../shared/models/commerce.dart';

/// The customer's draft for one week, beside what the server last accepted.
///
/// The draft is only a proposal: nothing is shown as saved or confirmed
/// until the server answers. Limits here (allowance, per-meal maximum, open
/// window) only stop obviously impossible taps; the server re-checks all of
/// them and its answer wins.
class SelectionDraft {
  SelectionDraft(this.server) : draft = Map.of(server.items);

  /// The last page the server returned.
  final SelectionPage server;

  /// meal id → quantity being edited.
  final Map<String, int> draft;

  int get chosen => draft.values.fold(0, (a, b) => a + b);
  int get allowance => server.allowance;
  int get remaining => allowance - chosen;
  bool get editable => server.open;

  /// True when the draft differs from what the server holds.
  bool get dirty {
    final a = {for (final e in draft.entries) if (e.value > 0) e.key: e.value};
    final b = {for (final e in server.items.entries) if (e.value > 0) e.key: e.value};
    if (a.length != b.length) return true;
    return a.entries.any((e) => b[e.key] != e.value);
  }

  bool get complete => chosen == allowance;

  int quantity(String mealId) => draft[mealId] ?? 0;

  /// The highest quantity the customer could pick for this meal now.
  int maxFor(String mealId) {
    final meal = server.menu?.meals.where((m) => m.id == mealId).firstOrNull;
    final perMeal = meal?.maxPerCustomer ?? allowance;
    final room = quantity(mealId) + remaining;
    return [perMeal, room, 14].reduce((a, b) => a < b ? a : b);
  }

  bool canSet(String mealId, int q) => editable && q >= 0 && q <= maxFor(mealId);

  SelectionDraft set(String mealId, int q) {
    if (!canSet(mealId, q)) return this;
    final next = SelectionDraft(server);
    next.draft
      ..clear()
      ..addAll(draft);
    if (q == 0) {
      next.draft.remove(mealId);
    } else {
      next.draft[mealId] = q;
    }
    return next;
  }

  /// After a save: the server's page becomes the truth.
  SelectionDraft accepted(SelectionPage page) => SelectionDraft(page);

  /// After a conflict or a closed window: keep nothing that the server
  /// did not accept.
  SelectionDraft reset(SelectionPage page) => SelectionDraft(page);
}
