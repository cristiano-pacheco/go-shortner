# Performance

Historical benchmarks measured on 2026-11-05. One million visible links. Metrics enabled, tracing disabled. The final section records the new workloads with tracing enabled. Portable results are in [benchmarks.json](benchmarks.json).

## Pool tuning

Mixed workload, 10 virtual users, 2 minutes, one million visible links. Only the PostgreSQL connection-pool settings changed. Both runs passed all gates with zero HTTP errors.

| Pool, open / idle | Requests/s | HTTP p95 | List p95 |
| --- | ---: | ---: | ---: |
| 10 / 5 | 3,956 | 9.86 ms | 10.07 ms |
| 30 / 10 | 7,080 | 2.28 ms | 2.03 ms |

Throughput improved 1.79×. Both runs retained exactly one million visible links. Physical row counts differ because soft-deleted rows accumulate.

## Higher concurrency

The 30-open, 10-idle pool was tested with unchanged Compose limits.

| Virtual users | Duration | Requests/s | HTTP p95 | List p95 |
| --- | --- | ---: | ---: | ---: |
| 50 | 30 seconds | 5,261 | 34.36 ms | 35.34 ms |
| 100 | 30 seconds | 5,831 | 57.99 ms | 66.74 ms |
| 200 | 30 seconds | 1,896 | 319.28 ms | 488.18 ms |
| 10, repeat | 2 minutes | 6,854 | 2.34 ms | 2.18 ms |

All runs passed with zero HTTP errors, OOM kills or restarts. Counts remained exact. Management reached its 0.5-CPU cap. More concurrency increased latency without improving throughput. These short trials do not establish a maximum sustained rate.

## Earlier results

These tests used the previous 10-open, 5-idle pool. Redirects have not been retested with the larger pool.

Closed-loop k6 tests used 50 virtual users for 60 seconds. Rates exclude readiness requests. Mixed iterations create, get, list, update, redirect with GET and HEAD, then soft-delete a link. Lists fetch page 1 with 20 items and an exact total.

| Configuration | Requests/s | HTTP p95 | List p95 | HTTP errors |
| --- | ---: | ---: | ---: | ---: |
| Original mixed | 382 | 574.74 ms | 898.36 ms | 0% |
| Optimized mixed, same 1-CPU cap | 6,221 | 30.97 ms | 39.53 ms | 0% |
| Optimized mixed, default caps | 6,832 | 33.01 ms | 41.47 ms | 0% |
| Original redirects | 14,256 | 9.12 ms | N/A | 0% |
| Optimized redirects | 27,179 | 3.96 ms | N/A | 0% |
| Optimized listing | 4,352 | 44.98 ms | 44.98 ms | 0% |

At the same total CPU cap, mixed throughput improved 16.3×. The default configuration permits more CPU. Both redirect tests allow 1 CPU for public traffic. Management is idle in the optimized redirect test.

The original mixed test failed the 500 ms latency gate. All optimized tests above passed. Closed-loop results do not certify an offered arrival rate.

## Resources

| Setting | Original | Optimized default |
| --- | --- | --- |
| Servers | 2 combined | 1 public, 1 management |
| Total Go memory limit | 256 MiB | 64 MiB |
| CPU caps | 2 × 0.5 | public 1, management 0.5 |
| Go runtime | Defaults | `GOMEMLIMIT=24MiB`, `GOMAXPROCS=1` per server |
| Swap | Docker defaults | Disabled |

The 64 MiB limit covers only the two Go servers. PostgreSQL, Redis, Nginx, k6 and monitoring are additional. Memory limits are not measured RSS reductions. Neither server was OOM-killed or restarted.

Compose runs PostgreSQL 17.5, Redis 7, Nginx 1.27 and k6 0.57.0. Dependencies and the generator have no CPU or memory limits.

## Changes

- Replaced full-table counts with [64 transactional counters](architecture.md#exact-pagination-totals). Under load, the original count took 477 ms. Fetching the page took 0.031 ms. A replacement count sample took 0.064 ms.
- Separated public and management routes into independent processes.
- Reused upstream HTTP connections. A smaller connection cache caused `EADDRNOTAVAIL` and 5.89% failures in a rejected trial.
- Gave the public process a full CPU quota. Management remains capped at 0.5 CPU.

## Failed offered-rate tests

All trials below ran for three minutes with zero HTTP errors. All failed the zero-dropped-iterations gate.

| Offered redirects/s | Virtual users | Priming requests | Dropped iterations |
| --- | ---: | ---: | ---: |
| 20,000 | 100 to 200 | 0 | 199,795 |
| 10,000 | 200 | 0 | 654 |
| 10,000 | 200 | 1,000 | 758 |
| 10,000 | 500 | 1,000 | 139,950 |

The 20,000/s trial served about 18,889 requests/s. The 500-user trial used a larger upstream connection cache, which was reverted. Neither 10,000/s nor 20,000/s is certified as a sustained offered rate.

## Reproduce

```sh
# Equal total CPU cap.
LOAD_OPEN_BROWSER=0 LOAD_PUBLIC_CPUS=0.5 LOAD_VUS=50 LOAD_DURATION=60s make test-load

# Default caps. Set LOAD_WORKLOAD to mixed, redirect or list.
LOAD_OPEN_BROWSER=0 LOAD_WORKLOAD=redirect LOAD_VUS=50 LOAD_DURATION=60s make test-load
```

After concurrent mixed load, maintained and independently counted totals both equaled 1,000,000. Build, default tests, E2E, integration with race detection, migration checks, lint, vulnerability scanning and nil-safety analysis passed.

## Limits

Redirect tests alternate GET and HEAD over 1,000 mostly cached codes. Nginx does not cache responses. Listing tests cover only page 1. Deep offsets still require proportional scanning. Soft-deleted rows accumulate between runs.

These are single Compose trials, not cold-cache, TLS or long-running production tests. Generator contention and tracing can change results.

## Repeated-read workloads

Measured on 2026-11-06 with the existing application image, metrics enabled and 1% root-trace sampling. Both Go containers retained their 32 MiB limits, with public capped at 1 CPU and management at 0.5 CPU. One million visible links. The PostgreSQL pool remained 30 open / 10 idle. Each configuration ran once.

| Scenario | Virtual users | Reads per code / batch size | Duration | Requests/s | HTTP p99 |
| --- | ---: | --- | --- | ---: | ---: |
| Redirect, sequential | 50 | 1 / 1 | 60s | 24,922 | 6.93 ms |
| Redirect, repeated bursts | 10 | 20 / 5 | 60s | 25,184 | 4.80 ms |
| Read-heavy CRUD + redirects | 20 | 20 / 10 | 30s | 7,219 | 60.10 ms |

All three runs passed the new p99 gates, with zero HTTP errors, 100% passing response checks and exact maintained link counts. Neither Go container was OOM-killed or restarted during a run.

Both redirect trials used 1,000 seeded codes and 1,000 warmup requests. The baseline used 50 sequential users; the burst trial used 10 users with up to five concurrent requests each. Throughput differed by about 1%. This short comparison does not establish a repeatable throughput gain. The burst trial's client p99 was lower.

The read-heavy workload creates, gets, lists and updates a link, reads its redirect 20 times, then deletes it. A successful iteration makes 25 requests. Each read must resolve to the updated destination. Its higher read share means its aggregate rate is not directly comparable to the historical seven-request mixed workload. The 30-second run validates the new workload; it is not a sustained-capacity test.

These runs include tracing overhead and use different request patterns from the historical results. The historical 27,179 redirects/s remains labeled with its original configuration. New summaries record p99 directly; historical p95 values have not been relabeled.
