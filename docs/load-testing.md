# Load testing

Requires Docker, Docker Compose and curl. Run from the repository root:

```sh
make test-load
```

The isolated stack runs two app instances behind Nginx, each limited to 0.5 CPU and 128 MiB. The command opens the Shortener and k6 Grafana dashboards, runs k6, then opens the HTML report. Reports are saved under `reports/load/`.

Set `LOAD_OPEN_BROWSER=0` for headless use. URLs and report paths are printed to the terminal. Grafana is available without login on localhost; tracing and its Tempo link are unavailable in this stack.

Configure the load and ports as needed:

```sh
LOAD_VUS=50 LOAD_DURATION=5m LOAD_PORT=8089 LOAD_GRAFANA_PORT=3002 LOAD_PROMETHEUS_PORT=9093 make test-load
```

Before k6 starts, the script seeds one million active links and runs `ANALYZE` on the isolated database. Set `LOAD_SEED_COUNT` to change the seed target (or `0` to skip insertion). Increasing the target adds seed links; repeating or lowering it does not change existing seed links. Seeding can take time and disk space.

By default, 10 virtual users run for 2 minutes. Each iteration creates, retrieves, lists, updates and deletes a link, and requests its redirect with GET and HEAD. Thresholds require fewer than 1% failed requests, over 99% passing checks per operation and overall, and p95 latency below 500 ms. Readiness requests are excluded from request thresholds. These thresholds are examples, not SLOs.

Each iteration leaves a soft-deleted row in PostgreSQL. Data persists across runs; failed iterations may leave active links. The test measures a mixed workload, not redirect performance alone. The generator shares the host with the stack; use `docker stats` to check resource saturation.

Only one script execution can run per Docker daemon. If a forced termination leaves the lock behind, confirm no test is running and remove it:

```sh
docker network rm shortner-load-run-lock
```

Stop the stack and remove its database volume:

```sh
docker compose -p shortner-load -f docker/docker-compose.load.yaml down -v --remove-orphans
```

Reports are retained. Remove `reports/load/` separately if needed.
