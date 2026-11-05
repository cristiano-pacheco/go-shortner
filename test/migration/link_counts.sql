-- Consumed in phases by scripts/check-link-counts.sh. The script sets an
-- isolated search_path and streams the actual production up/down migrations.
SELECT :'stage' = 'seed' AS seed,
       :'stage' = 'verify' AS verify,
       :'stage' = 'down' AS down,
       :'stage' = 'truncate' AS truncate
\gset

\if :seed
INSERT INTO links (code, destination, deleted_at)
SELECT 'check_' || n, 'https://example.com',
       CASE WHEN n % 7 = 0 THEN NOW() ELSE NULL END
FROM generate_series(1, 130) AS n;

\elif :verify
CREATE FUNCTION assert_link_counts()
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    IF (SELECT COUNT(*) FROM link_counts) <> 64 THEN
        RAISE EXCEPTION 'bucket count mismatch';
    END IF;
    IF EXISTS (
        SELECT 1 FROM link_counts AS counters
        LEFT JOIN (
            SELECT (id % 64)::SMALLINT AS bucket, COUNT(*) AS total
            FROM links WHERE deleted_at IS NULL GROUP BY id % 64
        ) AS actual USING (bucket)
        WHERE counters.total <> COALESCE(actual.total, 0)
    ) THEN
        RAISE EXCEPTION 'counter mismatch';
    END IF;
END;
$$;

-- Backfill includes existing non-deleted rows, not only future inserts.
SELECT assert_link_counts();
CREATE TEMP TABLE counter_versions AS
SELECT bucket, ctid::TEXT AS version FROM link_counts;
UPDATE links SET destination = 'https://example.org', active = FALSE, expires_at = NOW();
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM link_counts AS counters
        JOIN counter_versions AS versions USING (bucket)
        WHERE counters.ctid::TEXT <> versions.version
    ) THEN
        RAISE EXCEPTION 'zero-delta update wrote counter';
    END IF;
END; $$;
SELECT assert_link_counts();

INSERT INTO links (code, destination)
SELECT 'added_' || n, 'https://example.net' FROM generate_series(1, 130) AS n;
SELECT assert_link_counts();
UPDATE links SET deleted_at = NOW() WHERE id % 3 = 0;
SELECT assert_link_counts();
UPDATE links SET deleted_at = NULL WHERE id % 5 = 0;
SELECT assert_link_counts();
UPDATE links SET id = 1001 WHERE id = 1;
SELECT assert_link_counts();
DELETE FROM links WHERE id % 4 = 0;
SELECT assert_link_counts();

BEGIN;
UPDATE links SET deleted_at = NOW();
SELECT assert_link_counts();
ROLLBACK;
SELECT assert_link_counts();
BEGIN;
TRUNCATE links RESTART IDENTITY;
SELECT assert_link_counts();
ROLLBACK;
SELECT assert_link_counts();

INSERT INTO links (code, destination) VALUES ('added_2', 'https://example.net')
ON CONFLICT (code) DO UPDATE SET deleted_at = NULL;
SELECT assert_link_counts();

-- Corruption must reject the write and leave no partially inserted link.
BEGIN;
DELETE FROM link_counts WHERE bucket = 0;
DO $$
DECLARE rejected BOOLEAN := FALSE;
BEGIN
    BEGIN
        INSERT INTO links (id, code, destination)
        VALUES (640, 'missing_bucket', 'https://example.net');
    EXCEPTION WHEN raise_exception THEN
        rejected := TRUE;
    END;
    IF NOT rejected THEN
        RAISE EXCEPTION 'missing bucket accepted write';
    END IF;
    IF EXISTS (SELECT 1 FROM links WHERE id = 640) THEN
        RAISE EXCEPTION 'failed statement persisted';
    END IF;
    IF EXISTS (SELECT SUM(total)::BIGINT FROM link_counts HAVING COUNT(*) = 64) THEN
        RAISE EXCEPTION 'incomplete buckets accepted total query';
    END IF;
END;
$$;
ROLLBACK;
SELECT assert_link_counts();
DROP FUNCTION assert_link_counts();

\elif :down
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM links) THEN
        RAISE EXCEPTION 'down migration lost links';
    END IF;
    IF to_regclass('link_counts') IS NOT NULL
       OR to_regprocedure('apply_link_count_delta(smallint,bigint)') IS NOT NULL
       OR to_regprocedure('count_inserted_links()') IS NOT NULL
       OR to_regprocedure('count_deleted_links()') IS NOT NULL
       OR to_regprocedure('count_updated_links()') IS NOT NULL
       OR to_regprocedure('reset_link_counts()') IS NOT NULL THEN
        RAISE EXCEPTION 'down migration left owned objects';
    END IF;
END; $$;

\elif :truncate
TRUNCATE links RESTART IDENTITY;
DO $$ BEGIN
    IF (SELECT SUM(total) FROM link_counts) <> 0
       OR (SELECT COUNT(*) FROM link_counts) <> 64 THEN
        RAISE EXCEPTION 'truncate did not reset 64 buckets';
    END IF;
END; $$;

\else
DO $$ BEGIN RAISE EXCEPTION 'unknown migration contract stage'; END; $$;
\endif
