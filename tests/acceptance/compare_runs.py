#!/usr/bin/env python3
"""Compare two acceptance runs (separate processes): every assertion and every scenario
must have the same outcome in both, and a scenario passes only when it passes in both.
Prints one line per scenario and a JSON summary line; exits 1 if the runs disagree."""
import json
import re
import sys


def parse(path):
    tests, asserts, why = {}, {}, {}
    current, said = None, {}
    for line in open(path, encoding="utf-8", errors="replace"):
        r = re.match(r"^=== RUN\s+(Test\w+)$", line.strip())
        if r:
            current = r.group(1)
        f = re.match(r"^\s+(\w+\.go:\d+: .*)", line)
        if f and current:
            said.setdefault(current, []).append(f.group(1)[:400])
        m = re.match(r"^\s*--- (PASS|FAIL|SKIP): (Test\w+)", line)
        if m:
            tests[m.group(2)] = m.group(1)
            if m.group(1) == "FAIL" and m.group(2) in said:
                why[(m.group(2), "test")] = " | ".join(said[m.group(2)][-3:])
        if line.startswith("RESULT "):
            try:
                r = json.loads(line[7:])
            except json.JSONDecodeError:
                continue
            key = (r["scenario"], r["assertion"])
            asserts[key] = asserts.get(key, True) and r["pass"]
            if not r["pass"]:
                why.setdefault(key, r.get("why", ""))
    return tests, asserts, why


def main(a, b):
    ta, aa, wa = parse(a)
    tb, ab, _ = parse(b)
    disagree = []
    for k in sorted(set(aa) | set(ab)):
        if aa.get(k) != ab.get(k):
            disagree.append(f"assertion {k[0]}/{k[1]}: run1={aa.get(k)} run2={ab.get(k)}")
    summary = {}
    for t in sorted(set(ta) | set(tb)):
        x, y = ta.get(t), tb.get(t)
        if x != y:
            disagree.append(f"{t}: run1={x} run2={y}")
        state = "pass" if x == y == "PASS" else ("blocked" if x == y == "SKIP" else "fail")
        summary[t] = state
        print(f"{t:28s} {state:8s} run1={x} run2={y}")
    for (sc, asn), w in sorted(wa.items()):
        if not sc.endswith("/undriven") and "not built" not in w:
            print(f"RED {sc}/{asn}: {w[:300]}")
    print("SUMMARY " + json.dumps(summary, sort_keys=True))
    for d in disagree:
        print("DISAGREE " + d)
    return 1 if disagree else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1], sys.argv[2]))
