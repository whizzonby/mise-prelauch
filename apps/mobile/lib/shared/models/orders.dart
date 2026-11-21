import '../../core/api/api_client.dart';

class OrderSummary {
  const OrderSummary({
    required this.id,
    required this.number,
    required this.status,
    required this.week,
    required this.planName,
    required this.mealsCount,
    required this.totalMinor,
    required this.currency,
    required this.deliveryOutcome,
  });

  factory OrderSummary.fromJson(Json j) => OrderSummary(
    id: str(j, 'id'),
    number: str(j, 'number'),
    status: str(j, 'status'),
    week: str(j, 'week'),
    planName: str(j, 'plan_name'),
    mealsCount: integer(j, 'meals_count'),
    totalMinor: integer(j, 'total_minor'),
    currency: str(j, 'currency', 'TTD'),
    deliveryOutcome: strOrNull(j, 'delivery_outcome'),
  );

  final String id;
  final String number;
  final String status;
  final String week;
  final String planName;
  final int mealsCount;
  final int totalMinor;
  final String currency;
  final String? deliveryOutcome;

  Json toJson() => {
    'id': id,
    'number': number,
    'status': status,
    'week': week,
    'plan_name': planName,
    'meals_count': mealsCount,
    'total_minor': totalMinor,
    'currency': currency,
    'delivery_outcome': deliveryOutcome,
  };
}

/// Customer wording for the order statuses in the API.
String orderStatusLabel(String s) => switch (s) {
  'pending' => 'Awaiting payment',
  'confirmed' => 'Confirmed',
  'awaiting_production' => 'Confirmed',
  'in_production' => 'Being prepared',
  'ready_for_fulfillment' => 'Packed',
  'out_for_delivery' => 'Out for delivery',
  'delivered' => 'Delivered',
  'cancelled' => 'Cancelled',
  _ => s.replaceAll('_', ' '),
};

class OrderItem {
  const OrderItem({required this.mealId, required this.name, required this.slug, required this.quantity, required this.servings, required this.allergens});

  factory OrderItem.fromJson(Json j) => OrderItem(
    mealId: str(j, 'meal_id'),
    name: str(j, 'name'),
    slug: str(j, 'slug'),
    quantity: integer(j, 'quantity'),
    servings: integer(j, 'servings'),
    allergens: strings(j, 'allergens'),
  );

  final String mealId;
  final String name;
  final String slug;
  final int quantity;
  final int servings;
  final List<String> allergens;
}

class OrderDetail {
  const OrderDetail({
    required this.summary,
    required this.subtotalMinor,
    required this.discountMinor,
    required this.deliveryFeeMinor,
    required this.taxMinor,
    required this.addressLines,
    required this.items,
    required this.createdAt,
    required this.paymentId,
    required this.deliveryOutcomeState,
    required this.deliveryOutcomeReason,
  });

  factory OrderDetail.fromJson(Json j) {
    final a = asMap(j['address']);
    final outcome = j['delivery_outcome'] is Map ? asMap(j['delivery_outcome']) : null;
    return OrderDetail(
      summary: OrderSummary(
        id: str(j, 'id'),
        number: str(j, 'number'),
        status: str(j, 'status'),
        week: str(j, 'week'),
        planName: str(j, 'plan_name'),
        mealsCount: integer(j, 'meals_count'),
        totalMinor: integer(j, 'total_minor'),
        currency: str(j, 'currency', 'TTD'),
        deliveryOutcome: outcome == null ? null : str(outcome, 'state'),
      ),
      subtotalMinor: integer(j, 'subtotal_minor'),
      discountMinor: integer(j, 'discount_minor'),
      deliveryFeeMinor: integer(j, 'delivery_fee_minor'),
      taxMinor: integer(j, 'tax_minor'),
      addressLines: [
        str(a, 'recipient_name'),
        str(a, 'line1'),
        if (str(a, 'line2').isNotEmpty) str(a, 'line2'),
        [str(a, 'area'), str(a, 'city'), str(a, 'region_label')].where((x) => x.isNotEmpty).join(', '),
      ].where((x) => x.isNotEmpty).toList(),
      items: asList(j['items'], OrderItem.fromJson),
      createdAt: time(j, 'created_at'),
      paymentId: strOrNull(j, 'payment_id'),
      deliveryOutcomeState: outcome == null ? null : str(outcome, 'state'),
      deliveryOutcomeReason: outcome == null ? null : str(outcome, 'reason'),
    );
  }

  final OrderSummary summary;
  final int subtotalMinor;
  final int discountMinor;
  final int deliveryFeeMinor;
  final int taxMinor;
  final List<String> addressLines;
  final List<OrderItem> items;
  final DateTime? createdAt;
  final String? paymentId;

  /// A corrected delivery outcome (disputed / confirmed), if any.
  final String? deliveryOutcomeState;
  final String? deliveryOutcomeReason;

  bool get paid => paymentId != null && summary.status != 'pending';
  bool get cookable => const {'confirmed', 'awaiting_production', 'in_production', 'ready_for_fulfillment', 'out_for_delivery', 'delivered'}.contains(summary.status);
}

class DeliveryMilestone {
  const DeliveryMilestone({required this.key, required this.label, required this.done, required this.at});

  factory DeliveryMilestone.fromJson(Json j) =>
      DeliveryMilestone(key: str(j, 'key'), label: str(j, 'label'), done: boolean(j, 'done'), at: time(j, 'at'));

  final String key;
  final String label;
  final bool done;
  final DateTime? at;
}

/// `CustomerDelivery`: milestones from real records, in plain words.
class DeliveryStatus {
  const DeliveryStatus({
    required this.stage,
    required this.headline,
    required this.detail,
    required this.serviceDate,
    required this.window,
    required this.addressLines,
    required this.confirmationCode,
    required this.codeExplanation,
    required this.timeline,
    required this.issue,
    required this.liveTracking,
  });

  factory DeliveryStatus.fromJson(Json j) {
    final a = asMap(j['address']);
    String s(String k) => a[k] is String ? a[k] as String : '';
    return DeliveryStatus(
      stage: str(j, 'stage'),
      headline: str(j, 'headline'),
      detail: str(j, 'detail'),
      serviceDate: strOrNull(j, 'service_date'),
      window: strOrNull(j, 'window'),
      addressLines: [s('recipient_name'), s('line1'), s('line2'), [s('area'), s('city')].where((x) => x.isNotEmpty).join(', ')]
          .where((x) => x.isNotEmpty)
          .toList(),
      confirmationCode: strOrNull(j, 'confirmation_code'),
      codeExplanation: str(j, 'code_explanation'),
      timeline: asList(j['timeline'], DeliveryMilestone.fromJson),
      issue: strOrNull(j, 'issue'),
      liveTracking: boolean(j, 'live_tracking'),
    );
  }

  /// preparing | packed | scheduled | out_for_delivery | delivered | issue | cancelled
  final String stage;
  final String headline;
  final String detail;
  final String? serviceDate;
  final String? window;
  final List<String> addressLines;
  final String? confirmationCode;
  final String codeExplanation;
  final List<DeliveryMilestone> timeline;
  final String? issue;

  /// Always false today: there is no GPS tracking, and none is shown.
  final bool liveTracking;
}

class CustomerRecipe {
  const CustomerRecipe({
    required this.mealName,
    required this.mealSlug,
    required this.servings,
    required this.servingSize,
    required this.cookMinutes,
    required this.ingredients,
    required this.steps,
    required this.storage,
    required this.allergens,
    required this.mayContain,
    required this.crossContact,
  });

  factory CustomerRecipe.fromJson(Json j) => CustomerRecipe(
    mealName: str(j, 'meal_name'),
    mealSlug: str(j, 'meal_slug'),
    servings: integer(j, 'servings'),
    servingSize: str(j, 'serving_size'),
    cookMinutes: intOrNull(j, 'cook_minutes'),
    ingredients: strings(j, 'ingredients'),
    steps: strings(j, 'steps'),
    storage: str(j, 'storage'),
    allergens: strings(j, 'allergens'),
    mayContain: strings(j, 'may_contain'),
    crossContact: str(j, 'cross_contact_note'),
  );

  final String mealName;
  final String mealSlug;
  final int servings;
  final String servingSize;
  final int? cookMinutes;
  final List<String> ingredients;
  final List<String> steps;
  final String storage;
  final List<String> allergens;
  final List<String> mayContain;
  final String crossContact;
}
