#!/usr/bin/env python3
import json
import os
import sys
from pathlib import Path


REQUIRED_MODES = {"embedded", "subprocess"}
REQUIRED_SUBPROCESS_ENABLED_CASES = {
    "newer-client-older-backend",
}
REQUIRED_DISABLED_CASES = {
    "same-version",
    "newer-client-older-backend",
    "older-client-latest-backend",
}


def fail(message: str) -> None:
    print(f"matrix validation failed: {message}", file=sys.stderr)
    raise SystemExit(1)


def main() -> None:
    if len(sys.argv) != 2:
        fail("usage: validate-matrix.py MATRIX_JSON")

    matrix = json.loads(Path(sys.argv[1]).read_text())
    if matrix.get("schema") != 1:
        fail("schema must be 1")

    craftless = matrix.get("craftless") or {}
    expected_repo = os.environ.get("CRAFTLESS_REPOSITORY")
    if expected_repo and craftless.get("repository") != expected_repo:
        fail(f"craftless.repository must match CRAFTLESS_REPOSITORY={expected_repo}")
    expected_ref = os.environ.get("CRAFTLESS_REF")
    if expected_ref and craftless.get("ref") != expected_ref:
        fail(f"craftless.ref must match CRAFTLESS_REF={expected_ref}")
    if craftless.get("currentCapability") != "fabric-client-smoke":
        fail("current Craftless capability must be the real Fabric client smoke")

    rows = matrix.get("rows")
    if not isinstance(rows, list) or not rows:
        fail("rows must be a non-empty list")

    modes = {row.get("mode") for row in rows}
    if modes != REQUIRED_MODES:
        fail(f"modes must be exactly {sorted(REQUIRED_MODES)}, got {sorted(modes)}")

    enabled_rows = [row for row in rows if row.get("enabled") is True]
    disabled_rows = [row for row in rows if row.get("enabled") is False]
    if not enabled_rows:
        fail("at least one real-client row must be enabled")

    for mode in REQUIRED_MODES:
        enabled_cases = {row.get("case") for row in enabled_rows if row.get("mode") == mode}
        disabled_cases = {row.get("case") for row in disabled_rows if row.get("mode") == mode}
        if mode == "subprocess":
            if enabled_cases != REQUIRED_SUBPROCESS_ENABLED_CASES:
                fail(
                    f"{mode} enabled cases must be exactly "
                    f"{sorted(REQUIRED_SUBPROCESS_ENABLED_CASES)}, got {sorted(enabled_cases)}"
                )
            expected_disabled_cases = {"same-version", "older-client-latest-backend"}
        else:
            if enabled_cases:
                fail(f"{mode} cases must stay disabled until embedded shutdown is fixed, got {sorted(enabled_cases)}")
            expected_disabled_cases = REQUIRED_DISABLED_CASES
        if disabled_cases != expected_disabled_cases:
            fail(f"{mode} disabled cases must be exactly {sorted(expected_disabled_cases)}, got {sorted(disabled_cases)}")

    for row in rows:
        row_id = row.get("id")
        if not row_id:
            fail("every row needs an id")
        if not row.get("clientVersion") or not row.get("serverVersion"):
            fail(f"{row_id} must declare clientVersion and serverVersion")
        if row.get("expected") != ["join", "chat"]:
            fail(f"{row_id} expected must be ['join', 'chat']")
        if row.get("enabled") is True and row.get("clientVersion") != "1.21.6":
            fail(f"{row_id} enabled rows must use the current Craftless real client version")
        if row.get("enabled") is False and not row.get("blockedReason"):
            fail(f"{row_id} disabled rows must explain the blocker")

    print(f"validated {len(enabled_rows)} enabled and {len(disabled_rows)} disabled Craftless target rows")


if __name__ == "__main__":
    main()
