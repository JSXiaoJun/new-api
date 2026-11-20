"""Compare two run.py result files case by case.

usage: python -I compare.py runs/base-ratio.jsonl runs/new-ratio.jsonl

Prints every case whose charge, logged usage or log type changed, and the
verdict of each side. Use it before pushing a billing change: every change it
lists must be one you meant to make.
"""
import json
import sys


def load(path):
    with open(path, encoding="utf-8") as f:
        return {row["id"]: row for row in map(json.loads, f)}


def shape(row):
    logs = [(entry["type"], entry["prompt"], entry["compl"], entry["cache_read"], entry["cache_write"], entry["cache_5m"],
             entry["cache_1h"], entry["public_failure"]) for entry in row["logs"]]
    return row["paid"], logs


def main():
    base, new = load(sys.argv[1]), load(sys.argv[2])
    changed = 0
    for case_id in sorted(set(base) | set(new)):
        before, after = base.get(case_id), new.get(case_id)
        if before is None or after is None:
            print(f"ONLY IN {'NEW' if before is None else 'BASE'}: {case_id}")
            changed += 1
            continue
        if shape(before) == shape(after):
            continue
        changed += 1
        print(f"## {case_id}  expected={after['expected']}")
        print(f"   base [{before['verdict']}] paid={before['paid']} logs={shape(before)[1]}")
        print(f"   new  [{after['verdict']}] paid={after['paid']} logs={shape(after)[1]}")
    print(f"{changed} changed of {len(set(base) | set(new))} cases")


if __name__ == "__main__":
    main()
