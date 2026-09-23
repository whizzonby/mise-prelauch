-- Runs once, when the local Postgres volume is first created.
-- A separate database keeps Go integration tests away from development data.
CREATE DATABASE mise_test OWNER mise;
