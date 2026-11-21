import 'package:flutter_test/flutter_test.dart';
import 'package:mise_mobile/features/catalogue/catalogue_api.dart';
import 'package:mise_mobile/features/checkout/payment_flow.dart';
import 'package:mise_mobile/features/meal_selection/selection_state.dart';
import 'package:mise_mobile/shared/models/catalogue.dart';
import 'package:mise_mobile/shared/models/commerce.dart';

const _cycleId = '11111111-1111-4111-8111-111111111111';

Map<String, dynamic> mealJson(String id, {int? max}) => {
  'id': id,
  'slug': 'meal-$id',
  'name': 'Meal $id',
  'summary': '',
  'category': {'slug': 'family', 'name': 'Family'},
  'chef': null,
  'prep_minutes': 10,
  'cook_minutes': 25,
  'total_minutes': 35,
  'servings': 2,
  'protein': null,
  'dietary': [
    {'key': 'vegetarian', 'label': 'Vegetarian'},
  ],
  'allergens': {'status': 'declared', 'contains': [], 'notice': 'Made in a kitchen that handles nuts.'},
  'image': {'src': '/catalog/x.jpg', 'alt': 'A plate', 'width': 10, 'height': 10},
  'nutrition': null,
  'is_demo': false,
  'max_per_customer': max,
  'some_future_field': 'ignored',
};

Map<String, dynamic> pageJson({Map<String, int> items = const {}, bool open = true, int allowance = 3, int version = 4}) => {
  'cycle': {
    'id': _cycleId,
    'week': '2026-11-30',
    'week_label': 'Week of 30 November',
    'cutoff_at': '2026-11-26T03:59:00Z',
    'renewal_at': '2026-11-26T04:00:00Z',
    'state': 'scheduled',
    'state_label': 'Upcoming',
    'plan': {'id': 'p', 'name': 'Classic', 'meals_per_week': allowance},
    'amount_minor': 33000,
    'currency': 'TTD',
    'selection_open': open,
    'menu_published': true,
    'selection_status': items.isEmpty ? null : 'draft',
    'selected_count': items.values.fold(0, (a, b) => a + b),
    'billing': null,
    'order_id': null,
  },
  'menu': {
    'id': 'm',
    'week': '2026-11-30',
    'week_label': 'Week of 30 November',
    'title': null,
    'status': 'published',
    'cutoff_at': '2026-11-26T03:59:00Z',
    'is_open': open,
    'meal_count': 3,
    'note': null,
    'time_zone': 'America/Port_of_Spain',
    'published_at': null,
    'eligible_plan_ids': ['p'],
    'meals': [mealJson('a', max: 1), mealJson('b'), mealJson('c')],
  },
  'selection': items.isEmpty
      ? null
      : {
          'id': 's',
          'status': 'draft',
          'version': version,
          'items': [
            for (final e in items.entries) {'meal_id': e.key, 'quantity': e.value},
          ],
          'count': 0,
          'confirmed_at': null,
          'locked_at': null,
          'updated_at': '2026-11-21T00:00:00Z',
          'menu_id': 'm',
        },
  'allowance': allowance,
  'selected': 0,
  'remaining': allowance,
  'open': open,
  'confirmed': false,
  'notes': {
    'b': {
      'contains_your_allergens': [
        {'key': 'peanuts', 'label': 'Peanuts'},
      ],
      'allergens_unconfirmed': false,
      'matches_preferences': [],
    },
  },
  'has_allergies': true,
  'allergies_reviewed': true,
  'cross_contact_notice': 'Cross-contact is possible.',
  'server_time': '2026-11-21T00:00:00Z',
};

void main() {
  group('API serialization', () {
    test('a meal card parses and tolerates unknown fields', () {
      final m = Meal.fromJson(mealJson('a', max: 2));
      expect(m.name, 'Meal a');
      expect(m.facts, '35 min · Serves 2');
      expect(m.allergens.declared, isTrue);
      expect(m.calories, isNull, reason: 'unverified nutrition is not shown');
      expect(m.maxPerCustomer, 2);
    });

    test('a subscription and its actions parse; flags come from the server', () {
      final s = Subscription.fromJson({
        'id': 'sub',
        'status': 'active',
        'status_label': 'Active',
        'plan': {'id': 'p', 'name': 'Classic', 'meals_per_week': 3},
        'amount_minor': 33000,
        'currency': 'TTD',
        'price_formatted': 'TTD 330.00',
        'address': null,
        'payment_method': null,
        'auto_renew': false,
        'first_week': '2026-11-30',
        'activated_at': null,
        'pause': null,
        'cancel': null,
        'cycles': [pageJson()['cycle']],
        'actions': {'change_plan': true, 'pause': false, 'resume': false, 'cancel': true, 'undo_cancel': false, 'change_address': true, 'max_pause_weeks': 4},
        'outstanding': null,
        'terms': '',
        'test_payments': true,
      });
      expect(s.isActiveLike, isTrue);
      expect(s.openCycle?.id, _cycleId);
      expect(s.actions.pause, isFalse);
      expect(s.actions.cancel, isTrue);
      expect(s.testPayments, isTrue);
    });

    test('checkout outcomes map exactly', () {
      expect(parseOutcome('succeeded'), PaymentOutcome.succeeded);
      expect(parseOutcome('failed'), PaymentOutcome.failed);
      expect(parseOutcome('expired'), PaymentOutcome.expired);
      expect(parseOutcome('pending'), PaymentOutcome.pending);
      expect(parseOutcome('anything else'), PaymentOutcome.pending, reason: 'unknown is never success');
    });
  });

  group('payment status', () {
    test('a pending payment that times out never says it worked and warns not to pay again', () {
      final c = outcomeCopy(PaymentOutcome.pending, timedOut: true);
      expect(c.body, contains('Do not pay again'));
      expect(outcomeCopy(PaymentOutcome.failed, timedOut: false).body, contains('Nothing was charged'));
    });

    test('polling stops at the first final outcome', () async {
      final results = [PaymentOutcome.pending, PaymentOutcome.pending, PaymentOutcome.succeeded, PaymentOutcome.failed];
      var i = 0;
      final seen = await pollOutcome(
        () async => _result(results[i++]),
        interval: Duration.zero,
      ).map((r) => r.outcome).toList();
      expect(seen, [PaymentOutcome.pending, PaymentOutcome.pending, PaymentOutcome.succeeded]);
    });

    test('polling survives a network blip and gives up at the limit', () async {
      var calls = 0;
      final seen = await pollOutcome(
        () async {
          calls++;
          if (calls == 1) throw Exception('offline');
          return _result(PaymentOutcome.pending);
        },
        interval: const Duration(milliseconds: 5),
        limit: const Duration(milliseconds: 40),
      ).toList();
      expect(seen, isNotEmpty);
      expect(seen.every((r) => r.outcome == PaymentOutcome.pending), isTrue);
    });
  });

  group('meal selection draft', () {
    test('starts from what the server holds and is clean', () {
      final d = SelectionDraft(SelectionPage.fromJson(pageJson(items: {'b': 1})));
      expect(d.chosen, 1);
      expect(d.dirty, isFalse);
      expect(d.server.notes['b']!.containsYourAllergens.single.label, 'Peanuts');
    });

    test('respects the allowance and per-meal maximum', () {
      var d = SelectionDraft(SelectionPage.fromJson(pageJson()));
      d = d.set('a', 1);
      expect(d.canSet('a', 2), isFalse, reason: 'max_per_customer is 1');
      d = d.set('b', 2);
      expect(d.complete, isTrue);
      expect(d.canSet('c', 1), isFalse, reason: 'allowance reached');
      expect(d.dirty, isTrue);
    });

    test('cannot change a closed week', () {
      final d = SelectionDraft(SelectionPage.fromJson(pageJson(open: false)));
      expect(d.set('b', 1).quantity('b'), 0);
    });

    test('a conflict resets to the server copy, keeping nothing unsaved', () {
      var d = SelectionDraft(SelectionPage.fromJson(pageJson(items: {'a': 1})));
      d = d.set('b', 2);
      final fresh = SelectionPage.fromJson(pageJson(items: {'c': 3}, version: 5));
      d = d.reset(fresh);
      expect(d.draft, {'c': 3});
      expect(d.server.version, 5);
      expect(d.dirty, isFalse);
    });
  });

  test('meal queries compare by value, so filters do not refetch needlessly', () {
    const a = MealQuery(diet: {'vegan', 'halal'});
    const b = MealQuery(diet: {'halal', 'vegan'});
    expect(a, b);
    expect(a.params(1, 12)['diet'], 'halal,vegan');
    expect(a.copyWith(category: null).category, isNull);
  });
}

CheckoutResult _result(PaymentOutcome o) => CheckoutResult(
  checkout: const Checkout(id: 'c', status: 'open', redirectUrl: null, expiresAt: null, amountMinor: 33000, currency: 'TTD', provider: 'testpay', test: true),
  outcome: o,
  subscriptionStatus: 'pending_payment',
  weekLabel: 'Week of 30 November',
  invoiceId: null,
);
