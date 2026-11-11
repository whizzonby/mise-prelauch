-- +goose Up

-- Business decision, 11 November 2026: Mise delivers across Trinidad and
-- Tobago from launch. Every region in internal/vocab (the municipal
-- corporations of Trinidad, and Tobago) is an active delivery zone. To pause
-- a region later, set its status to 'planned' or 'unavailable'; addresses
-- there then stop showing "Mise delivers here".
INSERT INTO delivery_zones (country, region, status) VALUES
    ('TT', 'port-of-spain', 'active'),
    ('TT', 'san-fernando', 'active'),
    ('TT', 'arima', 'active'),
    ('TT', 'chaguanas', 'active'),
    ('TT', 'point-fortin', 'active'),
    ('TT', 'diego-martin', 'active'),
    ('TT', 'san-juan-laventille', 'active'),
    ('TT', 'tunapuna-piarco', 'active'),
    ('TT', 'sangre-grande', 'active'),
    ('TT', 'couva-tabaquite-talparo', 'active'),
    ('TT', 'mayaro-rio-claro', 'active'),
    ('TT', 'princes-town', 'active'),
    ('TT', 'penal-debe', 'active'),
    ('TT', 'siparia', 'active'),
    ('TT', 'tobago', 'active')
ON CONFLICT (country, region) DO NOTHING;

-- +goose Down

DELETE FROM delivery_zones WHERE country = 'TT' AND region IN (
    'port-of-spain', 'san-fernando', 'arima', 'chaguanas', 'point-fortin', 'diego-martin',
    'san-juan-laventille', 'tunapuna-piarco', 'sangre-grande', 'couva-tabaquite-talparo',
    'mayaro-rio-claro', 'princes-town', 'penal-debe', 'siparia', 'tobago');
