# Load testing

Requires Docker, Docker Compose, curl and tar. Run from the repository root.

```sh
LOAD_OPEN_BROWSER=0 LOAD_VUS=50 LOAD_DURATION=60s make test-load
```

The command builds the image, starts the stack, runs migrations, seeds one million links and runs k6. Failed gates return a nonzero exit code. The stack stays running.

## Prepare separate populations

Use `LOAD_DATASET=1m ./scripts/prepare-load-dataset.sh` or `LOAD_DATASET=10m ./scripts/prepare-load-dataset.sh`. Each uses a separate Compose project and persistent PostgreSQL volume. The script builds the application image, starts PostgreSQL, applies migrations, and commits seed batches independently. Rerun the same command after interruption to resume. The progress row and each batch commit together. Do not run workload traffic while preparing a population.

The preparation report in `reports/load/datasets/` records progress, elapsed time, visible and physical rows, table and index sizes, PostgreSQL statistics, maintenance timestamps and per-bucket count reconciliation. A failed or interrupted run leaves its partial report. The target is a seed count, not a guarantee that other workload-created rows are absent. Use a fresh volume when an exact starting population is required. `LOAD_SEED_BATCH_SIZE` defaults to 10,000. `LOAD_SEED_COUNT` overrides the target for small tests; do not use that override for the campaign. A target below the existing seed progress is rejected.

Run `./test/load/prepare-dataset-integration.sh` to test interruption, resume, counter reconciliation and isolation with small populations. It creates and removes separate temporary Compose projects and volumes.

Set `LOAD_DATASET=1m` or `LOAD_DATASET=10m` on `make test-load` to select the corresponding volume. Without it, the historical `shortner-load` project remains the default. Compose projects expose the same ports, so stop the previous stack before starting another. To remove a selected population, use `docker compose -p shortner-load-1m -f docker/docker-compose.load.yaml down -v` (or substitute `shortner-load-10m`). This deletes that population.

## Workloads

| `LOAD_WORKLOAD` | Requests per iteration |
| --- | --- |
| `mixed`, default | Create, get, list, update, redirect GET/HEAD, soft delete |
| `read-heavy` | Create, get, list, update, 20 redirects to that same link, soft delete |
| `redirect` | GET/HEAD over a seeded 1,000-code set, with configurable repeats per code |
| `list` | Page 1, 20 items, exact total |

Defaults are 10 virtual users for 2 minutes. Override with `LOAD_VUS` and `LOAD_DURATION`.

The original mixed scenario remains seven requests per successful iteration. The new `read-heavy` scenario defaults to 25. Repeated reads happen after updating the destination and before deleting the link. Every response must return `302` with the updated destination. HEAD checks also require an empty body.

```sh
# Reuse each newly created link 20 times, in batches of 10.
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=read-heavy LOAD_VUS=20 \
  LOAD_REDIRECT_REPEATS=20 LOAD_REDIRECT_BATCH_SIZE=10 make test-load

# Stress cached redirects with repeated reads of each selected code.
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=redirect LOAD_VUS=10 \
  LOAD_REDIRECT_REPEATS=20 LOAD_REDIRECT_BATCH_SIZE=5 \
  LOAD_WARMUP_REQUESTS=1000 LOAD_DURATION=60s make test-load
```

`LOAD_REDIRECT_REPEATS` controls requests to the same code within an iteration. Defaults are 2 for mixed, 20 for read-heavy and 1 for redirect. `LOAD_REDIRECT_BATCH_SIZE` defaults to 1, preserving sequential baseline behavior. Higher values send concurrent reads within each iteration. A batch cannot exceed 200 requests. Repeats cannot exceed 1,000. Mixed and read-heavy require at least two redirects so both GET and HEAD run. These settings have no effect on list-only requests.

The redirect workload already reuses codes across iterations. Reducing `LOAD_HOT_SET` increases cache locality; a value of 1 intentionally tests a single hot code. Keep that result separate from the 1,000-code benchmark. Nginx never caches responses.

```sh
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=redirect LOAD_VUS=50 LOAD_DURATION=60s make test-load
```

`LOAD_RATE` enables constant-arrival-rate execution in **iterations per second**, not requests per second. A successful mixed iteration makes `5 + LOAD_REDIRECT_REPEATS` requests; read-heavy uses the same formula. A redirect iteration makes `LOAD_REDIRECT_REPEATS` requests. List makes one. `LOAD_VUS` preallocates users. `LOAD_MAX_VUS` caps them at 200 by default and must cover the preallocated users in arrival-rate mode.

```sh
# Offer a rate. This does not guarantee the application can sustain it.
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=redirect LOAD_RATE=10000 \
  LOAD_VUS=200 LOAD_MAX_VUS=200 LOAD_DURATION=3m make test-load
```

Gates require less than 1% failed requests, over 99% passing checks and p99 below 500 ms overall and per operation. Arrival-rate tests also require zero dropped iterations. Historical runs used p95 gates. These are test gates, not production SLOs. See [results and failed trials](performance.md).

Compare like-for-like request mixes, batch sizes, working sets, CPU limits and tracing settings. More cache reads can raise aggregate requests/s while reducing the proportion of writes. That describes a different workload, not an implementation speedup. More virtual users can also reduce throughput once the application or generator saturates.

## Settings

| Variable | Default | Effect |
| --- | --- | --- |
| `LOAD_SEED_COUNT` | `1000000` | Seed target. `0` skips insertion. Lowering it does not remove rows. |
| `LOAD_HOT_SET` | `1000` | Redirect working-set size. |
| `LOAD_WARMUP_REQUESTS` | `0` | Prime redirects and upstream connections before timing. |
| `LOAD_REDIRECT_REPEATS` | Workload-specific | Same-code reads per iteration, 1 to 1,000; at least 2 for mixed/read-heavy. |
| `LOAD_REDIRECT_BATCH_SIZE` | `1` | Concurrent redirects per user, 1 to 200; limited by remaining repeats. |
| `LOAD_SKIP_BUILD` | `0` | `1` reuses the image. Build the current source first. |
| `LOAD_PUBLIC_CPUS` | `1.0` | Public CPU cap. Use `0.5` for the equal-total-CPU comparison. |
| `LOAD_MANAGEMENT_CPUS` | `0.5` | Management CPU cap. |

Each Go server has a 32 MiB limit, no swap, `GOMEMLIMIT=24MiB` and `GOMAXPROCS=1`. The combined 64 MiB excludes database, cache, proxy, generator and monitoring resources. Metrics and tracing are enabled; the load configuration samples 1% of root traces and exports them over OTLP gRPC to Jaeger. A parent trace's sampling decision is respected. Sampling and export add overhead, so results are not directly comparable to runs with tracing disabled (including the historical results in [performance.md](performance.md)).

Data persists. Mixed iterations leave soft-deleted rows. Failed iterations may leave active links. The script runs `ANALYZE` before each test.

Readiness and warmup requests do not count toward workload gates. Global HTTP totals include them. Business throughput uses `passes + fails` from `http_req_failed{phase:workload}`, divided by `state.testRunDurationMs / 1000`. The summary records the actual request-mix settings and nominal requests per successful iteration. Workload and per-operation latency summaries include p99. k6 remote write exports p99 as well as p95 for comparison.

CPU and memory samples are not continuous peaks. The generator may limit throughput. Each run checks OOM state, restarts and exact-count consistency.

## Dashboards

| Service | Default address | Port override |
| --- | --- | --- |
| Nginx | http://localhost:8088 | `LOAD_PORT` |
| Grafana | http://localhost:3001 | `LOAD_GRAFANA_PORT` |
| Prometheus | http://localhost:9092 | `LOAD_PROMETHEUS_PORT` |
| Jaeger | http://localhost:16687 | `LOAD_JAEGER_PORT` |

Grafana requires no login. Set `LOAD_OPEN_BROWSER=0` to suppress browser opening. In Jaeger, select the `go-shortner` service and search for traces after generating traffic. The low sampling rate means short or low-volume runs may produce no traces; increase `sample_rate` in `docker/load/load.yaml` for diagnosis and restart the application containers. Grafana also provisions a Jaeger datasource for Explore. Jaeger uses in-memory storage capped at 10,000 traces; traces are lost when its container restarts. The current instrumentation includes use-case, repository and cache spans, but not HTTP server spans. Avoid exposing this local stack publicly; traces may contain sensitive details.

## Check migrations

With the load database running:

```sh
./scripts/check-link-counts.sh
```

This checks backfill, mutations, rollback, truncation and up/down migrations in a temporary schema. It preserves load data. Each load run also reconciles totals after completion.

## Cleanup

Remove the stack and its database volume:

```sh
docker compose -p shortner-load -f docker/docker-compose.load.yaml down -v --remove-orphans
```

Only one load script can run per Docker daemon. After a forced termination, confirm no test is running before removing a stale lock:

```sh
docker network rm shortner-load-run-lock
```
