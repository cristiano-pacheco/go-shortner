# Architecture decisions

## Module and composition

The shortener module owns link management and public resolution. Shared infrastructure provides configuration, HTTP, PostgreSQL and Redis without owning link policy. Bricks loads configuration; Fx composes infrastructure, adapters and concrete use cases. HTTP handlers call those use cases without decorators. HTTP DTOs, use-case inputs and outputs, and persistence models have separate contracts. Repositories handle database access, cache adapters handle Redis, and use cases decide availability.

Two commands build separate Fx graphs. `management-server` provides authenticated CRUD and pagination. `public-server` provides GET/HEAD redirects without management handlers or authentication. Both expose `/health` and a separate metrics listener. PostgreSQL and Redis must be available at startup. Management writes invalidate the Redis keys read by public instances. Use cases validate inputs with Bricks and enforce link policy.

The shortener Fx module contributes its embedded SQL files to the `migration_filesystems` group. The migration runner collects that group and rejects duplicate filenames and versions. The explicit `migrate` command runs migrations independently of HTTP startup.

PostgreSQL assigns `BIGSERIAL` management IDs. An eight-byte integer uses less identifier storage than a 16-byte UUID; this is not a measured throughput claim. Public codes are independent of IDs. An unconditional unique constraint reserves each stored code, including after soft deletion.

A single configured API key protects management routes. There are no user accounts or per-link permissions. Public redirect routes do not require the key.

## Redirect cache

PostgreSQL is authoritative. Redis stores positive lookup data, including lifecycle information. On a cache miss or Redis error during operation, the resolver reads PostgreSQL. Redis must be available when the application starts. A database failure without usable cached data is a server error, not a missing link.

Updates and deletions attempt cache invalidation after the database mutation commits. Cache failures do not undo a successful database operation. A cache entry expires no later than the link's expiration and at most 60 seconds after the database lookup begins. Cache hits do not renew it. A delayed concurrent fill retains that original deadline instead of starting another 60 seconds. The resolver also checks expiration and active state on hits.

This permits a stale destination or active state for up to 60 seconds after a committed change, including when invalidation fails or a concurrent fill races with it. It does not guarantee immediate revocation.

## Exact pagination totals

Listing sums 64 PostgreSQL counters, partitioned by `id % 64`, instead of counting every link. Statement triggers update them on insertion, deletion and restoration. Other field updates leave them unchanged. `TRUNCATE` resets them. Rollback undoes counter changes.

The existing `(created_at DESC, id DESC)` index supplies the page. Total and items use separate queries, not one snapshot. Bucket locks follow ascending order. Bulk writes can contend, and multi-statement transactions may need deadlock retries. Migration backfill blocks writes. Imports that disable triggers must rebuild and verify counters.

Verify outside the request path:

```sql
SELECT (SELECT sum(total) FROM link_counts) AS maintained,
       (SELECT count(*) FROM links WHERE deleted_at IS NULL) AS actual;
```

## Scope

The application has no translation catalog, analytics, frontend or application-level rate limiting. English messages are returned directly. The specification calls for integration tests rather than unit tests, although the current repository also contains unit tests. The end-to-end suite exercises the HTTP application with real PostgreSQL and Redis. Proxy-level abuse protection is needed before internet exposure; URL validation permits localhost and private destinations and is not phishing protection.
