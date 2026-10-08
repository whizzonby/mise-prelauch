-- +goose Up

-- The waitlist form asks for a last name as well as a first name. Leads who
-- joined before it did have an empty last name.
ALTER TABLE leads ADD COLUMN last_name text NOT NULL DEFAULT '' CHECK (char_length(last_name) <= 80);

-- +goose Down

ALTER TABLE leads DROP COLUMN last_name;
