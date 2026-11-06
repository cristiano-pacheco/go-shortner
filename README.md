# URL shortener

Go URL shortener with separate management and public servers. PostgreSQL stores links. Redis caches redirects.

## How it works

**Admins** manage links through an authenticated API. **Everyone else** just follows short URLs — the public server turns a code into a `302` redirect and touches nothing else. One image, two hardened Go servers:

```mermaid
flowchart TB
    %% ══════════════════════ clients ══════════════════════
    ADMIN(["🔐 Admin"])
    VISITOR(["🧭 Visitor"])

    %% ══════════════════════ the app ══════════════════════
    subgraph APP["📦 go-shortner — one Go image · two hardened servers"]
        direction LR

        subgraph MGMT["🛠️ Management API · :8080"]
            direction TB
            MGW["X-API-Key middleware"]
            MUC["Link use cases<br>create · read · list · update · delete"]
            MGW --> MUC
        end

        subgraph PUB["⚡ Public redirector · :8081"]
            direction TB
            PR["GET / HEAD routes<br>no authentication"]
            RUC["Resolve use case"]
            PR --> RUC
        end
    end

    %% ══════════════════════ state ══════════════════════
    REDIS[("🫙 Redis<br>redirect snapshots<br>TTL ≤ 60 s")]
    PG[("🐘 PostgreSQL<br>source of truth<br>soft delete · count buckets")]

    %% ══════════════════════ watching ══════════════════════
    subgraph OBS["📊 Observability"]
        direction LR
        PROM["Prometheus<br>:9091"]
        GRAF["Grafana<br>:3000"]
        TEMPO["Tempo<br>traces"]
        PROM --> GRAF
    end

    %% ══════════════════════ wiring ══════════════════════
    ADMIN ==>|"JSON CRUD"| MGW
    VISITOR ==>|"GET /:code"| PR

    RUC -->|"① snapshot lookup"| REDIS
    REDIS -.->|"② hit → 302"| RUC
    RUC -->|"③ miss → query"| PG

    MUC ==>|"write + commit"| PG
    MUC -.->|"invalidate changed codes"| REDIS

    APP -.->|"metrics + traces"| OBS

    %% ══════════════════════ make it pretty ══════════════════════
    classDef client fill:#e0f2fe,stroke:#0284c7,stroke-width:2px,color:#075985
    classDef mgmt fill:#f5f3ff,stroke:#a855f7,stroke-width:2px,color:#581c87
    classDef pub fill:#f0fdf4,stroke:#16a34a,stroke-width:2px,color:#14532d
    classDef data fill:#fffbeb,stroke:#d97706,stroke-width:2px,color:#78350f
    classDef obs fill:#f8fafc,stroke:#94a3b8,stroke-width:2px,color:#334155

    class ADMIN,VISITOR client
    class MGW,MUC mgmt
    class PR,RUC pub
    class REDIS,PG data
    class PROM,GRAF,TEMPO obs

    style APP fill:#ffffff,stroke:#a1a1aa,stroke-width:2px
    style MGMT fill:#faf5ff,stroke:#c084fc,stroke-width:2px
    style PUB fill:#f0fdf4,stroke:#86efac,stroke-width:2px
    style OBS fill:#f8fafc,stroke:#cbd5e1,stroke-width:2px

    linkStyle 2 stroke:#a855f7,stroke-width:2.5px
    linkStyle 3 stroke:#16a34a,stroke-width:2.5px
    linkStyle 4,5,6 stroke:#059669,stroke-width:2.5px
    linkStyle 7 stroke:#7c3aed,stroke-width:2.5px
    linkStyle 8 stroke:#a855f7
    linkStyle 9,10 stroke:#94a3b8
```

How to read it:

- **Admin → Management API (`:8080`)** — JSON CRUD behind an `X-API-Key`. Every write commits to PostgreSQL first, then invalidates the changed code in Redis.
- **Visitor → Public redirector (`:8081`)** — `GET`/`HEAD` only. No key, no management surface.
- **Hot path ① → ② → ③** — the resolver checks the Redis snapshot first, answers `302` on a hit, and only queries PostgreSQL on a miss (or if Redis is down). A changed link can serve stale data for at most 60 seconds.
- **PostgreSQL** — the single source of truth: soft deletes, expiration, active state, and the counters that keep pagination totals exact.
- **Prometheus + Tempo → Grafana** — both servers export metrics and traces on their own listeners.

A single redirect, step by step:

```mermaid
sequenceDiagram
    autonumber
    actor V as 👤 Visitor
    participant P as ⚡ public-server
    participant R as 🫙 Redis
    participant D as 🐘 PostgreSQL

    V->>P: GET /go
    P->>R: get snapshot for code "go"
    alt snapshot exists · active · not expired
        R-->>P: destination
        P-->>V: 302 Found + Location
    else cache miss (or Redis down)
        R-->>P: nothing usable
        P->>D: SELECT destination, active, expires_at
        alt link exists · active · not expired
            D-->>P: row
            P->>R: store snapshot · TTL ≤ 60 s
            P-->>V: 302 Found + Location
        else unknown · inactive · expired · deleted
            D-->>P: no usable row
            P-->>V: 404 Not Found
        end
    end
```

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

The mixed workload reached 7,080 requests/s with 2.28 ms p95 and zero HTTP errors. A repeat reached 6,854 requests/s. Both used one million links, 10 virtual users and 2 minutes of closed-loop load. The PostgreSQL pool allows 30 open and 10 idle connections.

The two Go servers have a combined 64 MiB memory limit. Dependencies use additional resources. See [results and Compose limits](docs/performance.md). Sustained arrival-rate capacity remains unverified.

## Documentation

- [Operations](docs/operations.md)
- [HTTP API](docs/api.md)
- [Architecture](docs/architecture.md)
- [Load testing](docs/load-testing.md)
- [Metrics](docs/metrics.md)
- [Glossary](GLOSSARY.md) and [code map](GLOSSARY-MAP.md)

Configure proxy-level abuse protection before internet exposure. The application has no rate limiting or phishing detection.
