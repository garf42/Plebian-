#!/usr/bin/env python3
"""Assemble one node's packet: the constitution, the node's file, its neighbours'
assume and guarantee sections, the seams, prefabs and gaps naming it, and the doc
sections its `spec:` line names. Nothing else.

Usage:  python3 -I build/scripts/packet.py build/ n.grammar > packet.md
"""

import json
import re
import sys
from pathlib import Path

DOCS = ["docs/plebian.md", "docs/agent.md", "docs/tracer-paper-memory.md"]


def section(text, name):
    m = re.search(rf"^##\s+{re.escape(name)}\s*$(.*?)(?=^##\s|\Z)", text, re.M | re.S)
    return m.group(1).strip() if m else ""


def doc_sections(repo, spec):
    """Every doc heading whose id (PL-01, AG-03, TP-03A, ...) or title the spec line names."""
    ids = set(re.findall(r"\b(PL-\d+|AG-\d+|TP-\d+A?)\b", spec))
    for pre, a, b in re.findall(r"\b(PL|AG|TP)-(\d+)\s*…\s*(?:PL|AG|TP)-(\d+)", spec):
        ids |= {f"{pre}-{i:02d}" for i in range(int(a), int(b) + 1)}
    named = [n for n in ("Plebian bindings", "Parameter schedule", "Tests", "Decisions", "Principles", "Sources")
             if n.lower() in spec.lower()]
    tests = re.findall(r"TP-T(\d+)", spec)
    dec = re.findall(r"\bD(\d+)\b", spec)
    out = []
    for d in DOCS:
        text = (repo / d).read_text()
        for m in re.finditer(r"^(##+)\s+(.*)$", text, re.M):
            title = m.group(2)
            hid = re.match(r"(PL-\d+|AG-\d+|TP-\d+A?)\b", title)
            want = (hid and hid.group(1) in ids) or any(title.strip() == n for n in named)
            if tests and title.strip() == "Tests":
                want = True
            if dec and title.strip() == "Decisions":
                want = True
            if not want:
                continue
            level = len(m.group(1))
            rest = text[m.end():]
            stop = re.search(rf"^#{{1,{level}}}\s", rest, re.M)
            body = rest[: stop.start()] if stop else rest
            out.append(f"### {d} — {title}\n{body.strip()}\n")
    return out


def main(argv):
    root = Path(argv[1])
    node = argv[2]
    repo = root.resolve().parent
    nodes = {p.stem: p.read_text() for p in sorted((root / "nodes").glob("*.md"))}
    own = nodes[node]
    seams = [json.loads(l) for l in (root / "seams.jsonl").read_text().splitlines() if l.strip()]
    mine = [s for s in seams if node in s["seam"].split("->")]
    neighbours = sorted({p for s in mine for p in s["seam"].split("->")} - {node})
    prefabs = [json.loads(l) for l in (root / "prefabs.jsonl").read_text().splitlines() if l.strip()]
    gaps = re.split(r"\n(?=- G\d+)", (root / "gaps.md").read_text())
    spec = re.search(r"^spec:\s*(.*)$", own, re.M)

    print("# Packet:", node, "\n")
    print("## Constitution\n")
    print((root / "constitution.md").read_text())
    print(f"\n## Node {node}\n")
    print(own)
    print("\n## Neighbours (assume and guarantee only)\n")
    for n in neighbours:
        if n not in nodes:
            continue
        print(f"### {n}\n#### assume\n{section(nodes[n], 'assume')}\n#### guarantee\n{section(nodes[n], 'guarantee')}\n")
    print("\n## Seams naming this node\n")
    for s in mine:
        print(json.dumps(s, ensure_ascii=False))
    print("\n## Prefabs naming this node\n")
    for p in prefabs:
        if node in p.get("used_by", []):
            print(json.dumps(p, ensure_ascii=False))
    print("\n## Gaps naming this node\n")
    for g in gaps:
        if re.search(rf"node: {re.escape(node)}\b", g) or re.search(rf"\b{re.escape(node)}\b", g.split("Blocks:")[-1]):
            print(g.strip(), "\n")
    print("\n## Doc sections named by the spec line\n")
    for s in doc_sections(repo, spec.group(1) if spec else ""):
        print(s)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
