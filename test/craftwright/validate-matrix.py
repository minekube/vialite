#!/usr/bin/env python3
import json
import os
import sys
from pathlib import Path


REQUIRED_MODES = {"embedded", "subprocess"}
REQUIRED_CASES = {
    "latest-client-latest-backend",
    "latest-client-older-backend",
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

    craftwright = matrix.get("craftwright") or {}
    expected_repo = os.environ.get("CRAFTWRIGHT_REPOSITORY")
    if expected_repo and craftwright.get("repository") != expected_repo:
        fail(f"craftwright.repository must match CRAFTWRIGHT_REPOSITORY={expected_repo}")
    expected_ref = os.environ.get("CRAFTWRIGHT_REF")
    if expected_ref and craftwright.get("ref") != expected_ref:
        fail(f"craftwright.ref must match CRAFTWRIGHT_REF={expected_ref}")
    if craftwright.get("currentCapability") != "fake-session-api":
        fail("current Craftwright capability must be documented explicitly")
    if craftwright.get("requiredCapability") != "real-client-launch-connect-chat-wait":
        fail("required Craftwright capability must be documented explicitly")

    rows = matrix.get("rows")
    if not isinstance(rows, list) or not rows:
        fail("rows must be a non-empty list")

    modes = {row.get("mode") for row in rows}
    if modes != REQUIRED_MODES:
        fail(f"modes must be exactly {sorted(REQUIRED_MODES)}, got {sorted(modes)}")

    for mode in REQUIRED_MODES:
        cases = {row.get("case") for row in rows if row.get("mode") == mode}
        if cases != REQUIRED_CASES:
            fail(f"{mode} cases must be exactly {sorted(REQUIRED_CASES)}, got {sorted(cases)}")

    for row in rows:
        row_id = row.get("id")
        if not row_id:
            fail("every row needs an id")
        if row.get("enabled") is not False:
            fail(f"{row_id} must stay disabled until Craftwright has a real client runner")
        if not row.get("clientVersion") or not row.get("serverVersion"):
            fail(f"{row_id} must declare clientVersion and serverVersion")
        if row.get("expected") != ["join", "chat"]:
            fail(f"{row_id} expected must be ['join', 'chat']")

    print(f"validated {len(rows)} Craftwright target rows")


if __name__ == "__main__":
    main()
