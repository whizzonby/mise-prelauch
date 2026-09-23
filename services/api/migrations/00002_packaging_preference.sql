-- +goose Up

-- The packaging a lead would prefer their kits to arrive in (an option value
-- from the waitlist form, e.g. "compostable"). Asked at signup, optional.
ALTER TABLE lead_preferences ADD COLUMN packaging_preference text;

CREATE INDEX lead_preferences_packaging_idx ON lead_preferences (packaging_preference)
    WHERE packaging_preference IS NOT NULL;

-- +goose Down

DROP INDEX lead_preferences_packaging_idx;
ALTER TABLE lead_preferences DROP COLUMN packaging_preference;
