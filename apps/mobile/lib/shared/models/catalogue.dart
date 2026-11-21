import '../../core/api/api_client.dart';

/// `Term` and `Ref` in the API: a key and a label.
class Term {
  const Term(this.key, this.label);

  factory Term.fromJson(Json j) => Term(str(j, 'key', str(j, 'slug')), str(j, 'label', str(j, 'name')));

  final String key;
  final String label;

  Json toJson() => {'key': key, 'label': label};
}

class MealImage {
  const MealImage({required this.src, required this.alt, this.credit});

  factory MealImage.fromJson(Json j) => MealImage(src: str(j, 'src'), alt: str(j, 'alt'), credit: strOrNull(j, 'credit'));

  final String src;
  final String alt;
  final String? credit;

  Json toJson() => {'src': src, 'alt': alt, 'credit': credit};
}

/// Declared allergens. Never a statement that a meal is free of anything.
class AllergenInfo {
  const AllergenInfo({required this.declared, required this.contains, required this.notice});

  factory AllergenInfo.fromJson(Json j) =>
      AllergenInfo(declared: str(j, 'status') == 'declared', contains: asList(j['contains'], Term.fromJson), notice: str(j, 'notice'));

  final bool declared;
  final List<Term> contains;
  final String notice;

  Json toJson() => {'status': declared ? 'declared' : 'unconfirmed', 'contains': [for (final t in contains) t.toJson()], 'notice': notice};
}

/// One meal as listed (`MealCard`, `MenuMeal`).
class Meal {
  const Meal({
    required this.id,
    required this.slug,
    required this.name,
    required this.summary,
    required this.category,
    required this.chef,
    required this.prepMinutes,
    required this.cookMinutes,
    required this.totalMinutes,
    required this.servings,
    required this.protein,
    required this.dietary,
    required this.allergens,
    required this.image,
    required this.calories,
    required this.proteinG,
    required this.isDemo,
    this.maxPerCustomer,
    this.raw = const {},
  });

  factory Meal.fromJson(Json j) {
    final nut = j['nutrition'] is Map ? asMap(j['nutrition']) : null;
    return Meal(
      id: str(j, 'id'),
      slug: str(j, 'slug'),
      name: str(j, 'name'),
      summary: str(j, 'summary'),
      category: j['category'] is Map ? Term.fromJson(asMap(j['category'])) : null,
      chef: j['chef'] is Map ? Term.fromJson(asMap(j['chef'])) : null,
      prepMinutes: integer(j, 'prep_minutes'),
      cookMinutes: integer(j, 'cook_minutes'),
      totalMinutes: integer(j, 'total_minutes'),
      servings: integer(j, 'servings'),
      protein: j['protein'] is Map ? Term.fromJson(asMap(j['protein'])) : null,
      dietary: asList(j['dietary'], Term.fromJson),
      allergens: AllergenInfo.fromJson(asMap(j['allergens'])),
      image: j['image'] is Map ? MealImage.fromJson(asMap(j['image'])) : null,
      calories: nut == null ? null : intOrNull(nut, 'calories'),
      proteinG: nut == null ? null : doubleOrNull(nut, 'protein_g'),
      isDemo: boolean(j, 'is_demo'),
      maxPerCustomer: intOrNull(j, 'max_per_customer'),
      raw: j,
    );
  }

  final String id;
  final String slug;
  final String name;
  final String summary;
  final Term? category;
  final Term? chef;
  final int prepMinutes;
  final int cookMinutes;
  final int totalMinutes;
  final int servings;
  final Term? protein;
  final List<Term> dietary;
  final AllergenInfo allergens;
  final MealImage? image;

  /// Verified figures only; null when not verified.
  final int? calories;
  final double? proteinG;
  final bool isDemo;
  final int? maxPerCustomer;

  /// The JSON this was read from, for the offline cache.
  final Json raw;

  /// "35 min · Serves 2" — times are the approved ones from the API.
  String get facts => [if (totalMinutes > 0) '$totalMinutes min', if (servings > 0) 'Serves $servings'].join(' · ');
}

class Nutrition {
  const Nutrition({required this.calories, required this.proteinG, required this.carbsG, required this.fatG, this.fibreG, this.sodiumMg});

  factory Nutrition.fromJson(Json j) => Nutrition(
    calories: integer(j, 'calories'),
    proteinG: doubleOrNull(j, 'protein_g') ?? 0,
    carbsG: doubleOrNull(j, 'carbs_g') ?? 0,
    fatG: doubleOrNull(j, 'fat_g') ?? 0,
    fibreG: doubleOrNull(j, 'fibre_g'),
    sodiumMg: intOrNull(j, 'sodium_mg'),
  );

  final int calories;
  final double proteinG;
  final double carbsG;
  final double fatG;
  final double? fibreG;
  final int? sodiumMg;
}

class ChefSummary {
  const ChefSummary({required this.name, required this.roleTitle, required this.bio, this.image});

  factory ChefSummary.fromJson(Json j) => ChefSummary(
    name: str(j, 'name'),
    roleTitle: strOrNull(j, 'role_title'),
    bio: str(j, 'bio'),
    image: j['image'] is Map ? MealImage.fromJson(asMap(j['image'])) : null,
  );

  final String name;
  final String? roleTitle;
  final String bio;
  final MealImage? image;
}

class Ingredient {
  const Ingredient(this.name, this.quantity);

  final String name;
  final String? quantity;
}

/// `MealDetail`: the card plus verified detail.
class MealDetail {
  const MealDetail({
    required this.meal,
    required this.description,
    required this.cookingOverview,
    required this.images,
    required this.ingredients,
    required this.nutrition,
    required this.chef,
  });

  factory MealDetail.fromJson(Json j) => MealDetail(
    meal: Meal.fromJson(j),
    description: str(j, 'description'),
    cookingOverview: str(j, 'cooking_overview'),
    images: asList(j['images'], MealImage.fromJson),
    ingredients: asList(j['ingredients'], (i) => Ingredient(str(i, 'name'), strOrNull(i, 'quantity'))),
    nutrition: j['nutrition_detail'] is Map ? Nutrition.fromJson(asMap(j['nutrition_detail'])) : null,
    chef: j['chef_detail'] is Map ? ChefSummary.fromJson(asMap(j['chef_detail'])) : null,
  );

  final Meal meal;
  final String description;
  final String cookingOverview;
  final List<MealImage> images;
  final List<Ingredient> ingredients;
  final Nutrition? nutrition;
  final ChefSummary? chef;
}

class MealCategory {
  const MealCategory({required this.slug, required this.name, required this.mealCount});

  factory MealCategory.fromJson(Json j) => MealCategory(slug: str(j, 'slug'), name: str(j, 'name'), mealCount: integer(j, 'meal_count'));

  final String slug;
  final String name;
  final int mealCount;
}

class MealPage {
  const MealPage({required this.items, required this.total, required this.page, required this.pageSize});

  factory MealPage.fromJson(Json j) => MealPage(
    items: asList(j['items'], Meal.fromJson),
    total: integer(j, 'total'),
    page: integer(j, 'page', 1),
    pageSize: integer(j, 'page_size', 24),
  );

  final List<Meal> items;
  final int total;
  final int page;
  final int pageSize;

  bool get hasMore => page * pageSize < total;
}

class MenuSummary {
  const MenuSummary({
    required this.id,
    required this.week,
    required this.weekLabel,
    required this.title,
    required this.cutoffAt,
    required this.isOpen,
    required this.mealCount,
  });

  factory MenuSummary.fromJson(Json j) => MenuSummary(
    id: str(j, 'id'),
    week: str(j, 'week'),
    weekLabel: str(j, 'week_label'),
    title: strOrNull(j, 'title'),
    cutoffAt: time(j, 'cutoff_at'),
    isOpen: boolean(j, 'is_open'),
    mealCount: integer(j, 'meal_count'),
  );

  final String id;
  final String week;
  final String weekLabel;
  final String? title;
  final DateTime? cutoffAt;
  final bool isOpen;
  final int mealCount;
}

class WeeklyMenu {
  const WeeklyMenu({required this.summary, required this.note, required this.meals, required this.eligiblePlanIds});

  factory WeeklyMenu.fromJson(Json j) => WeeklyMenu(
    summary: MenuSummary.fromJson(j),
    note: strOrNull(j, 'note'),
    meals: asList(j['meals'], Meal.fromJson),
    eligiblePlanIds: strings(j, 'eligible_plan_ids'),
  );

  final MenuSummary summary;
  final String? note;
  final List<Meal> meals;
  final List<String> eligiblePlanIds;
}

/// Fixed value lists for filters (dietary keys, allergens, proteins).
class Reference {
  const Reference({required this.dietary, required this.mealAllergens, required this.proteins, required this.crossContactNotice});

  factory Reference.fromJson(Json j) => Reference(
    dietary: asList(j['dietary'], Term.fromJson),
    mealAllergens: asList(j['meal_allergens'], Term.fromJson),
    proteins: asList(j['proteins'], Term.fromJson),
    crossContactNotice: str(j, 'cross_contact_notice'),
  );

  final List<Term> dietary;
  final List<Term> mealAllergens;
  final List<Term> proteins;
  final String crossContactNotice;
}
