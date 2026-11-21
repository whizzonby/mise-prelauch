import '../../core/api/api_client.dart';
import 'catalogue.dart';

/// Every amount here comes from the API in minor units with its currency.
/// The app displays them; it never adds, discounts or converts.

class Price {
  const Price({required this.amountMinor, required this.currency, required this.formatted, this.effectiveFrom});

  factory Price.fromJson(Json j) => Price(
    amountMinor: integer(j, 'amount_minor'),
    currency: str(j, 'currency', 'TTD'),
    formatted: str(j, 'formatted'),
    effectiveFrom: time(j, 'effective_from'),
  );

  final DateTime? effectiveFrom;
  final int amountMinor;
  final String currency;
  final String formatted;
}

class Plan {
  const Plan({
    required this.id,
    required this.slug,
    required this.name,
    required this.summary,
    required this.description,
    required this.mealsPerWeek,
    required this.benefits,
    required this.price,
    required this.perMealMinor,
    required this.nextPrice,
    required this.purchasable,
    required this.unavailableReason,
    required this.isDemo,
  });

  factory Plan.fromJson(Json j) => Plan(
    id: str(j, 'id'),
    slug: str(j, 'slug'),
    name: str(j, 'name'),
    summary: str(j, 'summary'),
    description: str(j, 'description'),
    mealsPerWeek: integer(j, 'meals_per_week'),
    benefits: strings(j, 'benefits'),
    price: j['price'] is Map ? Price.fromJson(asMap(j['price'])) : null,
    perMealMinor: integer(j, 'per_meal_minor'),
    nextPrice: j['next_price'] is Map ? Price.fromJson(asMap(j['next_price'])) : null,
    purchasable: boolean(j, 'purchasable'),
    unavailableReason: strOrNull(j, 'unavailable_reason'),
    isDemo: boolean(j, 'is_demo'),
  );

  final String id;
  final String slug;
  final String name;
  final String summary;
  final String description;
  final int mealsPerWeek;
  final List<String> benefits;

  /// null when no price is configured: the plan cannot be bought.
  final Price? price;
  final int perMealMinor;
  final Price? nextPrice;
  final bool purchasable;
  final String? unavailableReason;
  final bool isDemo;
}

class Amounts {
  const Amounts({
    required this.currency,
    required this.subtotalMinor,
    required this.discountMinor,
    required this.deliveryFeeMinor,
    required this.taxMinor,
    required this.totalMinor,
  });

  factory Amounts.fromJson(Json j) => Amounts(
    currency: str(j, 'currency', 'TTD'),
    subtotalMinor: integer(j, 'subtotal_minor'),
    discountMinor: integer(j, 'discount_minor'),
    deliveryFeeMinor: integer(j, 'delivery_fee_minor'),
    taxMinor: integer(j, 'tax_minor'),
    totalMinor: integer(j, 'total_minor'),
  );

  final String currency;
  final int subtotalMinor;
  final int discountMinor;
  final int deliveryFeeMinor;
  final int taxMinor;
  final int totalMinor;
}

class PaymentsInfo {
  const PaymentsInfo({required this.available, required this.test});

  factory PaymentsInfo.fromJson(Json j) => PaymentsInfo(available: boolean(j, 'available'), test: boolean(j, 'test'));

  final bool available;
  final bool test;
}

class Quote {
  const Quote({
    required this.plan,
    required this.firstWeekLabel,
    required this.firstWeekCutoffAt,
    required this.dueNow,
    required this.renewal,
    required this.terms,
    required this.taxNote,
    required this.payments,
  });

  factory Quote.fromJson(Json j) => Quote(
    plan: Plan.fromJson(asMap(j['plan'])),
    firstWeekLabel: str(j, 'first_week_label'),
    firstWeekCutoffAt: time(j, 'first_week_cutoff_at'),
    dueNow: Amounts.fromJson(asMap(j['due_now'])),
    renewal: Amounts.fromJson(asMap(j['renewal'])),
    terms: str(j, 'terms'),
    taxNote: str(j, 'tax_note'),
    payments: PaymentsInfo.fromJson(asMap(j['payments'])),
  );

  final Plan plan;
  final String firstWeekLabel;
  final DateTime? firstWeekCutoffAt;
  final Amounts dueNow;
  final Amounts renewal;
  final String terms;
  final String taxNote;
  final PaymentsInfo payments;
}

class Address {
  const Address({
    required this.id,
    required this.label,
    required this.recipientName,
    required this.phone,
    required this.line1,
    required this.line2,
    required this.area,
    required this.city,
    required this.region,
    required this.regionLabel,
    required this.instructions,
    required this.isDefault,
    required this.deliveryAvailability,
  });

  factory Address.fromJson(Json j) => Address(
    id: str(j, 'id'),
    label: strOrNull(j, 'label'),
    recipientName: str(j, 'recipient_name'),
    phone: strOrNull(j, 'phone'),
    line1: str(j, 'line1'),
    line2: strOrNull(j, 'line2'),
    area: strOrNull(j, 'area'),
    city: str(j, 'city'),
    region: str(j, 'region'),
    regionLabel: str(j, 'region_label'),
    instructions: strOrNull(j, 'instructions'),
    isDefault: boolean(j, 'is_default'),
    deliveryAvailability: str(j, 'delivery_availability', 'unknown'),
  );

  final String id;
  final String? label;
  final String recipientName;
  final String? phone;
  final String line1;
  final String? line2;
  final String? area;
  final String city;
  final String region;
  final String regionLabel;
  final String? instructions;
  final bool isDefault;

  /// unknown | available | planned | unavailable — never a promise of delivery.
  final String deliveryAvailability;

  String get oneLine => [line1, ?line2, ?area, city, regionLabel].where((s) => s.isNotEmpty).join(', ');

  String get availabilityLabel => switch (deliveryAvailability) {
    'available' => 'We deliver here',
    'planned' => 'Delivery to this area is planned, not yet running',
    'unavailable' => 'We do not deliver here yet',
    _ => 'Delivery areas are still being confirmed',
  };
}

class BillingRef {
  const BillingRef({required this.id, required this.status, required this.totalMinor, required this.currency, required this.invoiceId});

  factory BillingRef.fromJson(Json j) => BillingRef(
    id: str(j, 'id'),
    status: str(j, 'status'),
    totalMinor: integer(j, 'total_minor'),
    currency: str(j, 'currency', 'TTD'),
    invoiceId: strOrNull(j, 'invoice_id'),
  );

  final String id;

  /// pending | due | paid | failed | void
  final String status;
  final int totalMinor;
  final String currency;
  final String? invoiceId;
}

/// One delivery week of a subscription (`SubscriptionCycle`).
class Cycle {
  const Cycle({
    required this.id,
    required this.week,
    required this.weekLabel,
    required this.cutoffAt,
    required this.state,
    required this.stateLabel,
    required this.planName,
    required this.mealsPerWeek,
    required this.amountMinor,
    required this.currency,
    required this.selectionOpen,
    required this.menuPublished,
    required this.selectionStatus,
    required this.selectedCount,
    required this.billing,
    required this.orderId,
  });

  factory Cycle.fromJson(Json j) {
    final plan = asMap(j['plan']);
    return Cycle(
      id: str(j, 'id'),
      week: str(j, 'week'),
      weekLabel: str(j, 'week_label'),
      cutoffAt: time(j, 'cutoff_at'),
      state: str(j, 'state'),
      stateLabel: str(j, 'state_label'),
      planName: str(plan, 'name'),
      mealsPerWeek: integer(plan, 'meals_per_week'),
      amountMinor: integer(j, 'amount_minor'),
      currency: str(j, 'currency', 'TTD'),
      selectionOpen: boolean(j, 'selection_open'),
      menuPublished: boolean(j, 'menu_published'),
      selectionStatus: strOrNull(j, 'selection_status'),
      selectedCount: integer(j, 'selected_count'),
      billing: j['billing'] is Map ? BillingRef.fromJson(asMap(j['billing'])) : null,
      orderId: strOrNull(j, 'order_id'),
    );
  }

  final String id;
  final String week;
  final String weekLabel;
  final DateTime? cutoffAt;

  /// scheduled | finalized | unpaid | needs_resolution | paused | cancelled
  final String state;
  final String stateLabel;
  final String planName;
  final int mealsPerWeek;
  final int amountMinor;
  final String currency;
  final bool selectionOpen;
  final bool menuPublished;
  final String? selectionStatus;
  final int selectedCount;
  final BillingRef? billing;
  final String? orderId;

  bool get confirmed => selectionStatus == 'confirmed' || selectionStatus == 'locked';
}

class SubscriptionActions {
  const SubscriptionActions({
    required this.changePlan,
    required this.pause,
    required this.resume,
    required this.cancel,
    required this.undoCancel,
    required this.changeAddress,
    required this.maxPauseWeeks,
  });

  factory SubscriptionActions.fromJson(Json j) => SubscriptionActions(
    changePlan: boolean(j, 'change_plan'),
    pause: boolean(j, 'pause'),
    resume: boolean(j, 'resume'),
    cancel: boolean(j, 'cancel'),
    undoCancel: boolean(j, 'undo_cancel'),
    changeAddress: boolean(j, 'change_address'),
    maxPauseWeeks: integer(j, 'max_pause_weeks'),
  );

  final bool changePlan;
  final bool pause;
  final bool resume;
  final bool cancel;
  final bool undoCancel;
  final bool changeAddress;
  final int maxPauseWeeks;
}

class Subscription {
  const Subscription({
    required this.id,
    required this.status,
    required this.statusLabel,
    required this.planId,
    required this.planName,
    required this.mealsPerWeek,
    required this.amountMinor,
    required this.currency,
    required this.priceFormatted,
    required this.address,
    required this.paymentMethod,
    required this.autoRenew,
    required this.pauseLabel,
    required this.cancelLabel,
    required this.cycles,
    required this.actions,
    required this.outstanding,
    required this.terms,
    required this.testPayments,
  });

  factory Subscription.fromJson(Json j) {
    final plan = asMap(j['plan']);
    return Subscription(
      id: str(j, 'id'),
      status: str(j, 'status'),
      statusLabel: str(j, 'status_label'),
      planId: str(plan, 'id'),
      planName: str(plan, 'name'),
      mealsPerWeek: integer(plan, 'meals_per_week'),
      amountMinor: integer(j, 'amount_minor'),
      currency: str(j, 'currency', 'TTD'),
      priceFormatted: str(j, 'price_formatted'),
      address: j['address'] is Map ? Address.fromJson(asMap(j['address'])) : null,
      paymentMethod: strOrNull(j, 'payment_method'),
      autoRenew: boolean(j, 'auto_renew'),
      pauseLabel: j['pause'] is Map ? str(asMap(j['pause']), 'label') : null,
      cancelLabel: j['cancel'] is Map ? str(asMap(j['cancel']), 'label') : null,
      cycles: asList(j['cycles'], Cycle.fromJson),
      actions: SubscriptionActions.fromJson(asMap(j['actions'])),
      outstanding: j['outstanding'] is Map ? Cycle.fromJson(asMap(j['outstanding'])) : null,
      terms: str(j, 'terms'),
      testPayments: boolean(j, 'test_payments'),
    );
  }

  final String id;

  /// pending_payment | active | pause_scheduled | paused | past_due |
  /// cancel_scheduled | cancelled | expired
  final String status;
  final String statusLabel;
  final String planId;
  final String planName;
  final int mealsPerWeek;
  final int amountMinor;
  final String currency;
  final String priceFormatted;
  final Address? address;
  final String? paymentMethod;
  final bool autoRenew;
  final String? pauseLabel;
  final String? cancelLabel;
  final List<Cycle> cycles;
  final SubscriptionActions actions;

  /// A week that still has to be paid.
  final Cycle? outstanding;
  final String terms;
  final bool testPayments;

  bool get isActiveLike => const {'active', 'pause_scheduled', 'cancel_scheduled', 'past_due'}.contains(status);

  /// The first week whose meals can still be chosen.
  Cycle? get openCycle {
    for (final c in cycles) {
      if (c.selectionOpen) return c;
    }
    return null;
  }

  /// The next week that is not over: the "upcoming box".
  Cycle? get upcoming {
    for (final c in cycles) {
      if (c.state != 'cancelled' && c.state != 'paused') return c;
    }
    return cycles.isEmpty ? null : cycles.first;
  }
}

class MealNote {
  const MealNote({required this.containsYourAllergens, required this.allergensUnconfirmed, required this.matchesPreferences});

  factory MealNote.fromJson(Json j) => MealNote(
    containsYourAllergens: asList(j['contains_your_allergens'], Term.fromJson),
    allergensUnconfirmed: boolean(j, 'allergens_unconfirmed'),
    matchesPreferences: asList(j['matches_preferences'], Term.fromJson),
  );

  final List<Term> containsYourAllergens;
  final bool allergensUnconfirmed;
  final List<Term> matchesPreferences;
}

/// `SelectionPage`: everything the weekly selection screen needs.
class SelectionPage {
  const SelectionPage({
    required this.cycle,
    required this.menu,
    required this.items,
    required this.version,
    required this.allowance,
    required this.selected,
    required this.remaining,
    required this.open,
    required this.confirmed,
    required this.notes,
    required this.crossContactNotice,
    required this.serverTime,
  });

  factory SelectionPage.fromJson(Json j) {
    final sel = j['selection'] is Map ? asMap(j['selection']) : null;
    final notes = <String, MealNote>{};
    asMap(j['notes']).forEach((k, v) => notes[k] = MealNote.fromJson(asMap(v)));
    return SelectionPage(
      cycle: Cycle.fromJson(asMap(j['cycle'])),
      menu: j['menu'] is Map ? WeeklyMenu.fromJson(asMap(j['menu'])) : null,
      items: {for (final i in asList(sel?['items'], (x) => x)) str(i, 'meal_id'): integer(i, 'quantity')},
      version: sel == null ? null : intOrNull(sel, 'version'),
      allowance: integer(j, 'allowance'),
      selected: integer(j, 'selected'),
      remaining: integer(j, 'remaining'),
      open: boolean(j, 'open'),
      confirmed: boolean(j, 'confirmed'),
      notes: notes,
      crossContactNotice: str(j, 'cross_contact_notice'),
      serverTime: time(j, 'server_time') ?? DateTime.now().toUtc(),
    );
  }

  final Cycle cycle;
  final WeeklyMenu? menu;

  /// meal id → quantity, as the server has it.
  final Map<String, int> items;

  /// For optimistic concurrency: another device's newer save is a 409.
  final int? version;
  final int allowance;
  final int selected;
  final int remaining;
  final bool open;
  final bool confirmed;
  final Map<String, MealNote> notes;
  final String crossContactNotice;
  final DateTime serverTime;
}

class PaymentOption {
  const PaymentOption({required this.provider, required this.label, required this.method, required this.test, required this.recurring});

  factory PaymentOption.fromJson(Json j) => PaymentOption(
    provider: str(j, 'provider'),
    label: str(j, 'label'),
    method: str(j, 'method'),
    test: boolean(j, 'test'),
    recurring: boolean(j, 'recurring'),
  );

  final String provider;
  final String label;

  /// card | paypal
  final String method;

  /// No real money moves.
  final bool test;

  /// Can be charged by Mise for later weeks.
  final bool recurring;
}

class Checkout {
  const Checkout({
    required this.id,
    required this.status,
    required this.redirectUrl,
    required this.expiresAt,
    required this.amountMinor,
    required this.currency,
    required this.provider,
    required this.test,
  });

  factory Checkout.fromJson(Json j) => Checkout(
    id: str(j, 'id'),
    status: str(j, 'status'),
    redirectUrl: strOrNull(j, 'redirect_url'),
    expiresAt: time(j, 'expires_at'),
    amountMinor: integer(j, 'amount_minor'),
    currency: str(j, 'currency', 'TTD'),
    provider: str(j, 'provider'),
    test: boolean(j, 'test'),
  );

  final String id;
  final String status;
  final String? redirectUrl;
  final DateTime? expiresAt;
  final int amountMinor;
  final String currency;
  final String provider;
  final bool test;
}

enum PaymentOutcome { pending, succeeded, failed, expired }

PaymentOutcome parseOutcome(String s) => switch (s) {
  'succeeded' => PaymentOutcome.succeeded,
  'failed' => PaymentOutcome.failed,
  'expired' => PaymentOutcome.expired,
  _ => PaymentOutcome.pending,
};

class CheckoutResult {
  const CheckoutResult({required this.checkout, required this.outcome, required this.subscriptionStatus, required this.weekLabel, required this.invoiceId});

  factory CheckoutResult.fromJson(Json j) => CheckoutResult(
    checkout: Checkout.fromJson(asMap(j['checkout'])),
    outcome: parseOutcome(str(j, 'outcome')),
    subscriptionStatus: str(j, 'subscription_status'),
    weekLabel: str(j, 'week_label'),
    invoiceId: strOrNull(j, 'invoice_id'),
  );

  final Checkout checkout;
  final PaymentOutcome outcome;
  final String subscriptionStatus;
  final String weekLabel;
  final String? invoiceId;
}
