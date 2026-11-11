-- Queries for favourites (sqlc). Keyed by the customer id from the session.

-- name: AddFavorite :execrows
INSERT INTO customer_favorites (customer_id, meal_id) VALUES (@customer_id, @meal_id)
ON CONFLICT (customer_id, meal_id) DO NOTHING;

-- name: RemoveFavorite :execrows
DELETE FROM customer_favorites WHERE customer_id = @customer_id AND meal_id = @meal_id;

-- name: ListFavoriteMealIDs :many
SELECT meal_id FROM customer_favorites WHERE customer_id = @customer_id ORDER BY created_at DESC;
