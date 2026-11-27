#!/usr/bin/env python3
"""Counts for build/aar.md, read from the records: nodes, demons by status, ledger
entries by node, gaps, seams by status, prefabs probed, test functions. Prints JSON."""
import json
import re
import sys
from pathlib import Path


def main(root):
    b = Path(root)
    nodes = sorted(p.stem for p in (b / "nodes").glob("*.md"))
    demons = {"killed": [], "pending": []}
    for p in (b / "nodes").glob("*.md"):
        for m in re.finditer(r"-\s*(D\d+):.*?negative_check:\s*(.*?)\n\s*status:\s*(\w+)", p.read_text(), re.S):
            (demons["killed"] if m.group(3) == "killed" else demons["pending"]).append(f"{p.stem}/{m.group(1)}")
    ledger = [json.loads(l) for l in (b / "ledger.jsonl").read_text().splitlines() if l.strip()]
    by_node = {}
    for e in ledger:
        by_node[e.get("node", "?")] = by_node.get(e.get("node", "?"), 0) + 1
    gaps = re.findall(r"^- (G\d+) — node: ([\w.]+)", (b / "gaps.md").read_text(), re.M)
    seams = [json.loads(l) for l in (b / "seams.jsonl").read_text().splitlines() if l.strip()]
    seam_status = {}
    for s in seams:
        seam_status[s.get("status")] = seam_status.get(s.get("status"), 0) + 1
    prefabs = [json.loads(l) for l in (b / "prefabs.jsonl").read_text().splitlines() if l.strip()]
    tests = 0
    for p in Path(root).parent.joinpath("tests").rglob("*_test.go"):
        tests += len(re.findall(r"^func Test\w+\(", p.read_text(errors="replace"), re.M))
    print(json.dumps({
        "nodes": len(nodes), "demons_killed": len(demons["killed"]), "demons_pending": demons["pending"],
        "ledger_entries": len(ledger), "ledger_by_node": by_node,
        "gaps": len(gaps), "gaps_by_node": {n: sum(1 for _, x in gaps if x == n) for n in sorted({x for _, x in gaps})},
        "seams": seam_status, "prefabs": len(prefabs), "prefabs_probed": sum(1 for p in prefabs if p.get("probed")),
        "test_functions": tests,
    }, indent=1))


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "build")
