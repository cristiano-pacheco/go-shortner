# URL shortener

Go URL shortener with separate management and public servers. PostgreSQL stores links. Redis caches redirects.

## Run

Requires Docker and Docker Compose.

```sh
make setup
```

| Service | Address |
| --- | --- |
| Management API | http://localhost:8080 |
| Public redirects | http://localhost:8081 |
| Grafana | http://localhost:3000 |

Setup creates credentials, runs migrations and seeds example links. Grafana uses `admin` / `admin`. Stop with `make docker-down`.

## Performance

With one million links, 50 virtual users and 60-second closed-loop tests:

| Workload | Requests/s |
| --- | ---: |
| Mixed | 6,832 |
| Redirects | 27,179 |

Both runs had zero HTTP errors. The two Go servers have a combined 64 MiB memory limit. Dependencies use additional resources. CPU limits and failed offered-rate tests are in [performance results](docs/performance.md). Sustained arrival-rate capacity remains unverified.

## Documentation

- [Operations](docs/operations.md)
- [HTTP API](docs/api.md)
- [Architecture](docs/architecture.md)
- [Load testing](docs/load-testing.md)
- [Metrics](docs/metrics.md)
- [Glossary](GLOSSARY.md) and [code map](GLOSSARY-MAP.md)

Configure proxy-level abuse protection before internet exposure. The application has no rate limiting or phishing detection.
