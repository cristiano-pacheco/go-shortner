#!/usr/bin/env python3
"""Summarize the measurement window of a management k6 JSON event stream."""

import argparse
import datetime as dt
import json
import math
import sys


OPERATIONS = ("create", "get", "list", "update", "delete")
SCENARIOS = (*OPERATIONS, "list_deep", "mixed")


def timestamp(value):
    try:
        result = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise argparse.ArgumentTypeError(f"invalid timestamp: {value}") from error
    if result.tzinfo is None:
        raise argparse.ArgumentTypeError("timestamps must include a UTC offset")
    return result.astimezone(dt.timezone.utc)


def percentile(values, fraction):
    if not values:
        return None
    values.sort()
    position = (len(values) - 1) * fraction
    lower = math.floor(position)
    upper = math.ceil(position)
    return values[lower] + (values[upper] - values[lower]) * (position - lower)


def arguments():
    parser = argparse.ArgumentParser(
        description="Report k6 JSON points in the half-open UTC measurement window [start, end). "
        "Requires management_requests_started and per-operation HTTP and check metrics tagged "
        "phase=measurement and operation. Window markers are read from the stream. "
        "All measured-phase failures, including those after the window, invalidate the run."
    )
    parser.add_argument("events", help="k6 --out json event file")
    parser.add_argument("--start", type=timestamp, help="override start for all operations, ISO 8601")
    parser.add_argument("--end", type=timestamp, help="override end for all operations, ISO 8601")
    parser.add_argument("--duration-seconds", type=int, help="expected duration of each window")
    parser.add_argument("--operation", choices=SCENARIOS, required=True)
    parser.add_argument("--rate", type=float, help="offered requests per second per operation (default: 1010 isolated, 200 mixed)")
    parser.add_argument("--output", default="-", help="JSON report path, or - for stdout")
    args = parser.parse_args()
    if (args.start is None) != (args.end is None):
        parser.error("--start and --end must be supplied together")
    if args.start is not None and args.end <= args.start:
        parser.error("--end must be later than --start")
    if args.duration_seconds is not None and args.duration_seconds <= 0:
        parser.error("--duration-seconds must be positive")
    if args.rate is not None and (not math.isfinite(args.rate) or args.rate <= 0):
        parser.error("--rate must be positive and finite")
    return args


def new_operation():
    return {
        "started_in_window": 0,
        "started_outside_window": 0,
        "completed_in_window": 0,
        "completed_outside_window": 0,
        "duration_samples": [],
        "duration_outside_window": 0,
        "http_failure_samples": 0,
        "http_failures": 0,
        "checks": 0,
        "failed_checks": 0,
        "buckets": {},
    }


def measurement_windows(args, operations):
    if args.start is not None:
        windows = {operation: (args.start, args.end) for operation in operations}
    else:
        windows = {}
        with open(args.events, encoding="utf-8") as stream:
            for line_number, line in enumerate(stream, 1):
                try:
                    event = json.loads(line)
                    if event.get("type") != "Point" or event.get("metric") != "management_window_start":
                        continue
                    tags = event["data"]["tags"]
                    operation = tags["operation"]
                    if tags.get("phase") != "measurement" or operation not in operations or operation in windows:
                        raise ValueError("unexpected or duplicate measurement window")
                    start = dt.datetime.fromtimestamp(int(tags["start_ms"]) / 1000, dt.timezone.utc)
                    end = dt.datetime.fromtimestamp(int(tags["end_ms"]) / 1000, dt.timezone.utc)
                    if end <= start:
                        raise ValueError("invalid measurement window")
                    windows[operation] = (start, end)
                except (ValueError, KeyError, TypeError, OverflowError, OSError) as error:
                    raise ValueError(f"invalid marker on line {line_number}: {error}") from error
        missing = set(operations) - set(windows)
        if missing:
            raise ValueError(f"missing measurement windows: {', '.join(sorted(missing))}")
    if args.duration_seconds is not None:
        for operation, (start, end) in windows.items():
            if (end - start).total_seconds() != args.duration_seconds:
                raise ValueError(f"unexpected measurement duration for {operation}")
    return windows


def make_report(args):
    operations = OPERATIONS if args.operation == "mixed" else (args.operation,)
    windows = measurement_windows(args, operations)
    stats = {name: new_operation() for name in operations}
    dropped = 0
    unattributed = 0
    with open(args.events, encoding="utf-8") as stream:
        for line_number, line in enumerate(stream, 1):
            try:
                event = json.loads(line)
                if event.get("type") != "Point":
                    continue
                point = event["data"]
                tags = point.get("tags") or {}
                metric = event["metric"]
                when = timestamp(point["time"])
                value = float(point["value"])
                if not math.isfinite(value):
                    raise ValueError("non-finite metric value")
            except (ValueError, KeyError, TypeError, argparse.ArgumentTypeError) as error:
                raise ValueError(f"invalid event on line {line_number}: {error}") from error
            if metric == "dropped_iterations":
                # Dropped iterations have scenario tags, not request tags.
                dropped += value
                continue
            if tags.get("phase") != "measurement":
                continue
            if metric not in ("management_requests_started", "http_reqs", "http_req_duration", "http_req_failed", "checks"):
                continue
            operation = tags.get("operation")
            if operation not in stats:
                unattributed += 1
                continue
            entry = stats[operation]
            start, end = windows[operation]
            within = start <= when < end
            if metric == "management_requests_started":
                entry["started_in_window" if within else "started_outside_window"] += value
            elif metric == "http_reqs":
                entry["completed_in_window" if within else "completed_outside_window"] += value
                if within:
                    bucket = math.floor((when - start).total_seconds())
                    entry["buckets"][bucket] = entry["buckets"].get(bucket, 0) + value
            elif metric == "http_req_duration":
                if within:
                    entry["duration_samples"].append(value)
                else:
                    entry["duration_outside_window"] += 1
            elif metric == "http_req_failed":
                entry["http_failure_samples"] += 1
                entry["http_failures"] += value
            else:
                entry["checks"] += 1
                if value != 1:
                    entry["failed_checks"] += 1

    issues = []
    if dropped:
        issues.append("dropped iterations")
    if unattributed:
        issues.append("unattributed measurement metric points")
    offered_rate = args.rate if args.rate is not None else (200 if args.operation == "mixed" else 1010)
    results = {}
    for name, entry in stats.items():
        start, end = windows[name]
        duration = (end - start).total_seconds()
        started = entry["started_in_window"]
        completed = entry["completed_in_window"]
        total_completed = completed + entry["completed_outside_window"]
        p99 = percentile(entry["duration_samples"], 0.99)
        peak = None
        for bucket, count in entry["buckets"].items():
            seconds = min(1.0, duration - bucket)
            candidate = {"start": (start + dt.timedelta(seconds=bucket)).isoformat(),
                         "duration_seconds": seconds, "completed": count, "rate_per_second": count / seconds}
            if peak is None or candidate["rate_per_second"] > peak["rate_per_second"]:
                peak = candidate
        operation_issues = []
        if not started or not completed:
            operation_issues.append("missing started or completed requests")
        if entry["started_outside_window"]:
            operation_issues.append("requests started outside the measurement window")
        if total_completed != started:
            operation_issues.append("started and eventually completed counts differ")
        if len(entry["duration_samples"]) != completed:
            operation_issues.append("missing in-window duration samples")
        if entry["http_failure_samples"] != total_completed:
            operation_issues.append("missing HTTP failure samples")
        if entry["checks"] < total_completed:
            operation_issues.append("missing functional checks")
        if entry["http_failures"]:
            operation_issues.append("HTTP failures")
        if entry["failed_checks"]:
            operation_issues.append("failed functional checks")
        if p99 is None or p99 > 100:
            operation_issues.append("p99 exceeds 100 ms or is unavailable")
        if args.operation != "mixed" and completed / duration < 1000:
            operation_issues.append("mean completed rate below 1000 requests/s")
        if operation_issues:
            issues.extend(f"{name}: {issue}" for issue in operation_issues)
        results[name] = {
            "window": {"start": start.isoformat(), "end": end.isoformat(), "duration_seconds": duration},
            "offered_rate_per_second": offered_rate,
            "requests_per_iteration": 1,
            "started_in_window": started,
            "started_outside_window": entry["started_outside_window"],
            "completed_in_window": completed,
            "completed_outside_window": entry["completed_outside_window"],
            "incomplete_at_window_end": max(0, started - completed),
            "never_completed": max(0, started - total_completed),
            "mean_completed_rate_per_second": completed / duration,
            "peak": peak,
            "p99_ms": p99,
            "duration_samples_in_window": len(entry["duration_samples"]),
            "duration_samples_outside_window": entry["duration_outside_window"],
            "http_failure_samples": entry["http_failure_samples"],
            "http_failures": entry["http_failures"],
            "checks": entry["checks"],
            "failed_checks": entry["failed_checks"],
            "issues": operation_issues,
        }
    return {
        "scenario": args.operation,
        "diagnostic_only": args.operation == "mixed",
        "campaign_conditions": all((end - start).total_seconds() == 300 for start, end in windows.values())
        and offered_rate == (200 if args.operation == "mixed" else 1010),
        "dropped_iterations": dropped,
        "unattributed_metric_points": unattributed,
        "operations": results,
        "valid": not issues,
        "issues": issues,
    }


def main():
    args = arguments()
    try:
        report = make_report(args)
        text = json.dumps(report, indent=2, allow_nan=False) + "\n"
        if args.output == "-":
            sys.stdout.write(text)
        else:
            with open(args.output, "w", encoding="utf-8") as output:
                output.write(text)
        return 0 if report["valid"] else 1
    except (OSError, ValueError) as error:
        print(f"Cannot produce report: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
