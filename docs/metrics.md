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
This budget excludes HTTP, dependency, pool, Go and process metrics; their
instrumentation must document its own cardinality and update the total. No
business collectors are registered until their instrumentation is implemented.
