# Load testing

Requires Docker, Docker Compose, curl and tar. Run from the repository root.

```sh
LOAD_OPEN_BROWSER=0 LOAD_VUS=50 LOAD_DURATION=60s make test-load
```

The command builds the image, starts the stack, runs migrations, seeds one million links and runs k6. Reports go to `reports/load/run-*/`. Failed gates return a nonzero exit code. The stack stays running.

## Workloads

| `LOAD_WORKLOAD` | Requests per iteration |
| --- | --- |
| `mixed`, default | Create, get, list, update, redirect GET/HEAD, soft delete |
| `redirect` | One GET or HEAD over a seeded 1,000-code set |
| `list` | Page 1, 20 items, exact total |

Defaults are 10 virtual users for 2 minutes. Override with `LOAD_VUS` and `LOAD_DURATION`.

```sh
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=redirect LOAD_VUS=50 LOAD_DURATION=60s make test-load
```

`LOAD_RATE` enables constant-arrival-rate execution in iterations per second. Mixed iterations make seven requests. `LOAD_VUS` preallocates users. `LOAD_MAX_VUS` caps them at 200 by default.

```sh
# Offer a rate. This does not guarantee the application can sustain it.
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=redirect LOAD_RATE=10000 \
  LOAD_VUS=200 LOAD_MAX_VUS=200 LOAD_DURATION=3m make test-load
```

Gates require less than 1% failed requests, over 99% passing checks and p95 below 500 ms overall and per operation. Arrival-rate tests also require zero dropped iterations. These are test gates, not production SLOs. See [results and failed trials](performance.md).

## Settings

| Variable | Default | Effect |
| --- | --- | --- |
| `LOAD_SEED_COUNT` | `1000000` | Seed target. `0` skips insertion. Lowering it does not remove rows. |
| `LOAD_HOT_SET` | `1000` | Redirect working-set size. |
| `LOAD_WARMUP_REQUESTS` | `0` | Prime redirects and upstream connections before timing. |
| `LOAD_SKIP_BUILD` | `0` | `1` reuses the image. Build the current source first. |
| `LOAD_PUBLIC_CPUS` | `1.0` | Public CPU cap. Use `0.5` for the equal-total-CPU comparison. |
| `LOAD_MANAGEMENT_CPUS` | `0.5` | Management CPU cap. |

Each Go server has a 32 MiB limit, no swap, `GOMEMLIMIT=24MiB` and `GOMAXPROCS=1`. The combined 64 MiB excludes database, cache, proxy, generator and monitoring resources. Tracing is disabled. Metrics remain enabled.

Data persists. Mixed iterations leave soft-deleted rows. Failed iterations may leave active links. The script runs `ANALYZE` before each test.

Readiness and warmup requests do not count toward workload gates. Global HTTP totals include them. Business throughput uses `passes + fails` from `http_req_failed{phase:workload}`, divided by `state.testRunDurationMs / 1000`.

## Reports

| Files | Contents |
| --- | --- |
| `summary.json`, `index.html` | Rates, latency, checks and gates |
| `k6.log`, `services.log` | Generator output and service logs |
| `environment.txt`, `compose.yaml`, `dataset.txt` | Host, limits and row counts |
| `source.tar.gz`, `containers.txt` | Source inputs, image IDs, OOM and restart state |
| `resources.txt`, `prometheus.txt` | Sampled CPU, memory and application metrics |
| `counts.txt` | Exact-count reconciliation. A mismatch fails the run. |

Resource samples are not continuous peaks. The generator shares the host and may limit throughput.

## Dashboards

| Service | Default address | Port override |
| --- | --- | --- |
| Nginx | http://localhost:8088 | `LOAD_PORT` |
| Grafana | http://localhost:3001 | `LOAD_GRAFANA_PORT` |
| Prometheus | http://localhost:9092 | `LOAD_PROMETHEUS_PORT` |

Grafana requires no login. Set `LOAD_OPEN_BROWSER=0` to suppress browser opening. This stack has no Tempo service.

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

Reports remain. Only one load script can run per Docker daemon. After a forced termination, confirm no test is running before removing a stale lock:

```sh
docker network rm shortner-load-run-lock
```
