# Architecture decisions

## Module and composition

The shortener module owns link management and public resolution. Shared infrastructure provides configuration, HTTP, PostgreSQL and Redis without owning link policy. Bricks loads configuration; Fx composes infrastructure, adapters and concrete use cases. HTTP handlers call those use cases without decorators. HTTP DTOs, use-case inputs and outputs, and persistence models have separate contracts. Repositories handle database access, cache adapters handle Redis, and use cases decide availability.

PostgreSQL and Redis have separate Fx modules and lifecycle owners. The repository receives the PostgreSQL resource; the redirect cache receives the Redis client. Both dependencies must be available at startup. The server composes the Bricks validator and tracing modules, an application-owned metrics registry, the administrative metrics listener and the HTTP server. Use cases validate structural inputs with Bricks and retain domain checks for link policy.

The shortener Fx module contributes its embedded SQL files to the `migration_filesystems` group. The migration runner collects that group and rejects duplicate filenames and versions. The explicit `migrate` command runs migrations independently of HTTP startup.

PostgreSQL assigns `BIGSERIAL` management IDs. An eight-byte integer uses less identifier storage than a 16-byte UUID; this is not a measured throughput claim. Public codes are independent of IDs. An unconditional unique constraint reserves each stored code, including after soft deletion.

A single configured API key protects management routes. There are no user accounts or per-link permissions. Public redirect routes do not require the key.

## Redirect cache

PostgreSQL is authoritative. Redis stores positive lookup data, including lifecycle information. On a cache miss or Redis error during operation, the resolver reads PostgreSQL. Redis must be available when the application starts. A database failure without usable cached data is a server error, not a missing link.

Updates and deletions attempt cache invalidation after the database mutation commits. Cache failures do not undo a successful database operation. A cache entry expires no later than the link's expiration and at most 60 seconds after the database lookup begins. Cache hits do not renew it. A delayed concurrent fill retains that original deadline instead of starting another 60 seconds. The resolver also checks expiration and active state on hits.

This permits a stale destination or active state for up to 60 seconds after a committed change, including when invalidation fails or a concurrent fill races with it. It does not guarantee immediate revocation.

## Scope

The application has no translation catalog, analytics, frontend or application-level rate limiting. English messages are returned directly. The specification calls for integration tests rather than unit tests, although the current repository also contains unit tests. The end-to-end suite exercises the HTTP application with real PostgreSQL and Redis. Proxy-level abuse protection is needed before internet exposure; URL validation permits localhost and private destinations and is not phishing protection.
