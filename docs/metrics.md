# Metrics conventions

The application supplies one Prometheus registry to the Bricks administrative
listener. The registry contains the Go and process collectors. Future metrics
must register with the injected `prometheus.Registerer`, not the global
registry. The listener binds to `0.0.0.0:9090` inside the container. Compose
publishes it on `127.0.0.1:${METRICS_PORT:-9090}`; Prometheus scrapes it on the
Compose network. The public listener has no metrics route.

Use separate success counters, error counters and duration histograms for use
cases. Count each execution once, including input rejection. Use these bounded
error categories: `invalid_argument`, `conflict`, `not_found`, `canceled` and
`internal`. Do not use error messages or link-specific values as labels.
Subsequent instrumentation must classify from error identities, not strings.

Use seconds for durations. The proposed default histogram boundaries are
`0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 15, 30`.
They distinguish millisecond cache operations and include the 15-second HTTP
write timeout, with a final finite bucket beyond that deadline. They are not
performance targets.

For six use cases, one unlabeled success counter, five error-category series
and one histogram per use case produce at most 138 series per instance: six
success, 30 error and 102 histogram series (14 finite buckets, `+Inf`, `_sum`
and `_count` per histogram).
This proposed budget excludes HTTP, dependency, pool, Go and process metrics.

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
proposed use-case budget, the maximum is 46,038 series, excluding dependency,
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
fill skipped because its deadline has passed does not count as a SET.
