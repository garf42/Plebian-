#!/usr/bin/env python3
"""D3 (n.proof): a scenario that passes in one run and fails in the other fails the gate;
it never passes on a retry. Run: python3 -I tests/acceptance/test_compare_runs.py"""
import contextlib
import io
import json
import os
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import compare_runs  # noqa: E402

PASS = "=== RUN   TestA05\nRESULT {\"scenario\":\"A05\",\"assertion\":\"words_heard\",\"pass\":true}\n--- PASS: TestA05 (1.00s)\n"
FAIL = "=== RUN   TestA05\nRESULT {\"scenario\":\"A05\",\"assertion\":\"words_heard\",\"pass\":false,\"why\":\"heard nothing\"}\n--- FAIL: TestA05 (1.00s)\n"


def run(a, b):
    with tempfile.TemporaryDirectory() as d:
        pa, pb = os.path.join(d, "1.log"), os.path.join(d, "2.log")
        open(pa, "w").write(a)
        open(pb, "w").write(b)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = compare_runs.main(pa, pb)
    summary = json.loads([l for l in out.getvalue().splitlines() if l.startswith("SUMMARY ")][0][8:])
    return code, summary


for a, b in ((PASS, FAIL), (FAIL, PASS)):
    code, s = run(a, b)
    if code == 0 or s.get("TestA05") != "fail":
        sys.exit(f"test_compare_runs.py: a scenario that passed in one run of two came out {s.get('TestA05')!r} (exit {code})")
code, s = run(PASS, PASS)
if code != 0 or s.get("TestA05") != "pass":
    sys.exit(f"test_compare_runs.py: a scenario that passed in both runs came out {s.get('TestA05')!r} (exit {code})")
print("ok: one passing run of two fails the scenario")
