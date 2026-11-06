# Reproduce the 10-million-link measurements

This is the procedure for the results in the [README](../README.md). Selected measurements and conditions are published in [management-capacity-10m.json](management-capacity-10m.json). Raw output stays in the local `reports/load/` directory; it is not committed.

These are single repetitions. They do not complete the original campaign, which required three passing repetitions per scenario. A failed scenario remains a failed result.

## Requirements and conditions

- Docker, Docker Compose, Python 3, curl and sufficient disk space. Management JSON events use about 1 GiB per five-minute run at 1,010 requests/s; preparation and the baseline require additional database storage.
- The recorded application revision is `e01b795188f02af193d62930a8a1b1db47e54149`. The runners and documentation in this checkout add the telemetry and reproduction commands used here. Use these runners rather than the older runner that only exported JSON.
- Recorded tooling: Docker 29.2.1, Compose 5.1.0, k6 0.57.0, PostgreSQL 17.5, Prometheus 3.5.0 and Grafana 12.1.0. Image IDs and load-script hashes are retained with the measurements.
- The original measurements give each Go server 0.5 CPU, 32 MiB, no swap, `GOMEMLIMIT=24MiB` and `GOMAXPROCS=1`. The later GET-only ramp gives the public server 64 MiB and `GOMEMLIMIT=48MiB`; management remains at 32 MiB. Both use 10% root trace sampling. These limits exclude PostgreSQL, Redis, Nginx, k6 and monitoring, which have no explicit container limits here.
- A separate historical stack remained running during these measurements. Background workloads, available resources and telemetry overhead can change the results. Repeating these commands is not a guarantee of identical numbers.

Run all commands from the repository root. Do not run generators concurrently. Restoration replaces `links` in the selected test database and clears that stack's Redis. Use test data only.

## 1. Select ports and prepare the population

These alternate ports match the recorded session and allow the historical stack to remain running:

```sh
export LOAD_DATASET=10m
export LOAD_PORT=18088
export LOAD_GRAFANA_PORT=13001
export LOAD_PROMETHEUS_PORT=19092
export LOAD_JAEGER_PORT=26687

./scripts/prepare-load-dataset.sh
```

This builds the application, migrates and seeds the isolated `shortner-load-10m` project. Preparation resumes if interrupted. Inspect the new `reports/load/datasets/10m-*/preparation.txt`: the seed target, visible rows and maintained count must reconcile. A seed target of ten million does not remove workload-created rows from an existing volume. Use a separate, fresh test population when creating a new baseline; do not silently snapshot a modified dataset.

## 2. Create a baseline and run creation

For a newly prepared population without a management baseline:

```sh
LOAD_MANAGEMENT_PREPARE=snapshot SCENARIO=create \
  LOAD_SKIP_BUILD=1 ./scripts/run-management-load.sh
```

If the baseline already exists, use this instead. This is the creation command used in the recorded session:

```sh
SCENARIO=create LOAD_SKIP_BUILD=1 ./scripts/run-management-load.sh
```

Do not run both commands to represent a single repetition. Each restores the baseline, verifies counts, waits for readiness, sends 1,000 warmup requests, then offers 1,010 requests/s for 300 seconds. k6 preallocates 200 virtual users and can grow to 1,000.

## 3. Reproduce the other completed isolated scenarios

```sh
for scenario in get list list_deep update; do
  SCENARIO="$scenario" LOAD_SKIP_BUILD=1 ./scripts/run-management-load.sh ||
    printf 'Scenario failed: %s. Keep its reports.\n' "$scenario"
done
```

Each invocation restores the same baseline and writes a separate `reports/load/management-*` directory. Every isolated scenario offers 1,010 requests/s for five minutes. The user stopped the remaining long matrix. Isolated deletion was interrupted and management-only `mixed` was not run; they have no completed isolated result. Deletion is covered by the final full-API mix below. The runner also supports `SCENARIO=delete` and `SCENARIO=mixed` for future work, but those are not additional completed measurements here.

Lookup IDs come from a deterministic, broad pool of 100,000 real IDs. Update and delete use separate reserved pools, without reusing records during a run. Listing uses 20 items and an exact total; `list_deep` requests page 450,001 on the ten-million-link baseline. Mixed listing cannot independently verify an exact total while concurrent writes occur.

Warmup is sequential and outside the measured window. Deep pagination can make it lengthy: the recorded 1,000-request warmup took roughly 13.5 minutes. The five-minute measurement begins afterward. Do not shorten it without recording the changed procedure.

## 4. Stress the public redirect

```sh
DURATION_SECONDS=60 RATE=1500 \
RATE_STAGES='[{"target":1500,"duration":"10s"},{"target":2250,"duration":"5s"},{"target":2250,"duration":"15s"},{"target":3000,"duration":"5s"},{"target":3000,"duration":"25s"}]' \
LOAD_SKIP_BUILD=1 ./scripts/run-api-load.sh redirect
```

The runner restores the baseline, uses 1,000 seeded codes, sends 1,000 warmup requests, then uses one **60-second ramping-arrival-rate execution**: 30,000 req/s for 10 seconds, a five-second ramp to 45,000, 15 seconds at 45,000, a five-second ramp to 60,000, then 25 seconds at 60,000. The rates are offered requests, not achieved capacity. Each iteration makes 20 alternating GET/HEAD redirects in two sequential batches of ten. Stage targets are in iterations/s (1,500, 2,250 and 3,000), multiplied by 20. There are 200 preallocated virtual users and a maximum of 1,000. It validates `302`, destination and empty HEAD bodies without following redirects to external sites.

This is a hot-set workload, not uniformly distributed reads across all ten million codes. It exports remote-write metrics and retains `summary.json`, but omits JSON event streaming to avoid that additional disk-writing cost at the higher rate. Its instrumentation differs from the management runs. The runner writes a `reports/load/redirect-stress-*` directory.

## 5. Exercise the complete API together

```sh
DURATION_SECONDS=60 LOAD_SKIP_BUILD=1 ./scripts/run-api-load.sh mixed
```

After restoring the baseline, the runner offers 200 iterations/s for 60 seconds. Each successful iteration performs create, get, list, update, redirect GET, redirect HEAD and delete: **1,400 offered requests/s**, 200 per operation. Reads target the link created in the same iteration; this differs from the isolated lookup's broad ID pool. This additional workload has readiness checks and no workload warmup. Both extra runs are short exploratory measurements, not five-minute capacity validations. It exports both JSON events and remote-write metrics to a `reports/load/full-api-*` directory.

## 6. Latest GET-only ramp with 64 MiB

After the short stress and full-API run, the user authorized one additional five-minute execution: only `GET /{code}`, gradual arrival-rate increases and 64 MiB for the public server. CPU remains 0.5 and root trace sampling remains 10%. The public Go memory target changes to 48 MiB. This configuration differs from the failed 32-MiB GET/HEAD stress in several ways; memory alone is not a controlled explanation for the difference.

```sh
LOAD_PUBLIC_MEMORY=64m LOAD_PUBLIC_GOMEMLIMIT=48MiB \
DURATION_SECONDS=300 RATE=500 VUS=100 MAX_VUS=200 \
REDIRECT_METHOD=GET REDIRECT_REPEATS=1 REDIRECT_BATCH_SIZE=1 \
RATE_STAGES='[{"target":1000,"duration":"30s"},{"target":2000,"duration":"30s"},{"target":4000,"duration":"30s"},{"target":8000,"duration":"30s"},{"target":12000,"duration":"30s"},{"target":16000,"duration":"30s"},{"target":22000,"duration":"30s"},{"target":30000,"duration":"30s"},{"target":40000,"duration":"30s"},{"target":50000,"duration":"30s"}]' \
LOAD_SKIP_BUILD=1 ./scripts/run-api-load.sh redirect
```

Each iteration sends exactly one GET, without following the redirect. The warmup sends 1,000 GETs over the same 1,000-code hot set, in batches of 200, outside timing. During the measured window there are no request batches: concurrency comes from the arrival-rate executor, preallocating 100 VUs and allowing 200. The offer increases linearly through each 30-second stage, starting at 500 and ending at 50,000 requests/s. These are not ten constant-rate plateaus.

Remote write stays active and JSON event streaming stays disabled for this high-rate run. Preserve the k6 summary and sample Prometheus over the exact run interval to show successful request rates, HTTP errors, target availability and discarded iterations. A sampled rolling peak is not a guaranteed sustained capacity. If the generator runs out of VUs or CPU, this setup cannot independently establish the application's ceiling.

## 7. Verify telemetry, reports and failures

Open [Grafana on port 13001](http://localhost:13001). Use **Shortener · Performance & efficiency** for application metrics and README captures. Use **Load test · live** for generator metrics. Select the exact run interval; do not combine independent runs into one screenshot.

During a run, verify fresh generator and server samples:

```sh
curl -fsSG http://localhost:19092/api/v1/query \
  --data-urlencode 'query=k6_http_reqs_total'
curl -fsSG http://localhost:19092/api/v1/query \
  --data-urlencode 'query=up{job="shortener"}'
```

The k6 log must list **Prometheus remote write** as an output. A running Prometheus or an `up` server does not prove that k6 metrics arrived. The management runner passes both outputs explicitly: a CLI `--out json=…` overrides `K6_OUT` rather than adding to it.

For each management run, inspect:

- `run.txt`: revision, load-script hash, stage times, outputs and exit status.
- `management-preparation.txt` and `counts-after.txt`: physical, visible and maintained counts before and after.
- `report.json`: measured window, offered rate, completions inside/outside it, mean, one-second peak, p99, HTTP errors, failed checks, dropped iterations and validation issues.
- `summary.json` and `k6.log`: k6 thresholds, configuration, warnings and failures.
- `resources-before.json`, `resources-after.json`, `generator-state.json`: image IDs, limits, trace sampling, OOM state and restart counts.

You can recompute a management report from its events. Substitute the actual directory and operation; use `--rate 200` for `mixed`:

```sh
python3 scripts/report-management-load.py reports/load/management-XXXXXXXX/events.json \
  --operation create --duration-seconds 300 --rate 1010 \
  --output /tmp/recomputed-management-report.json
```

Isolated management gates require at least 1,000 completed requests/s inside the measured window, p99 ≤ 100 ms, zero HTTP errors, failed checks and dropped iterations. The extra API/redirect workloads retain their existing gates: less than 1% failed HTTP requests, over 99% passing checks, overall/per-operation p99 below 500 ms, and zero dropped iterations. Report the actual failures even if a percentage-based gate passes.

For the published GET peak, query successful client requests using a rolling 20-second rate sampled every five seconds. These timestamps select the recorded run; substitute your own `load_started_utc` and completion time when repeating it:

```sh
curl -fsSG http://localhost:19092/api/v1/query_range \
  --data-urlencode 'query=sum(rate(k6_http_reqs_total{operation="redirect_get",phase="workload",expected_response="true"}[20s]))' \
  --data-urlencode start=2026-11-06T17:43:53Z \
  --data-urlencode end=2026-11-06T17:48:56Z \
  --data-urlencode step=5
```

The published data retains these samples and the matching server-rate and CPU series. The peak is the maximum returned rate, not a one-second event count. Whole-run mean uses completed workload HTTP requests divided by k6's `state.testRunDurationMs / 1000`, including any final drain. In a k6 `http_req_failed` summary, `values.passes` counts failed HTTP requests (the predicate was true) and `values.fails` counts successful requests. Do not reverse those names when verifying totals.

Client p99 comes from k6. Grafana endpoint percentiles estimate server-side latency from histogram buckets. Grafana peak rates use scrape/rate intervals; the management report's peak uses one-second event windows. These measurements are distinct. Never substitute one for another.

The 32-MiB redirect stress failed: the public server was OOM-killed shortly after startup. It does not establish the maximum GET throughput. The recorded historical 27,179 req/s used a different workload, CPU budget and tracing configuration; see [historical results](performance.md).

The first attempt in `management-7AGp79D3` was interrupted because remote write was absent. Its shell exit status is unreliable after a runner edit during execution. It is excluded from the valid results. Raw evidence and an explanatory note were preserved locally.

## 8. Stop without deleting the population

```sh
docker compose -p shortner-load-10m -f docker/docker-compose.load.yaml stop
```

The runners leave the stack running. Do not add `down -v` unless you intend to delete the test database. See [load testing](load-testing.md) for other workloads and cleanup details.
