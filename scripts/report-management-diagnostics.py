#!/usr/bin/env python3
"""Summarize diagnostic samples within the k6 measurement window.

Sampling is a diagnostic workload, not an equivalent capacity comparison.
Missing samples remain missing; this tool does not infer a bottleneck.
"""
import argparse
import datetime as dt
import json
import math
from pathlib import Path


def timestamp(value):
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))


def nonnegative_delta(last, first):
    if isinstance(last, (int, float)) and isinstance(first, (int, float)) and last >= first:
        return last - first
    return None


def cpu_cores(stats):
    try:
        elapsed = (timestamp(stats["read"]) - timestamp(stats["preread"])).total_seconds()
        current = stats["cpu_stats"]["cpu_usage"]["total_usage"]
        previous = stats["precpu_stats"]["cpu_usage"]["total_usage"]
        usage = nonnegative_delta(current, previous)
        if elapsed > 0 and usage is not None:
            return usage / 1_000_000_000 / elapsed
    except (KeyError, ValueError, TypeError):
        pass
    return None


def selected_metric(name):
    return (name.startswith(("go_memstats_", "go_gc_", "go_sql_", "shortener_redis_pool_",
                             "shortener_repository_operation_duration_seconds", "shortener_cache_operation_duration_seconds"))
            or name in ("go_goroutines", "process_resident_memory_bytes"))


def summarize(samples, report, summary):
    windows = [operation["window"] for operation in report["operations"].values()]
    if not windows:
        raise ValueError("measurement report has no operation windows")
    start = min(timestamp(window["start"]) for window in windows)
    end = max(timestamp(window["end"]) for window in windows)
    if end <= start:
        raise ValueError("invalid measurement window")
    containers = {}
    metrics = {}
    errors = []
    postgres = []
    redis = []
    incidents = []
    for sample in samples:
        when = timestamp(sample["timestamp"])
        # Collection errors outside the window also indicate missing evidence.
        if sample.get("kind") == "collector_error":
            errors.append({"timestamp": sample["timestamp"], "error": sample.get("error")})
        if sample.get("kind") == "container":
            state = sample.get("state") or {}
            if state.get("OOMKilled") or sample.get("restart_count") or (state.get("ExitCode") or 0) != 0:
                incidents.append({"timestamp": sample["timestamp"], "service": sample.get("service"),
                                  "state": state, "restart_count": sample.get("restart_count")})
        if not start <= when < end:
            continue
        kind = sample.get("kind")
        if sample.get("error"):
            errors.append({"timestamp": sample["timestamp"], "kind": kind, "error": sample["error"]})
        if kind == "container":
            service = sample.get("service") or "unknown"
            key = (service, sample.get("id"))
            item = containers.setdefault(key, {"service": service, "container_id": sample.get("id"),
                "samples": 0, "cpu_cores_max": None, "memory_bytes_max": None,
                "throttled_periods_delta": None, "throttled_nanoseconds_delta": None,
                "cpu_periods_delta": None, "throttled_period_fraction": None, "oom_killed": False, "restart_count_max": 0, "_first_throttle": None, "_last_throttle": None})
            item["samples"] += 1
            state = sample.get("state") or {}
            item["oom_killed"] |= bool(state.get("OOMKilled"))
            item["restart_count_max"] = max(item["restart_count_max"], sample.get("restart_count") or 0)
            if sample.get("stats_error"):
                errors.append({"timestamp": sample["timestamp"], "kind": service, "error": sample["stats_error"]})
            stats = sample.get("stats") or {}
            cores = cpu_cores(stats)
            memory = (stats.get("memory_stats") or {}).get("usage")
            if cores is not None:
                item["cpu_cores_max"] = max(item["cpu_cores_max"] or 0, cores)
            if isinstance(memory, (int, float)):
                item["memory_bytes_max"] = max(item["memory_bytes_max"] or 0, memory)
            throttling = (stats.get("cpu_stats") or {}).get("throttling_data")
            if throttling:
                if item["_first_throttle"] is None:
                    item["_first_throttle"] = throttling
                item["_last_throttle"] = throttling
        elif kind == "postgres":
            postgres.append({"timestamp": sample["timestamp"], "data": sample.get("data"), "error": sample.get("error")})
        elif kind == "redis":
            redis.append({"timestamp": sample["timestamp"], "data": sample.get("data"), "error": sample.get("error")})
        elif kind == "prometheus":
            data = sample.get("data") or {}
            if data.get("status") != "success":
                errors.append({"timestamp": sample["timestamp"], "kind": kind, "error": "Prometheus query did not succeed"})
                continue
            for series in data.get("data", {}).get("result", []):
                labels = series.get("metric") or {}
                name = labels.get("__name__", "")
                if not selected_metric(name):
                    continue
                try:
                    value = float(series["value"][1])
                    if not math.isfinite(value):
                        continue
                except (ValueError, TypeError, KeyError, IndexError):
                    continue
                key = json.dumps(labels, sort_keys=True)
                metric = metrics.setdefault(key, {"labels": labels, "first": value, "last": value,
                                                  "max": value, "samples": 0, "counter_reset": False})
                if (name.endswith("_total") or name.endswith("_sum") or name.endswith("_count")) and value < metric["last"]:
                    metric["counter_reset"] = True
                metric["last"] = value
                metric["max"] = max(metric["max"], value)
                metric["samples"] += 1
    for item in containers.values():
        first, last = item.pop("_first_throttle"), item.pop("_last_throttle")
        if item["samples"] >= 2 and first is not None and last is not None:
            item["throttled_periods_delta"] = nonnegative_delta(last.get("throttled_periods"), first.get("throttled_periods"))
            item["throttled_nanoseconds_delta"] = nonnegative_delta(last.get("throttled_time"), first.get("throttled_time"))
            item["cpu_periods_delta"] = nonnegative_delta(last.get("periods"), first.get("periods"))
            periods, throttled = item["cpu_periods_delta"], item["throttled_periods_delta"]
            if periods and throttled is not None:
                item["throttled_period_fraction"] = throttled / periods
    for metric in metrics.values():
        name = metric["labels"]["__name__"]
        if metric["samples"] >= 2 and (name.endswith("_total") or name.endswith("_sum") or name.endswith("_count")):
            metric["delta"] = None if metric["counter_reset"] else nonnegative_delta(metric["last"], metric["first"])
    # Pair cumulative histogram sums/counts and pool wait totals only when both
    # series have at least two samples and neither counter reset.
    by_labels = {}
    for metric in metrics.values():
        labels = dict(metric["labels"])
        name = labels.pop("__name__")
        by_labels[(name, json.dumps(labels, sort_keys=True))] = metric
    latency = []
    pairs = (
        ("shortener_repository_operation_duration_seconds_sum", "shortener_repository_operation_duration_seconds_count"),
        ("shortener_cache_operation_duration_seconds_sum", "shortener_cache_operation_duration_seconds_count"),
        ("go_sql_wait_duration_seconds_total", "go_sql_wait_count_total"),
        ("shortener_redis_pool_wait_duration_seconds_total", "shortener_redis_pool_waits_total"),
    )
    for (name, labels_key), duration in by_labels.items():
        for duration_name, count_name in pairs:
            if name != duration_name:
                continue
            count = by_labels.get((count_name, labels_key), {})
            seconds, observations = duration.get("delta"), count.get("delta")
            mean = seconds / observations if seconds is not None and observations is not None and observations > 0 else None
            latency.append({"source": duration_name, "labels": json.loads(labels_key),
                            "seconds_delta": seconds, "observations_delta": observations, "mean_seconds": mean})
    k6_metrics = summary.get("metrics", {})
    generator = {"containers": [item for item in containers.values() if item["service"] == "k6"],
                 "dropped_iterations": report.get("dropped_iterations"),
                 "vus": k6_metrics.get("vus"), "vus_max": k6_metrics.get("vus_max"),
                 "configuration": summary.get("configuration"),
                 "note": "Dropped iterations indicate missed scheduling, not a proven generator CPU limit. Missing container samples do not imply zero usage."}
    warnings = []
    if not generator["containers"]:
        warnings.append("No k6 container sample in the measurement window")
    if not metrics:
        warnings.append("No selected Prometheus metric samples in the measurement window")
    if not postgres:
        warnings.append("No PostgreSQL samples in the measurement window")
    if not redis:
        warnings.append("No Redis samples in the measurement window")
    return {"window": {"start": start.isoformat(), "end": end.isoformat(),
                        "note": "Union of operation windows. CPU samples may begin before the window."},
            "instrumentation": "diagnostic sampling; compare only with the same instrumentation",
            "containers": list(containers.values()), "generator": generator,
            "prometheus_series": list(metrics.values()), "mean_latency_and_pool_waits": latency,
            "postgres_samples": postgres, "redis_samples": redis, "incidents": incidents,
            "collection_errors": errors, "warnings": warnings}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--samples", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--summary", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    with args.samples.open(encoding="utf-8") as source:
        samples = [json.loads(line) for line in source if line.strip()]
    with args.report.open(encoding="utf-8") as source:
        report = json.load(source)
    summary = {}
    if args.summary and args.summary.exists():
        with args.summary.open(encoding="utf-8") as source:
            summary = json.load(source)
    result = summarize(samples, report, summary)
    with args.output.open("w", encoding="utf-8") as output:
        json.dump(result, output, indent=2, allow_nan=False)
        output.write("\n")


if __name__ == "__main__":
    main()
