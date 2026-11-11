-- Queries for the catalogue (sqlc).
--
-- Public queries return only published meals, and demo content only when
-- @allow_demo is true (CATALOG_DEMO_ENABLED). Nutrition is joined only when
-- verified. A meal's primary photograph is the one at position 0.

-- ---------------------------------------------------------------- public

-- name: ListPublishedMeals :many
-- diet_groups: each element is one requirement, written as the tags that
-- satisfy it separated by "|" ("vegetarian|vegan"); a meal must match every
-- requirement. exclude_allergens drops meals that declare any of them, and
-- meals whose allergens are not yet declared at all.
SELECT
    m.id, m.slug, m.name, m.summary, m.prep_minutes, m.cook_minutes, m.servings, m.protein,
    m.dietary_tags, m.allergens, m.allergen_status, m.is_demo, m.published_at,
    c.slug AS category_slug, c.name AS category_name,
    ch.slug AS chef_slug, ch.display_name AS chef_name,
    img.src AS image_src, img.alt AS image_alt, img.width AS image_width, img.height AS image_height,
    n.calories AS calories, n.protein_g AS protein_g,
    count(*) OVER () AS total
FROM meals m
JOIN meal_categories c ON c.id = m.category_id
LEFT JOIN chef_profiles ch ON ch.id = m.chef_id AND ch.status = 'published' AND (NOT ch.is_demo OR @allow_demo::bool)
LEFT JOIN meal_images img ON img.meal_id = m.id AND img.position = 0
LEFT JOIN meal_nutrition n ON n.meal_id = m.id AND n.status = 'verified'
WHERE m.status = 'published'
  AND (NOT m.is_demo OR @allow_demo::bool)
  AND (sqlc.narg(category)::text IS NULL OR c.slug = sqlc.narg(category)::text)
  AND NOT EXISTS (
      SELECT 1 FROM unnest(@diet_groups::text[]) AS g(alts)
      WHERE NOT (m.dietary_tags && string_to_array(g.alts, '|')))
  AND (sqlc.narg(max_minutes)::int IS NULL OR m.prep_minutes + m.cook_minutes <= sqlc.narg(max_minutes)::int)
  AND (sqlc.narg(protein)::text IS NULL OR m.protein = sqlc.narg(protein)::text)
  AND (sqlc.narg(chef)::text IS NULL OR ch.slug = sqlc.narg(chef)::text)
  AND (sqlc.narg(search)::text IS NULL
       OR (setweight(to_tsvector('english', m.name), 'A') ||
           setweight(to_tsvector('english', m.summary), 'B') ||
           setweight(to_tsvector('english', m.description), 'C'))
          @@ websearch_to_tsquery('english', sqlc.narg(search)::text)
       OR m.name ILIKE '%' || sqlc.narg(search_like)::text || '%')
  AND (cardinality(@exclude_allergens::text[]) = 0
       OR (m.allergen_status = 'declared' AND NOT (m.allergens && @exclude_allergens::text[])))
  AND (cardinality(@ids::uuid[]) = 0 OR m.id = ANY (@ids::uuid[]))
ORDER BY
    CASE WHEN @sort::text = 'quickest' THEN m.prep_minutes + m.cook_minutes END ASC,
    CASE WHEN @sort::text = 'name' THEN m.name END ASC,
    m.published_at DESC, m.id
LIMIT @lim OFFSET @off;

-- name: GetPublishedMealBySlug :one
SELECT
    m.*,
    c.slug AS category_slug, c.name AS category_name,
    ch.slug AS chef_slug, ch.display_name AS chef_name, ch.role_title AS chef_role_title,
    ch.bio AS chef_bio, ch.image_src AS chef_image_src, ch.image_alt AS chef_image_alt
FROM meals m
JOIN meal_categories c ON c.id = m.category_id
LEFT JOIN chef_profiles ch ON ch.id = m.chef_id AND ch.status = 'published' AND (NOT ch.is_demo OR @allow_demo::bool)
WHERE m.slug = @slug AND m.status = 'published' AND (NOT m.is_demo OR @allow_demo::bool);

-- name: IsMealVisible :one
SELECT EXISTS (
    SELECT 1 FROM meals
    WHERE id = @id AND status = 'published' AND (NOT is_demo OR @allow_demo::bool));

-- name: ListImages :many
SELECT * FROM meal_images WHERE meal_id = @meal_id ORDER BY position;

-- name: ListIngredients :many
SELECT * FROM meal_ingredients WHERE meal_id = @meal_id ORDER BY position;

-- name: GetVerifiedNutrition :one
SELECT * FROM meal_nutrition WHERE meal_id = @meal_id AND status = 'verified';

-- name: ListCategoriesWithCounts :many
SELECT c.slug, c.name, c.description, c.position,
       count(m.id) FILTER (WHERE m.status = 'published' AND (NOT m.is_demo OR @allow_demo::bool)) AS meal_count
FROM meal_categories c
LEFT JOIN meals m ON m.category_id = c.id
WHERE NOT c.is_demo OR @allow_demo::bool
GROUP BY c.id
ORDER BY c.position, c.name;

-- name: GetPublishedChefBySlug :one
SELECT * FROM chef_profiles
WHERE slug = @slug AND status = 'published' AND (NOT is_demo OR @allow_demo::bool);

-- name: ListPublishedChefs :many
SELECT slug, display_name FROM chef_profiles
WHERE status = 'published' AND (NOT is_demo OR @allow_demo::bool)
ORDER BY display_name;

-- ---------------------------------------------------------------- admin

-- name: AdminListMeals :many
SELECT
    m.id, m.slug, m.name, m.status, m.is_demo, m.published_at, m.updated_at, m.version,
    m.allergen_status, c.name AS category_name, ch.display_name AS chef_name,
    n.status AS nutrition_status,
    count(*) OVER () AS total
FROM meals m
JOIN meal_categories c ON c.id = m.category_id
LEFT JOIN chef_profiles ch ON ch.id = m.chef_id
LEFT JOIN meal_nutrition n ON n.meal_id = m.id
WHERE (sqlc.narg(status)::text IS NULL OR m.status = sqlc.narg(status)::text)
  AND (sqlc.narg(search_like)::text IS NULL OR m.name ILIKE '%' || sqlc.narg(search_like)::text || '%')
ORDER BY m.updated_at DESC, m.id
LIMIT @lim OFFSET @off;

-- name: GetMeal :one
SELECT * FROM meals WHERE id = @id;

-- name: CreateMeal :one
INSERT INTO meals (slug, name, summary, description, cooking_overview, category_id, chef_id, prep_minutes,
                   cook_minutes, servings, protein, dietary_tags, allergens, allergen_status, is_demo, created_by, updated_by)
VALUES (@slug, @name, @summary, @description, @cooking_overview, @category_id, sqlc.narg(chef_id), @prep_minutes,
        @cook_minutes, @servings, sqlc.narg(protein), @dietary_tags, @allergens, @allergen_status, @is_demo, sqlc.narg(actor), sqlc.narg(actor))
RETURNING *;

-- name: UpdateMeal :one
-- Succeeds only if nobody saved the meal since the editor loaded @version.
UPDATE meals SET
    slug = @slug, name = @name, summary = @summary, description = @description,
    cooking_overview = @cooking_overview, category_id = @category_id, chef_id = sqlc.narg(chef_id),
    prep_minutes = @prep_minutes, cook_minutes = @cook_minutes, servings = @servings,
    protein = sqlc.narg(protein), dietary_tags = @dietary_tags, allergens = @allergens,
    allergen_status = @allergen_status, is_demo = @is_demo,
    version = version + 1, updated_by = sqlc.narg(actor), updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: SetMealStatus :one
UPDATE meals SET
    status = @status::text,
    published_at = CASE WHEN @status::text = 'published' THEN COALESCE(published_at, now()) ELSE published_at END,
    version = version + 1, updated_by = sqlc.narg(actor), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteImages :exec
DELETE FROM meal_images WHERE meal_id = @meal_id;

-- name: InsertImage :exec
INSERT INTO meal_images (meal_id, position, src, alt, width, height, credit)
VALUES (@meal_id, @position, @src, @alt, sqlc.narg(width), sqlc.narg(height), sqlc.narg(credit));

-- name: DeleteIngredients :exec
DELETE FROM meal_ingredients WHERE meal_id = @meal_id;

-- name: InsertIngredient :exec
INSERT INTO meal_ingredients (meal_id, position, name, quantity) VALUES (@meal_id, @position, @name, sqlc.narg(quantity));

-- name: GetNutrition :one
SELECT * FROM meal_nutrition WHERE meal_id = @meal_id;

-- name: UpsertNutrition :one
INSERT INTO meal_nutrition (meal_id, calories, protein_g, carbs_g, fat_g, fibre_g, sodium_mg, status, verified_by, verified_at)
VALUES (@meal_id, @calories, @protein_g, @carbs_g, @fat_g, sqlc.narg(fibre_g), sqlc.narg(sodium_mg), @status, sqlc.narg(verified_by), sqlc.narg(verified_at))
ON CONFLICT (meal_id) DO UPDATE SET
    calories = EXCLUDED.calories, protein_g = EXCLUDED.protein_g, carbs_g = EXCLUDED.carbs_g, fat_g = EXCLUDED.fat_g,
    fibre_g = EXCLUDED.fibre_g, sodium_mg = EXCLUDED.sodium_mg, status = EXCLUDED.status,
    verified_by = EXCLUDED.verified_by, verified_at = EXCLUDED.verified_at, updated_at = now()
RETURNING *;

-- name: ListCategories :many
SELECT * FROM meal_categories ORDER BY position, name;

-- name: GetCategory :one
SELECT * FROM meal_categories WHERE id = @id;

-- name: GetCategoryBySlug :one
SELECT * FROM meal_categories WHERE slug = @slug;

-- name: CreateCategory :one
INSERT INTO meal_categories (slug, name, description, position, is_demo)
VALUES (@slug, @name, @description, @position, @is_demo)
RETURNING *;

-- name: ListChefs :many
SELECT * FROM chef_profiles ORDER BY display_name;

-- name: GetChef :one
SELECT * FROM chef_profiles WHERE id = @id;

-- name: GetChefBySlug :one
SELECT * FROM chef_profiles WHERE slug = @slug;

-- name: CreateChef :one
INSERT INTO chef_profiles (slug, display_name, role_title, bio, image_src, image_alt, status, is_demo)
VALUES (@slug, @display_name, sqlc.narg(role_title), @bio, sqlc.narg(image_src), sqlc.narg(image_alt), @status, @is_demo)
RETURNING *;

-- name: UpdateChef :one
UPDATE chef_profiles SET
    slug = @slug, display_name = @display_name, role_title = sqlc.narg(role_title), bio = @bio,
    image_src = sqlc.narg(image_src), image_alt = sqlc.narg(image_alt), status = @status,
    is_demo = @is_demo, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: GetMealBySlug :one
SELECT * FROM meals WHERE slug = @slug;

-- name: MealRefs :many
-- The facts other modules record about a meal (weekly menus, orders),
-- whatever its status. Callers decide what a status means for them.
SELECT id, slug, name, status, is_demo, version, servings, allergens, allergen_status, dietary_tags
FROM meals WHERE id = ANY(@ids::uuid[]);
