#!/usr/bin/env python3
"""
Regression test runner for MobileVC.

Usage:
    python3 run_regression.py              # run all tests
    python3 run_regression.py --test test_permission_input_guard  # run one
    python3 run_regression.py --no-restart # skip backend restart
    python3 run_regression.py --keep-log   # don't truncate server.log
"""

import argparse
import asyncio
import subprocess
import sys
import time
from datetime import datetime
from pathlib import Path

TEST_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = TEST_DIR.parent.parent
SERVER_LOG = PROJECT_ROOT / "server.log"

TESTS = [
    "test_permission_input_guard",
    "test_session_resume_permission",
    "test_push_token_lifecycle",
]


def build_backend() -> bool:
    print("[runner] building backend...")
    result = subprocess.run(
        ["go", "build", "-o", "server", "./cmd/server"],
        cwd=PROJECT_ROOT,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        print(f"[runner] BUILD FAILED:\n{result.stderr}")
        return False
    print("[runner] build OK")
    return True


def run_test(test_name: str) -> bool:
    test_file = TEST_DIR / f"{test_name}.py"
    if not test_file.exists():
        print(f"[runner] test not found: {test_file}")
        return False
    print(f"\n{'='*60}")
    print(f"[runner] running: {test_name}")
    print(f"{'='*60}")
    start = time.monotonic()
    result = subprocess.run(
        [sys.executable, str(test_file)],
        cwd=PROJECT_ROOT,
        capture_output=False,
        timeout=120,
    )
    elapsed = time.monotonic() - start
    passed = result.returncode == 0
    status = "PASSED" if passed else "FAILED"
    print(f"\n[runner] {test_name}: {status} ({elapsed:.1f}s)")
    return passed


def main():
    parser = argparse.ArgumentParser(description="MobileVC regression test runner")
    parser.add_argument("--test", help="Run a single test by name")
    parser.add_argument("--no-restart", action="store_true", help="Skip backend restart")
    parser.add_argument("--no-build", action="store_true", help="Skip build step")
    parser.add_argument("--keep-log", action="store_true", help="Keep existing server.log")
    args = parser.parse_args()

    if args.test:
        test_names = [args.test]
    else:
        test_names = TESTS

    # build
    if not args.no_build:
        if not build_backend():
            sys.exit(1)

    # truncate log
    if not args.keep_log:
        SERVER_LOG.write_text("")

    # run tests
    results: dict[str, bool] = {}
    for name in test_names:
        results[name] = run_test(name)

    # summary
    print(f"\n{'='*60}")
    print("[runner] RESULTS")
    print(f"{'='*60}")
    passed = sum(1 for v in results.values() if v)
    failed = len(results) - passed
    for name, ok in results.items():
        status = "PASS" if ok else "FAIL"
        print(f"  {status}: {name}")
    print(f"\n  {passed} passed, {failed} failed out of {len(results)}")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
