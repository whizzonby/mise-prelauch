-- Local development only: fills the waitlist with invented leads so the admin
-- dashboard has something to show. Every address is @example.com.
--   docker compose exec -T postgres psql -U mise -d mise < infrastructure/scripts/seed-dev.sql
-- Safe to run more than once: it replaces its own rows and touches nothing else.

BEGIN;

DELETE FROM leads WHERE email LIKE 'seed-%@example.com';

WITH names AS (
    SELECT ARRAY['Asha','Ben','Cara','Dev','Elena','Farah','Gabriel','Hema','Imani','Jamal','Kavita','Leon',
                 'Maya','Nikhil','Oriel','Priya','Quincy','Rhea','Sanjay','Tessa','Uma','Vikram','Wendy','Xavier',
                 'Yasmin','Zane'] AS first_names,
           ARRAY['port-of-spain','port-of-spain','port-of-spain','diego-martin-west','san-juan-east-west-corridor',
                 'arima-east','chaguanas-central','chaguanas-central','san-fernando-south','tobago','elsewhere'] AS locations,
           ARRAY['VERIFIED','VERIFIED','VERIFIED','VERIFIED','PENDING','QUALIFIED','UNSUBSCRIBED'] AS statuses
),
inserted AS (
    INSERT INTO leads (first_name, email, email_canonical, location, status, referral_code, email_verified_at,
                       consent_at, consent_version, created_at, updated_at)
    SELECT first_names[1 + (n % array_length(first_names, 1))],
           'seed-' || n || '@example.com',
           'seed-' || n || '@example.com',
           locations[1 + floor(random() * array_length(locations, 1))::int],
           st.status,
           'SEED' || lpad(n::text, 4, '0'),
           CASE WHEN st.status = 'PENDING' THEN NULL ELSE ts.at + interval '20 minutes' END,
           ts.at, '2026-09', ts.at, ts.at
    FROM generate_series(1, 140) AS n
    CROSS JOIN names
    -- More signups in recent days than a month ago.
    CROSS JOIN LATERAL (SELECT now() - (power(random(), 1.8) * 34 + n * 0) * interval '1 day' AS at) ts
    CROSS JOIN LATERAL (SELECT statuses[1 + floor(random() * array_length(statuses, 1) + n * 0)::int] AS status) st
    RETURNING id, created_at
)
INSERT INTO lead_preferences (lead_id, household_size, dietary_preferences, meal_interests, meals_per_week, cooking_frequency)
SELECT id,
       CASE WHEN random() < 0.2 THEN NULL ELSE 1 + floor(random() * 6)::int END,
       (SELECT COALESCE(array_agg(d), '{}') FROM unnest(ARRAY['vegetarian','vegan','pescatarian','halal','no-pork','gluten-free','dairy-free','high-protein']) d
         WHERE random() < 0.22 AND id IS NOT NULL),
       CASE WHEN random() < 0.4 THEN ARRAY['caribbean-classics','family'] ELSE '{}'::text[] END,
       CASE WHEN random() < 0.4 THEN 2 + floor(random() * 4)::int END,
       CASE WHEN random() < 0.4 THEN (ARRAY['most-days','few-times-a-week','weekends','rarely'])[1 + floor(random() * 4)::int] END
FROM inserted;

INSERT INTO lead_attribution (lead_id, touch, utm_source, utm_medium, utm_campaign, landing_page, created_at)
SELECT l.id, 'first', src.source,
       CASE src.source WHEN 'newsletter' THEN 'email' WHEN 'google' THEN 'cpc' ELSE 'social' END,
       'prelaunch', '/', l.created_at
FROM leads l
CROSS JOIN LATERAL (
    SELECT (ARRAY['instagram','instagram','instagram','facebook','whatsapp','newsletter','google',NULL,NULL,NULL])
           [1 + floor(random() * 10 + length(l.email) * 0)::int] AS source
) src
WHERE l.email LIKE 'seed-%@example.com' AND src.source IS NOT NULL;

-- Every fifth seeded lead was invited by an earlier one.
WITH pairs AS (
    SELECT referred.id AS referred_id, referrer.id AS referrer_id, referrer.referral_code, referred.status, referred.created_at
    FROM leads referred
    JOIN leads referrer ON referrer.email = 'seed-' || ((substring(referred.email FROM 'seed-(\d+)@')::int % 12) + 1) || '@example.com'
    WHERE referred.email LIKE 'seed-%@example.com'
      AND substring(referred.email FROM 'seed-(\d+)@')::int % 5 = 0
      AND referred.id <> referrer.id
),
linked AS (
    UPDATE leads SET referred_by = pairs.referrer_id FROM pairs WHERE leads.id = pairs.referred_id RETURNING leads.id
)
INSERT INTO referrals (referrer_lead_id, referred_lead_id, referral_code, status, created_at, converted_at)
SELECT referrer_id, referred_id, referral_code,
       CASE WHEN status = 'PENDING' THEN 'pending' ELSE 'converted' END,
       created_at,
       CASE WHEN status = 'PENDING' THEN NULL ELSE created_at + interval '20 minutes' END
FROM pairs;

COMMIT;
