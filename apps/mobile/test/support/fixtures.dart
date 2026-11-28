import 'package:dio/dio.dart';

/// Demo data for widget tests and test renders. Names and figures are
/// fixtures, not Mise's catalogue or prices.

const cycleId = '11111111-1111-4111-8111-111111111111';
const orderId = '22222222-2222-4222-8222-222222222222';
const convId = '33333333-3333-4333-8333-333333333333';
const subId = '44444444-4444-4444-8444-444444444444';
const billingId = '55555555-5555-4555-8555-555555555555';
const checkoutId = '66666666-6666-4666-8666-666666666666';
const planId = '77777777-7777-4777-8777-777777777777';

const _meals = [
  ('a1111111-0000-4000-8000-000000000001', 'curry-chicken', 'Curry chicken with roti', 'Family', 45, 'meal-curry-chicken'),
  ('a1111111-0000-4000-8000-000000000002', 'pelau', 'One-pot pelau', 'Family', 55, 'meal-plantain'),
  ('a1111111-0000-4000-8000-000000000003', 'channa-and-aloo', 'Channa and aloo', 'Vegetarian', 35, 'meal-channa'),
  ('a1111111-0000-4000-8000-000000000004', 'oxtail', 'Stewed oxtail', 'Weekend', 90, 'meal-oxtail'),
  ('a1111111-0000-4000-8000-000000000005', 'pumpkin-soup', 'Pumpkin soup', 'Light', 30, 'meal-pumpkin-soup'),
];

/// Bundled photos used in place of network images in tests.
String fixtureImageFor(String src) => switch (src) {
  _ when src.contains('channa') => 'assets/images/onboarding-cook.jpg',
  _ when src.contains('pumpkin') => 'assets/images/onboarding-kit.jpg',
  _ => 'assets/images/onboarding-meals.jpg',
};

Map<String, dynamic> meal(int i, {int? max}) {
  final (id, slug, name, cat, mins, img) = _meals[i];
  return {
    'id': id,
    'slug': slug,
    'name': name,
    'summary': 'A weeknight favourite from home.',
    'category': {'slug': cat.toLowerCase(), 'name': cat},
    'chef': null,
    'prep_minutes': 15,
    'cook_minutes': mins - 15,
    'total_minutes': mins,
    'servings': 2,
    'protein': null,
    'dietary': i == 2
        ? [
            {'key': 'vegetarian', 'label': 'Vegetarian'},
          ]
        : [],
    'allergens': {
      'status': 'declared',
      'contains': i == 0
          ? [
              {'key': 'gluten', 'label': 'Gluten'},
            ]
          : [],
      'notice': 'Prepared in a kitchen that also handles nuts, fish and gluten.',
    },
    'image': {'src': '/catalog/demo/$img.jpg', 'alt': name, 'width': 1200, 'height': 900},
    'nutrition': null,
    'is_demo': false,
    'max_per_customer': max,
  };
}

Map<String, dynamic> menu() => {
  'id': 'm1',
  'week': '2026-11-30',
  'week_label': 'Week of 30 November',
  'title': 'Comfort from home',
  'status': 'published',
  'cutoff_at': '2026-11-27T03:59:00Z',
  'is_open': true,
  'meal_count': 5,
  'note': 'Five dinners for the first week of December.',
  'time_zone': 'America/Port_of_Spain',
  'published_at': '2026-11-20T12:00:00Z',
  'eligible_plan_ids': [planId],
  'meals': [for (var i = 0; i < 5; i++) meal(i, max: i == 3 ? 1 : null)],
};

Map<String, dynamic> cycle({bool open = true, int selected = 1, String billing = 'paid'}) => {
  'id': cycleId,
  'week': '2026-11-30',
  'week_label': 'Week of 30 November',
  'cutoff_at': '2026-11-27T03:59:00Z',
  'renewal_at': '2026-11-27T04:00:00Z',
  'state': 'scheduled',
  'state_label': 'Upcoming',
  'plan': {'id': planId, 'name': 'Classic for two', 'meals_per_week': 3},
  'amount_minor': 54000,
  'currency': 'TTD',
  'selection_open': open,
  'menu_published': true,
  'selection_status': selected > 0 ? 'draft' : null,
  'selected_count': selected,
  'billing': {'id': billingId, 'status': billing, 'total_minor': 54000, 'currency': 'TTD', 'invoice_id': null, 'invoice_number': null},
  'order_id': billing == 'paid' ? orderId : null,
};

Map<String, dynamic> address() => {
  'id': 'ad111111-0000-4000-8000-000000000001',
  'label': 'Home',
  'recipient_name': 'Kavita Ramdass',
  'phone': '+1 868 555 0100',
  'line1': '12 Saddle Road',
  'line2': null,
  'area': 'Maraval',
  'city': 'Port of Spain',
  'region': 'port-of-spain',
  'region_label': 'Port of Spain',
  'country': 'TT',
  'instructions': null,
  'is_default': true,
  'delivery_availability': 'available',
  'created_at': '2026-11-01T00:00:00Z',
  'updated_at': '2026-11-01T00:00:00Z',
};

Map<String, dynamic> subscription() => {
  'id': subId,
  'status': 'active',
  'status_label': 'Active',
  'plan': {'id': planId, 'name': 'Classic for two', 'meals_per_week': 3},
  'amount_minor': 54000,
  'currency': 'TTD',
  'price_formatted': 'TTD 540.00',
  'address': address(),
  'payment_method': 'Test card',
  'auto_renew': true,
  'first_week': '2026-11-23',
  'activated_at': '2026-11-20T00:00:00Z',
  'pause': null,
  'cancel': null,
  'cycles': [cycle()],
  'actions': {'change_plan': true, 'pause': true, 'resume': false, 'cancel': true, 'undo_cancel': false, 'change_address': true, 'max_pause_weeks': 4},
  'outstanding': null,
  'terms': 'Each week is charged before it is delivered. Cancel before the next unpaid week.',
  'test_payments': true,
};

Map<String, dynamic> plan(String slug, String name, int meals, int price) => {
  'id': slug == 'classic' ? planId : 'p-$slug',
  'slug': slug,
  'name': name,
  'summary': '',
  'description': '',
  'meals_per_week': meals,
  'billing_interval': 'week',
  'segment': {'key': 'household', 'label': 'Household'},
  'benefits': ['Choose from the weekly menu', 'Skip or cancel online'],
  'self_serve': true,
  'price': {'id': 'pr', 'amount_minor': price, 'currency': 'TTD', 'effective_from': '2026-11-01T00:00:00Z', 'formatted': 'TTD ${price ~/ 100}.00'},
  'per_meal_minor': price ~/ meals,
  'next_price': null,
  'purchasable': true,
  'is_demo': false,
};

Map<String, dynamic> amounts(int total) => {
  'currency': 'TTD',
  'subtotal_minor': total,
  'discount_minor': 0,
  'delivery_fee_minor': 0,
  'tax_minor': 0,
  'total_minor': total,
  'tax': {},
  'discount': null,
};

Map<String, dynamic> selectionPage() => {
  'cycle': cycle(),
  'menu': menu(),
  'selection': {
    'id': 's',
    'status': 'draft',
    'version': 3,
    'items': [
      {'meal_id': meal(0)['id'], 'quantity': 1},
    ],
    'count': 1,
    'confirmed_at': null,
    'locked_at': null,
    'updated_at': '2026-11-21T00:00:00Z',
    'menu_id': 'm1',
  },
  'allowance': 3,
  'selected': 1,
  'remaining': 2,
  'open': true,
  'confirmed': false,
  'notes': {
    meal(0)['id']: {
      'contains_your_allergens': [
        {'key': 'gluten', 'label': 'Gluten'},
      ],
      'allergens_unconfirmed': false,
      'matches_preferences': [],
    },
    meal(2)['id']: {
      'contains_your_allergens': [],
      'allergens_unconfirmed': false,
      'matches_preferences': [
        {'key': 'vegetarian', 'label': 'Vegetarian'},
      ],
    },
  },
  'has_allergies': true,
  'allergies_reviewed': true,
  'cross_contact_notice': 'Meals are prepared in a kitchen that handles common allergens.',
  'server_time': '2026-11-25T14:00:00Z',
};

Map<String, Object? Function(RequestOptions)> fixtureRoutes() => {
  'GET /me': (_) => {'id': 'u1', 'email': 'kavita@example.com', 'first_name': 'Kavita', 'last_name': 'Ramdass', 'email_verified': true},
  'GET /meals': (_) => {'items': [for (var i = 0; i < 5; i++) meal(i)], 'total': 5, 'page': 1, 'page_size': 12},
  'GET /meals/*': (o) {
    final m = meal(_meals.indexWhere((x) => o.path.endsWith(x.$2)).clamp(0, 4));
    return {
      ...m,
      'description': 'Chicken marinated in green seasoning, curried with potato and served with soft roti. A Sunday lunch you can make on a Tuesday.',
      'cooking_overview': 'Marinate, brown, simmer with curry and potato, warm the roti.',
      'images': [m['image']],
      'ingredients': [
        {'name': 'Chicken thighs', 'quantity': '500 g'},
        {'name': 'Green seasoning', 'quantity': '2 tbsp'},
        {'name': 'Potato', 'quantity': '2'},
        {'name': 'Roti skins', 'quantity': '4'},
      ],
      'nutrition_detail': {'calories': 610, 'protein_g': 42, 'carbs_g': 58, 'fat_g': 21, 'fibre_g': 6, 'sodium_mg': 880, 'verified_at': '2026-11-10T00:00:00Z'},
      'chef_detail': null,
      'published_at': '2026-11-10T00:00:00Z',
    };
  },
  'GET /meal-categories': (_) => {
    'items': [
      {'slug': 'family', 'name': 'Family', 'description': '', 'meal_count': 2},
      {'slug': 'vegetarian', 'name': 'Vegetarian', 'description': '', 'meal_count': 1},
      {'slug': 'light', 'name': 'Light', 'description': '', 'meal_count': 1},
    ],
  },
  'GET /reference': (_) => {
    'dietary': [
      {'key': 'vegetarian', 'label': 'Vegetarian'},
      {'key': 'pescatarian', 'label': 'Pescatarian'},
    ],
    'allergens': [
      {'key': 'peanuts', 'label': 'Peanuts'},
      {'key': 'gluten', 'label': 'Gluten'},
    ],
    'meal_allergens': [
      {'key': 'gluten', 'label': 'Gluten'},
      {'key': 'fish', 'label': 'Fish'},
    ],
    'proteins': [],
    'meal_interests': [],
    'countries': [],
    'regions': [
      {'key': 'port-of-spain', 'label': 'Port of Spain'},
    ],
    'topics': [],
    'cross_contact_notice': 'Meals are prepared in a kitchen that handles common allergens.',
  },
  'GET /weekly-menus': (_) => {
    'items': [
      {for (final e in menu().entries) if (e.key != 'meals') e.key: e.value},
    ],
  },
  'GET /weekly-menus/*': (_) => menu(),
  'GET /plans': (_) => {
    'items': [plan('classic', 'Classic for two', 3, 54000), plan('family', 'Family of four', 4, 96000)],
  },
  'GET /plans/*': (_) => plan('classic', 'Classic for two', 3, 54000),
  'GET /subscription-info': (_) => {
    'payments': {'available': true, 'test': true},
    'next_open_week': '2026-11-30',
    'cutoff_rule': 'Choose your meals by Thursday 11:59 pm, Trinidad and Tobago time.',
    'time_zone': 'America/Port_of_Spain',
    'max_pause_weeks': 4,
    'cancel_reasons': [],
  },
  'GET /me/subscription': (_) => {'subscription': subscription()},
  'GET /me/subscription-cycles': (_) => {
    'items': [cycle()],
  },
  'GET /me/meal-selections/*': (_) => selectionPage(),
  'POST /me/subscriptions/quote': (_) => {
    'plan': plan('classic', 'Classic for two', 3, 54000),
    'first_week': '2026-11-30',
    'first_week_label': 'Week of 30 November',
    'first_week_cutoff_at': '2026-11-27T03:59:00Z',
    'due_now': amounts(54000),
    'renewal': amounts(54000),
    'promotion': null,
    'terms': 'By paying you agree to weekly renewal until you skip or cancel.',
    'tax_note': 'Tax and delivery are not charged at present.',
    'payments': {'available': true, 'test': true},
  },
  'GET /me/addresses': (_) => {
    'items': [address()],
  },
  'GET /payment-options': (_) => {
    'currency': 'TTD',
    'items': [
      {'provider': 'testpay', 'label': 'Card (test)', 'method': 'card', 'test': true, 'recurring': true},
    ],
  },
  'GET /checkout/sessions/*': (_) => {
    'checkout': {'id': checkoutId, 'status': 'completed', 'expires_at': '2026-11-21T01:00:00Z', 'billing_cycle_id': billingId, 'amount_minor': 54000, 'currency': 'TTD', 'provider': 'testpay', 'test': true},
    'outcome': 'succeeded',
    'subscription_status': 'active',
    'week': '2026-11-30',
    'week_label': 'Week of 30 November',
    'invoice_id': null,
  },
  'GET /me/orders': (_) => {
    'items': [
      {'id': orderId, 'number': 'MS-10482', 'status': 'out_for_delivery', 'week': '2026-11-23', 'plan_name': 'Classic for two', 'meals_count': 3, 'total_minor': 54000, 'currency': 'TTD', 'created_at': '2026-11-20T00:00:00Z'},
      {'id': '99999999-9999-4999-8999-999999999999', 'number': 'MS-10377', 'status': 'delivered', 'week': '2026-11-16', 'plan_name': 'Classic for two', 'meals_count': 3, 'total_minor': 54000, 'currency': 'TTD', 'created_at': '2026-11-13T00:00:00Z'},
    ],
  },
  'GET /me/orders/*': (_) => {
    'id': orderId,
    'number': 'MS-10482',
    'status': 'out_for_delivery',
    'week': '2026-11-23',
    'plan_name': 'Classic for two',
    'meals_count': 3,
    'currency': 'TTD',
    'subtotal_minor': 54000,
    'discount_minor': 0,
    'delivery_fee_minor': 0,
    'tax_minor': 0,
    'total_minor': 54000,
    'address': {'recipient_name': 'Kavita Ramdass', 'line1': '12 Saddle Road', 'area': 'Maraval', 'city': 'Port of Spain', 'region': 'port-of-spain', 'region_label': 'Port of Spain', 'country': 'TT'},
    'delivery_window': null,
    'created_at': '2026-11-20T00:00:00Z',
    'items': [
      {'meal_id': meal(0)['id'], 'meal_version': 1, 'name': 'Curry chicken with roti', 'slug': 'curry-chicken', 'quantity': 2, 'servings': 2, 'allergens': ['gluten'], 'allergen_status': 'declared', 'dietary_tags': []},
      {'meal_id': meal(2)['id'], 'meal_version': 1, 'name': 'Channa and aloo', 'slug': 'channa-and-aloo', 'quantity': 1, 'servings': 2, 'allergens': [], 'allergen_status': 'declared', 'dietary_tags': ['vegetarian']},
    ],
    'subscription_id': subId,
    'billing_cycle_id': billingId,
    'payment_id': 'pay1',
    'subscription_cycle_id': cycleId,
  },
  'GET /me/orders/*/delivery': (_) => {
    'order_id': orderId,
    'stage': 'out_for_delivery',
    'headline': 'Your box is on its way',
    'detail': 'It left our kitchen this morning.',
    'service_date': '2026-11-23',
    'window': '2 pm – 6 pm',
    'address': {'recipient_name': 'Kavita Ramdass', 'line1': '12 Saddle Road', 'city': 'Port of Spain'},
    'confirmation_code': '4821',
    'code_explanation': 'Give this code to the driver.',
    'timeline': [
      {'key': 'confirmed', 'label': 'Order confirmed', 'done': true, 'at': '2026-11-20T12:00:00Z'},
      {'key': 'preparing', 'label': 'Preparing your box', 'done': true, 'at': '2026-11-22T10:00:00Z'},
      {'key': 'packed', 'label': 'Ready for delivery', 'done': true, 'at': '2026-11-23T11:00:00Z'},
      {'key': 'out', 'label': 'Out for delivery', 'done': false, 'at': null},
      {'key': 'delivered', 'label': 'Delivered', 'done': false, 'at': null},
    ],
    'issue': null,
    'can_reschedule': false,
    'reschedule_policy': '',
    'open_request': null,
    'history': [],
    'live_tracking': false,
  },
  'GET /me/orders/*/meals/*/recipe': (_) => {
    'order_id': orderId,
    'meal_id': meal(0)['id'],
    'meal_name': 'Curry chicken with roti',
    'meal_slug': 'curry-chicken',
    'week': '2026-11-23',
    'version': 2,
    'servings': 2,
    'serving_size': '450 g',
    'cook_minutes': 35,
    'ingredients': ['500 g chicken thighs', '2 tbsp green seasoning', '2 potatoes, diced', '4 roti skins'],
    'steps': [
      'Rub the chicken with the green seasoning and leave it for 10 minutes.',
      'Heat the oil in a deep pan. Add the curry powder and stir for a minute.',
      'Brown the chicken, then add the potato and 1 cup of water. Cover and simmer for 20 minutes.',
      'Warm the roti in a dry pan and serve with the curry.',
    ],
    'storage': 'Keep chilled. Cook within 3 days of delivery.',
    'allergens': ['gluten'],
    'may_contain': [],
    'cross_contact_note': '',
    'published_at': '2026-11-10T00:00:00Z',
    'effective_from': '2026-11-16',
  },
  'GET /chat/availability': (_) => {'enabled': true, 'online': true, 'message': '', 'response_expectation': 'We usually reply within an hour.'},
  'GET /me/chat/conversations': (_) => {
    'items': [
      {'id': convId, 'number': 204, 'status': 'open', 'subject': 'Missing roti skins', 'last_seq': 3, 'last_message': 'Thanks, we are sending a replacement.', 'last_message_at': '2026-11-21T15:02:00Z', 'unread': 1, 'team_read_seq': 2, 'feedback_rating': null, 'created_at': '2026-11-21T14:50:00Z', 'updated_at': '2026-11-21T15:02:00Z'},
    ],
  },
  'GET /me/chat/conversations/*': (_) => {'id': convId, 'number': 204, 'status': 'open', 'subject': 'Missing roti skins', 'last_seq': 3, 'last_message': '', 'last_message_at': null, 'unread': 0, 'team_read_seq': 2, 'feedback_rating': null, 'created_at': '2026-11-21T14:50:00Z', 'updated_at': '2026-11-21T15:02:00Z'},
  'GET /me/chat/conversations/*/messages': (o) => (o.queryParameters['after'] ?? 0) == 0
      ? {
          'items': [
            {'conversation_id': convId, 'seq': 1, 'author': 'customer', 'author_name': '', 'body': 'Hi, my box came without the roti skins.', 'client_message_id': 'c1-aaaaaaaa', 'created_at': '2026-11-21T14:50:00Z'},
            {'conversation_id': convId, 'seq': 2, 'author': 'agent', 'author_name': 'Ria', 'body': 'Sorry about that! Which order is it?', 'client_message_id': null, 'created_at': '2026-11-21T14:55:00Z'},
            {'conversation_id': convId, 'seq': 3, 'author': 'customer', 'author_name': '', 'body': 'MS-10482, this week.', 'client_message_id': 'c3-aaaaaaaa', 'created_at': '2026-11-21T14:58:00Z'},
            {'conversation_id': convId, 'seq': 4, 'author': 'agent', 'author_name': 'Ria', 'body': 'Thanks, we are sending a replacement with today\'s deliveries.', 'client_message_id': null, 'created_at': '2026-11-21T15:02:00Z'},
          ],
        }
      : {'items': []},
  'POST /me/chat/conversations/*/read': (_) => {'read': true},
  'GET /help/categories': (_) => {
    'items': [
      {'slug': 'delivery', 'name': 'Delivery', 'articles': 3},
      {'slug': 'your-box', 'name': 'Your box', 'articles': 2},
      {'slug': 'payments', 'name': 'Payments', 'articles': 2},
    ],
  },
  'GET /help/articles': (_) => {
    'items': [
      {'slug': 'when-does-my-box-arrive', 'category': 'delivery', 'title': 'When does my box arrive?', 'summary': 'Delivery days and windows.', 'published_at': '2026-11-01T00:00:00Z'},
      {'slug': 'skip-a-week', 'category': 'your-box', 'title': 'How do I skip a week?', 'summary': 'Pause from your subscription page.', 'published_at': '2026-11-01T00:00:00Z'},
    ],
  },
  'GET /me/notifications': (_) => {
    'items': [
      {'id': 'n1', 'type': 'delivery.update', 'title': 'Your box is on its way', 'body': 'It left our kitchen this morning.', 'link': '/orders/$orderId/delivery', 'read_at': null, 'created_at': '2026-11-23T11:05:00Z'},
      {'id': 'n2', 'type': 'chat.reply', 'title': 'New reply from Mise', 'body': 'Our team replied in conversation 204.', 'link': '/support/conversations/$convId', 'read_at': null, 'created_at': '2026-11-21T15:02:00Z'},
      {'id': 'n3', 'type': 'payment.succeeded', 'title': 'Payment received', 'body': 'Thank you. We received your payment for the week of 30 November.', 'link': '/subscription', 'read_at': '2026-11-21T00:00:00Z', 'created_at': '2026-11-20T09:00:00Z'},
    ],
    'unread': 2,
  },
  'GET /me/favorites': (_) => {
    'items': [meal(0), meal(2)],
    'meal_ids': [meal(0)['id'], meal(2)['id']],
  },
  'GET /me/profile': (_) => {'email': 'kavita@example.com', 'pending_email': null, 'first_name': 'Kavita', 'last_name': 'Ramdass', 'email_verified': true, 'phone': '+1 868 555 0100', 'contact_channel': 'email', 'language': 'en', 'delivery_country': 'TT', 'delivery_region': 'port-of-spain', 'delivery_availability': 'available', 'waitlist_linked': false, 'member_since': '2026-11-01T00:00:00Z'},
  'GET /me/notification-preferences': (_) => {
    'account_security': {'enabled': true, 'required': true},
    'topics': [
      {'topic': 'delivery_updates', 'label': 'Delivery updates', 'enabled': true, 'source': 'customer', 'updated_at': null},
      {'topic': 'meal_reminders', 'label': 'Meal choice reminders', 'enabled': true, 'source': 'customer', 'updated_at': null},
    ],
    'upcoming': [],
  },
  'GET /me/sessions': (_) => {
    'items': [
      {'id': 's1', 'current': true, 'client': 'mobile', 'user_agent': '', 'device_name': 'Mise app on Android', 'created_at': '2026-11-20T00:00:00Z', 'last_seen_at': '2026-11-21T15:00:00Z', 'expires_at': '2027-05-20T00:00:00Z'},
      {'id': 's2', 'current': false, 'client': 'web', 'user_agent': 'Chrome', 'device_name': '', 'created_at': '2026-11-10T00:00:00Z', 'last_seen_at': '2026-11-19T20:00:00Z', 'expires_at': '2026-12-10T00:00:00Z'},
    ],
  },
  'GET /me/account/deletion-request': (_) => {'request': null, 'notice': 'Your request is recorded and the Mise team will contact you. Nothing is deleted yet.'},
};
