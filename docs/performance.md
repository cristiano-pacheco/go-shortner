# Performance

Measured on 2026-11-05. One million visible links. Metrics enabled, tracing disabled.

## Results

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

Apple M2 Max host. Colima VM with 4 vCPUs and 8,307,826,688 bytes RAM. Linux aarch64, kernel `6.8.0-100-generic`, Docker 29.2.1. Go 1.27.3, PostgreSQL 17.5, Redis 7, Nginx 1.27, k6 0.57.0. The generator shares the VM. Dependencies have no resource caps.

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

[Measurement snapshot](benchmarks/2026-11-05.json) contains results and report paths. Raw reports remain in `reports/load/`. Baseline revision is `d0f86c0050cf8dc20ad8dba390a8b114c60e79e5`. Optimized reports archive uncommitted source inputs and image IDs. See [load testing](load-testing.md) for report fields.

After concurrent mixed load, maintained and independently counted totals both equaled 1,000,000. Build, default tests, E2E, integration with race detection, migration checks, lint, vulnerability scanning and nil-safety analysis passed.

## Limits

Redirect tests alternate GET and HEAD over 1,000 mostly cached codes. Nginx does not cache responses. Listing tests cover only page 1. Deep offsets still require proportional scanning. Soft-deleted rows accumulate between runs.

These are single local trials, not cold-cache, TLS or long-running production tests. Generator contention and tracing can change results. Measure from a separate host before setting production capacity.
