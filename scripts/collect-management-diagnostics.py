#!/usr/bin/env python3
"""Collect timestamped, private load-run evidence until SIGTERM or SIGINT.

This is active instrumentation and can affect a run. Keep diagnostic runs
separate from capacity comparisons. Output can contain database metadata and
must be reviewed before publication.
"""
import argparse
import datetime
import http.client
import json
import os
import signal
import socket
import subprocess
import time
import urllib.parse
import urllib.request
from pathlib import Path


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost", timeout=5)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


def utcnow():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def command(args, timeout=15):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout, check=False)
    if result.returncode:
        raise RuntimeError(f"{args[0]} exited {result.returncode}: {result.stderr.strip()}")
    return result.stdout


def socket_path():
    host = os.environ.get("DOCKER_HOST") or command(
        ["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"]
    ).strip()
    if not host.startswith("unix://"):
        raise RuntimeError("Docker diagnostics require a local Unix socket")
    return host[len("unix://"):]


def docker_stats(path, container):
    conn = UnixConnection(path)
    try:
        conn.request("GET", f"/containers/{container}/stats?stream=false")
        response = conn.getresponse()
        if response.status != 200:
            raise RuntimeError(f"Docker stats returned HTTP {response.status}")
        stats = json.loads(response.read())
        # Keep cgroup counters as well as the preceding CPU sample. These permit
        # offline calculation of usage and throttling without inventing zeros.
        return {key: stats.get(key) for key in (
            "read", "preread", "cpu_stats", "precpu_stats", "memory_stats", "pids_stats"
        )}
    finally:
        conn.close()


def collect(args, path, write):
    ids = command(["docker", "ps", "-aq", "--filter", f"label=com.docker.compose.project={args.project}"]).split()
    if not ids:
        write("containers", {"error": "No containers found for project"})
        return
    inspect = json.loads(command(["docker", "inspect", *ids]))
    postgres = redis = None
    for container in inspect:
        labels = container.get("Config", {}).get("Labels") or {}
        service = labels.get("com.docker.compose.service")
        cid = container["Id"]
        state = container.get("State") or {}
        limits = container.get("HostConfig") or {}
        details = {"id": cid, "service": service, "image_id": container.get("Image"),
                   "state": {k: state.get(k) for k in ("Status", "Running", "OOMKilled", "ExitCode", "StartedAt", "FinishedAt")},
                   "restart_count": container.get("RestartCount"),
                   "limits": {k: limits.get(k) for k in ("NanoCpus", "CpuQuota", "CpuPeriod", "Memory", "MemorySwap")}}
        if state.get("Running"):
            try:
                details["stats"] = docker_stats(path, cid)
            except Exception as exc:
                details["stats_error"] = str(exc)
        write("container", details)
        if service == "postgres" and state.get("Running"):
            postgres = cid
        if service == "redis" and state.get("Running"):
            redis = cid

    if postgres:
        sql = """SELECT 'activity', coalesce(state,''), coalesce(wait_event_type,''), coalesce(wait_event,''), count(*)::text,
                        coalesce(max(extract(epoch from clock_timestamp() - query_start)) FILTER (WHERE state = 'active'), 0)::text
                 FROM pg_stat_activity WHERE datname = current_database() GROUP BY 2,3,4;
                 SELECT 'locks', locktype, mode, granted::text, count(*)::text FROM pg_locks GROUP BY 2,3,4;
                 SELECT 'database', xact_commit::text, xact_rollback::text, deadlocks::text,
                        blk_read_time::text, blk_write_time::text FROM pg_stat_database WHERE datname = current_database();"""
        try:
            write("postgres", {"data": command(["docker", "exec", postgres, "psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-U", "shortner_load", "-d", "shortner_load", "-c", sql])})
        except Exception as exc:
            write("postgres", {"error": str(exc)})
    else:
        write("postgres", {"error": "PostgreSQL container is not running"})
    if redis:
        try:
            info = {section: command(["docker", "exec", redis, "redis-cli", "INFO", section])
                    for section in ("stats", "memory", "cpu")}
            write("redis", {"data": info})
        except Exception as exc:
            write("redis", {"error": str(exc)})
    else:
        write("redis", {"error": "Redis container is not running"})

    # Query only operational metrics; avoid scrape configuration and environment.
    query = '{__name__=~"go_.*|process_.*|shortener_.*|db_.*|redis_.*"}'
    url = f"http://127.0.0.1:{args.prometheus_port}/api/v1/query?" + urllib.parse.urlencode({"query": query})
    try:
        with urllib.request.urlopen(url, timeout=10) as response:
            write("prometheus", {"data": json.load(response)})
    except Exception as exc:
        write("prometheus", {"error": str(exc)})


def query_plans(args, write):
    ids = command(["docker", "ps", "-q", "--filter", f"label=com.docker.compose.project={args.project}",
                   "--filter", "label=com.docker.compose.service=postgres"]).split()
    if len(ids) != 1:
        raise RuntimeError("Expected one running PostgreSQL container")
    # Match the two List repository queries and the page calculation in management.js.
    # EXPLAIN does not execute the queries. The deep offset is evaluated separately.
    total = int(command(["docker", "exec", ids[0], "psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1",
                         "-U", "shortner_load", "-d", "shortner_load", "-c",
                         "SELECT SUM(total)::bigint FROM link_counts HAVING COUNT(*) = 64"]).strip())
    offset = (total * 9 // 10 // 20) * 20
    sql = f"""EXPLAIN (FORMAT JSON) SELECT SUM(total)::bigint FROM link_counts HAVING COUNT(*) = 64;
             EXPLAIN (FORMAT JSON) SELECT * FROM links WHERE deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT 20;
             EXPLAIN (FORMAT JSON) SELECT * FROM links WHERE deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT 20 OFFSET {offset};"""
    data = command(["docker", "exec", ids[0], "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "shortner_load", "-d", "shortner_load", "-c", sql], timeout=60)
    write("query_plans", {"analyze": False, "visible_count": total, "deep_offset": offset, "data": data})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--interval", type=float, default=5)
    parser.add_argument("--prometheus-port", type=int, default=int(os.environ.get("LOAD_PROMETHEUS_PORT", "9092")))
    parser.add_argument("--query-plans", action="store_true", help="collect planner estimates once and exit; do not use during capacity measurements")
    args = parser.parse_args()
    if args.interval <= 0 or not 1 <= args.prometheus_port <= 65535:
        parser.error("interval must be positive and port must be between 1 and 65535")
    args.output.mkdir(parents=True, exist_ok=True)
    stopping = False

    def stop(_signum, _frame):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    with (args.output / "samples.jsonl").open("a", encoding="utf-8", buffering=1) as output:
        def write(kind, data):
            output.write(json.dumps({"timestamp": utcnow(), "kind": kind, **data}, separators=(",", ":")) + "\n")

        write("collector_start", {"project": args.project, "interval_seconds": args.interval,
                                  "mode": "query_plans" if args.query_plans else "sampling",
                                  "instrumentation": "docker stats, Prometheus instant query, PostgreSQL wait/lock aggregates and Redis INFO"})
        if args.query_plans:
            try:
                query_plans(args, write)
            except Exception as exc:
                write("collector_error", {"error": str(exc)})
                return 1
        else:
            try:
                path = socket_path()
            except Exception as exc:
                write("collector_error", {"error": str(exc)})
                return 1
            while not stopping:
                start = time.monotonic()
                try:
                    collect(args, path, write)
                except Exception as exc:
                    write("collector_error", {"error": str(exc)})
                while not stopping and time.monotonic() - start < args.interval:
                    time.sleep(max(0, min(0.2, args.interval - (time.monotonic() - start))))
        write("collector_stop", {})
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
