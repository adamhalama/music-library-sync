#!/usr/bin/env python3
"""Run freedl.capture.start for selected rows of a saved capture plan."""
import json, sys, os
sys.path.insert(0, "/Users/jaa/dev/utils/update-downloads/.dev/freedl-0912")
from agent import Agent, SCRATCH

plan_path = sys.argv[1]
ids = sys.argv[2:]
plan = json.load(open(plan_path))
if ids:
    keep = set(ids)
    for r in plan["rows"]:
        r["selected"] = r["remote_id"] in keep and r.get("selectable", False)
sel = [r for r in plan["rows"] if r.get("selected")]
print(f"capturing {len(sel)} row(s):", file=sys.stderr)
for r in sel:
    print(f"  {r['index']:>3} {r['title']}  -> {r['free_dl_probe'].get('purchase_url')}", file=sys.stderr)
if not sel:
    sys.exit("nothing selected")

a = Agent()
a.call("session.initialize", {"protocol_version": 2})
res = a.call("freedl.capture.start", {"plan": plan, "selected_remote_ids": [r["remote_id"] for r in sel]})
run = res["run_id"]
print(f"capture run {run}", file=sys.stderr)

def on_frame(frame):
    m = frame.get("method")
    if m == "run.finished":
        return frame["params"]
    if m in ("sync.event", "sync.progress"):
        p = frame.get("params", {})
        ev = p.get("event", p)
        msg = ev.get("message")
        if msg:
            print(f"  {msg}", flush=True)
    elif m and m.startswith("ui."):
        a.reply(frame["id"], {"confirmed": True})
    return None

fin = a.pump(on_frame)
print("\nFINISHED:", json.dumps(fin, indent=2)[:4000])
a.close()
