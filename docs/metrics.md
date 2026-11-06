# Metrics conventions

The application supplies one Prometheus registry to the Bricks administrative
listener. The registry contains the Go and process collectors. Future metrics
must register with the injected `prometheus.Registerer`, not the global
registry. The listener binds to `0.0.0.0:9090` inside the container. Compose
publishes management metrics on `127.0.0.1:${METRICS_PORT:-9090}` and public-process
metrics on `127.0.0.1:${PUBLIC_METRICS_PORT:-9093}`. Prometheus scrapes both on the
Compose network with `role` labels. The public HTTP listener has no metrics route.

Each use case has separate success counters, error counters and duration histograms.
Each execution is counted once, including input rejection. Error categories are
`invalid_argument`, `conflict`, `not_found`, `canceled` and `internal`. HTTP
metrics include requests rejected before a use case runs. A 302 response is a
successful redirect, not evidence that the visitor reached the destination. Labels
exclude error messages and link-specific values.

Durations use seconds. Use-case, cache, generation and repository histogram boundaries are
`0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 15, 30`.
They distinguish millisecond cache operations and include the 15-second HTTP
write timeout, with a final finite bucket beyond that deadline. They are not
performance targets.

For six use cases, one unlabeled success counter, five error-category series
and one histogram per use case produce at most 138 series per instance: six
success, 30 error and 102 histogram series (14 finite buckets, `+Inf`, `_sum`
and `_count` per histogram).
This budget excludes HTTP, dependency, pool, Go and process metrics.

HTTP request duration uses the finite buckets `0.001, 0.0025, 0.005, 0.01,
0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15` seconds. These are not
performance targets. HTTP metrics use ten method labels, including `OTHER`,
and five route labels, including `unmatched` and `/health` for non-probe
requests. The histogram has 17 series per method and route, and the in-flight
gauge has one, for at most 900 series. Status codes are bounded to the HTTP
server's 100 through 999 range. The outcome is determined by status, so the
counter has at most 45,000 series across all methods, routes and statuses. The
combined theoretical HTTP maximum is 45,900 series per instance. Normal use
creates only the label combinations observed in traffic. Together with the
use-case budget, the maximum is 46,038 series, excluding dependency,
pool, Go and process metrics.

Cache operation duration uses the same 14 finite buckets as use-case duration.
GET has success, miss and error results. SET and DELETE have success and error
results. Their seven result combinations each add one counter and 17 histogram
series, for 126 series. Seven cache decisions, two failure counters, two
resolution sources and one freshness retry counter add 12 more. The maximum
for these cache and resolution metrics is 138 series per instance. The combined
HTTP, use-case, cache and resolution budget is 46,176 series, excluding pool,
Go and process metrics.

Each cache read records one decision. Precedence is invalid encoding, Redis
error, miss, stale, inactive, invalid destination, expired, then usable hit.
Operation metrics measure Redis commands, not connection-pool hits. A cache
fill skipped because its deadline has passed does not count as a SET. Redis pool
hits mean reuse of a connection, not a usable redirect-cache hit. Resolution
source distinguishes cache-backed and database-backed results; cache-fill and
invalidation failures can occur even when the parent operation succeeds.

Generation adds up to 40 series for two result combinations and four creation
events. Repository operations add up to 432 series across six operations and
four results, for a conservative combined bound of 46,648 series per instance,
excluding pool, Go and process collectors. These are theoretical bounds, not
measured cardinalities. Repository list duration covers both count and data
reads, not one query.

The dashboard opens with selected-range summaries, horizontal throughput bars per
endpoint and a full-width latency table with mean, p50, p90 and p99. Resource charts
show Go CPU, memory, resolution sources and SQL utilization.
Diagnostics stay collapsed. Select instances and a time range. Thresholds are triage
references, not SLOs: 5xx 1%/5%, p99 100/250ms, SQL occupancy 80%/95%.
The p99 card shows the maximum rolling p99, not a whole-run percentile. CPU and memory
cards show sampled combined peaks across the selected Go processes. Missing data is
not success; idle is not an outage. Route-error rankings use the selected range;
rates use Grafana's adaptive interval. See the [dashboard guide](dashboard.md).
