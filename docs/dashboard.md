# Performance dashboard

Grafana provisions `docker/observability/dashboards/shortener.json` in the development and load stacks. Its UID remains `shortener-operations`.

The main panels use one full-width visualization per row. Endpoint views omit HEAD requests. Overall summary cards still include all instrumented traffic, so their totals can exceed the traffic shown in endpoint views.

- Horizontal bars show peak requests/s for every HTTP method and route. A route's peak can occur at a different moment from another route's peak. Do not add their maxima to claim a simultaneous total.
- A latency table shows mean, p50, p90 and p99 side by side for each endpoint. In-cell bars compare routes within each column. Rows sort by p99 so the slowest endpoint appears first.

The table measures the selected interval. Mean divides duration sum by request count. Each percentile aggregates histogram bucket increases across instances before applying `histogram_quantile`. These values are distinct from the summary card's worst rolling redirect p99.

CPU and memory charts show resource use in a separate row group. Cache resolution and PostgreSQL utilization expose dependency pressure. HTTP totals, endpoint trends and the redirect-only p50/p99 comparison remain in the collapsed HTTP diagnostics row.

| Summary card | Calculation over the selected range |
| --- | --- |
| Peak req/s | Maximum sampled aggregate HTTP request rate |
| Redirect p99 | Maximum rolling histogram p99, not the whole-run percentile |
| HTTP 5xx | Server-error requests divided by total observed HTTP requests |
| Cache hits | Usable cache hits divided by cache decisions |
| Peak CPU | Maximum sampled sum of Go process CPU consumption in cores |
| Go memory | Maximum sampled sum of Go process resident memory |

Rates use Grafana's adaptive interval. Each datasource declares its actual scrape interval, 5 seconds for load tests and 15 seconds for development. Histogram percentiles are estimates. k6 measures client-side duration, including the proxy and network; the application histogram measures server-side duration. Compare their trends, not their values as equivalent measurements.

CPU and process memory exclude the database, Redis, proxy, generator and monitoring. The memory metric counts resident process pages, including shared pages. Container limits use different accounting. Neither number is a substitute for the other. Missing telemetry stays missing. It never becomes a successful zero-error result.

## Capture for the README

1. Run the load test, then select its exact start and end time in Grafana. Include a little idle time on each side to show the rise and fall of traffic.
2. Keep all instances selected. Leave diagnostic rows collapsed. Use a desktop-width viewport so the six summary cards and full-width panels remain legible.
3. Capture the summary cards, endpoint bars and latency table. Keep the panel titles, endpoint names and measurement units visible. Name the workload and its concurrency in the image caption.
4. Save the endpoint bars as `docs/assets/dashboard.jpg` and the latency table as `docs/assets/endpoint-latency.jpg`. Embed them in the README's "See the system work" section. Include the workload, date and duration in the alt text or caption.

Use one completed run per screenshot. A rolling Prometheus peak and a k6 whole-run average measure different things. Keep both labels visible when showing both. Never replace the measured chart values with benchmark targets.

## Diagnose

The collapsed rows group cache behavior, database capacity, link operations, runtime health and HTTP routes. A successful run can leave error-only panels empty because those labeled counters never appeared. Check telemetry availability before interpreting an empty chart.

`docker/load/dashboards/load.json` is the companion generator dashboard. Its p99 panel shows the worst exported per-series k6 p99, not a percentile aggregated across all requests. The workload-wide client p99 lives in `summary.json`.
