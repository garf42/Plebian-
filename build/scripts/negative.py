#!/usr/bin/env python3
"""Record a demon's negative check in its node file: the kill test was seen red with the
demon installed. Usage: negative.py build/ n.grammar D1 "<red line>" """
import re
import sys
from pathlib import Path


def main(root, node, demon, line):
    p = Path(root) / "nodes" / f"{node}.md"
    text = p.read_text()
    m = re.search(rf"(-\s*{demon}:.*?negative_check:)\s*pending(.*?status:)\s*live", text, re.S)
    if not m:
        print(f"{node}/{demon}: no pending negative check", file=sys.stderr)
        return 1
    line = line.replace("\n", " ")[:160]
    text = text[: m.start()] + m.group(1) + f" seen red — {line}" + m.group(2) + " killed" + text[m.end():]
    p.write_text(text)
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:5]))
