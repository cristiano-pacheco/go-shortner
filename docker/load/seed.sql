\set ON_ERROR_STOP on

BEGIN;
SELECT pg_advisory_xact_lock(73619822);
CREATE TABLE IF NOT EXISTS load_seed_progress (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    seeded BIGINT NOT NULL
);
INSERT INTO load_seed_progress (id, seeded) VALUES (TRUE, 0)
ON CONFLICT (id) DO NOTHING;

INSERT INTO links (code, destination, created_at, updated_at)
SELECT 'loadseed_' || n::text,
       'https://example.com/load-seed/' || n::text,
       NOW() - (n % 31536000) * INTERVAL '1 second',
       NOW() - (n % 31536000) * INTERVAL '1 second'
FROM generate_series((SELECT seeded + 1 FROM load_seed_progress WHERE id), :target::bigint) AS n;

UPDATE load_seed_progress SET seeded = GREATEST(seeded, :target::bigint) WHERE id;
COMMIT;
ANALYZE links;
