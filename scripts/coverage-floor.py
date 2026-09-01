#!/usr/bin/env python3
"""Fails if a clover report is under a floor.

Go and Java each gate coverage with their own toolchain. PHP writes clover and
nothing reads it, which is the same as not measuring — a number in a file
nobody opens does not stop a regression.
"""

import sys
import xml.etree.ElementTree as ET


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("usage: coverage-floor.py <clover.xml> <percent>", file=sys.stderr)
        return 2

    path, floor = argv[1], float(argv[2])
    metrics = ET.parse(path).getroot().find(".//project/metrics")
    if metrics is None:
        print(f"{path} has no project metrics", file=sys.stderr)
        return 1

    total = int(metrics.get("statements", 0))
    covered = int(metrics.get("coveredstatements", 0))
    if total == 0:
        print(f"{path} reports no statements at all", file=sys.stderr)
        return 1

    pct = 100.0 * covered / total
    print(f"coverage: {pct:.1f}% ({covered}/{total} statements)")
    if pct < floor:
        print(f"::error::coverage is {pct:.1f}%, below the {floor}% floor")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
