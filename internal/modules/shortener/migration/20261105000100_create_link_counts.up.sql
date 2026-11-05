-- Block writers during the backfill and trigger installation. Readers can
-- continue; the old COUNT-based application remains compatible during rollout.
BEGIN;
LOCK TABLE links IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE link_counts (
    bucket SMALLINT PRIMARY KEY CHECK (bucket >= 0 AND bucket < 64),
    total BIGINT NOT NULL CHECK (total >= 0)
);

INSERT INTO link_counts (bucket, total)
SELECT buckets.bucket, COALESCE(counts.total, 0)
FROM generate_series(0, 63) AS buckets(bucket)
LEFT JOIN (
    SELECT (id % 64)::SMALLINT AS bucket, COUNT(*) AS total
    FROM links
    WHERE deleted_at IS NULL
    GROUP BY id % 64
) AS counts USING (bucket);

-- Every caller walks buckets in ascending order before acquiring row locks.
-- Missing buckets are an invariant failure, never a successful lost update.
CREATE FUNCTION apply_link_count_delta(count_bucket SMALLINT, count_delta BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE link_counts SET total = total + count_delta WHERE bucket = count_bucket;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'missing link count bucket %', count_bucket;
    END IF;
END;
$$;

CREATE FUNCTION count_inserted_links()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    delta RECORD;
BEGIN
    FOR delta IN
        SELECT (id % 64)::SMALLINT AS bucket, COUNT(*) AS total
        FROM new_links
        WHERE deleted_at IS NULL
        GROUP BY id % 64
        ORDER BY bucket
    LOOP
        PERFORM apply_link_count_delta(delta.bucket, delta.total);
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE FUNCTION count_deleted_links()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    delta RECORD;
BEGIN
    FOR delta IN
        SELECT (id % 64)::SMALLINT AS bucket, -COUNT(*) AS total
        FROM old_links
        WHERE deleted_at IS NULL
        GROUP BY id % 64
        ORDER BY bucket
    LOOP
        PERFORM apply_link_count_delta(delta.bucket, delta.total);
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE FUNCTION count_updated_links()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    delta RECORD;
BEGIN
    -- Destination, activation and expiration updates have a zero net delta and
    -- do not write any counter. This also handles restoration and ID changes.
    FOR delta IN
        SELECT bucket, SUM(total)::BIGINT AS total
        FROM (
            SELECT (id % 64)::SMALLINT AS bucket, COUNT(*) AS total
            FROM new_links
            WHERE deleted_at IS NULL
            GROUP BY id % 64
            UNION ALL
            SELECT (id % 64)::SMALLINT AS bucket, -COUNT(*) AS total
            FROM old_links
            WHERE deleted_at IS NULL
            GROUP BY id % 64
        ) AS changes
        GROUP BY bucket
        HAVING SUM(total) <> 0
        ORDER BY bucket
    LOOP
        PERFORM apply_link_count_delta(delta.bucket, delta.total);
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE FUNCTION reset_link_counts()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    -- TRUNCATE holds ACCESS EXCLUSIVE on links, excluding concurrent writers.
    -- Recreate missing buckets too, preserving the fixed 64-row invariant.
    INSERT INTO link_counts (bucket, total)
    SELECT bucket, 0 FROM generate_series(0, 63) AS buckets(bucket)
    ORDER BY bucket
    ON CONFLICT (bucket) DO UPDATE SET total = 0;
    RETURN NULL;
END;
$$;

CREATE TRIGGER links_count_insert
AFTER INSERT ON links
REFERENCING NEW TABLE AS new_links
FOR EACH STATEMENT EXECUTE FUNCTION count_inserted_links();

CREATE TRIGGER links_count_delete
AFTER DELETE ON links
REFERENCING OLD TABLE AS old_links
FOR EACH STATEMENT EXECUTE FUNCTION count_deleted_links();

CREATE TRIGGER links_count_update
AFTER UPDATE ON links
REFERENCING OLD TABLE AS old_links NEW TABLE AS new_links
FOR EACH STATEMENT EXECUTE FUNCTION count_updated_links();

CREATE TRIGGER links_count_truncate
AFTER TRUNCATE ON links
FOR EACH STATEMENT EXECUTE FUNCTION reset_link_counts();
COMMIT;
