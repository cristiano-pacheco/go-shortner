#!/usr/bin/env python3
"""Record selected Docker settings without writing environment variables."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--project", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    services = ("management", "public", "postgres", "redis", "proxy", "jaeger", "prometheus")
    result = {}
    errors = []
    for service in services:
        try:
            ids = subprocess.check_output(
                ["docker", "ps", "-q", "--filter", f"label=com.docker.compose.project={args.project}",
                 "--filter", f"label=com.docker.compose.service={service}"], text=True
            ).split()
            if len(ids) != 1:
                raise ValueError(f"expected one running container, got {len(ids)}")
            container = json.loads(subprocess.check_output(["docker", "inspect", ids[0]], text=True))[0]
            host = container["HostConfig"]
            state = container["State"]
            result[service] = {
                "image_id": container["Image"],
                "image_reference": container["Config"]["Image"],
                "command": container["Config"]["Cmd"],
                "nano_cpus": host["NanoCpus"],
                "cpu_quota": host["CpuQuota"],
                "cpu_period": host["CpuPeriod"],
                "memory_bytes": host["Memory"],
                "memory_swap_bytes": host["MemorySwap"],
                "oom_killed": state["OOMKilled"],
                "restart_count": container["RestartCount"],
            }
            if service in ("management", "public"):
                if host["NanoCpus"] != 500000000 or host["Memory"] != 33554432 or host["MemorySwap"] != 33554432:
                    errors.append(f"{service}: unexpected CPU or memory limit")
                mounts = [mount for mount in container["Mounts"]
                          if mount["Destination"] == "/app/config/load.yaml"]
                result[service]["config_mounts"] = [
                    {"destination": mount["Destination"], "read_only": not mount["RW"]}
                    for mount in mounts
                ]
                environment = dict(item.split("=", 1) for item in container["Config"]["Env"] if "=" in item)
                result[service]["app_env"] = environment.get("APP_ENV")
                result[service]["app_config_dir"] = environment.get("APP_CONFIG_DIR")
                if (environment.get("APP_ENV") != "load" or environment.get("APP_CONFIG_DIR") != "/app/config"
                        or len(mounts) != 1 or mounts[0]["RW"]):
                    errors.append(f"{service}: unexpected application configuration mount or APP_ENV")
                else:
                    content = Path(mounts[0]["Source"]).read_bytes()
                    expected = Path("docker/load/management.yaml").read_bytes()
                    result[service]["load_config_sha256"] = hashlib.sha256(content).hexdigest()
                    if content != expected or content != (b"app:\n  open-telemetry:\n    trace_enabled: true\n"
                                                           b"    trace_url: jaeger:4317\n    sample_rate: 0.1\n"):
                        errors.append(f"{service}: unexpected tracing configuration")
                    else:
                        result[service]["tracing"] = {"enabled": True, "sample_rate": 0.1, "endpoint": "jaeger:4317"}
                overrides = [key for key in environment if "TRACE" in key.upper() or "OTEL" in key.upper()
                             or "SAMPLE_RATE" in key.upper()]
                if overrides:
                    errors.append(f"{service}: unexpected tracing environment overrides")
        except (OSError, subprocess.CalledProcessError, ValueError, KeyError) as error:
            errors.append(f"{service}: {error}")
    with open(args.output, "w", encoding="utf-8") as output:
        json.dump({"services": result, "errors": errors}, output, indent=2)
        output.write("\n")
    for error in errors:
        print(error, file=sys.stderr)
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
