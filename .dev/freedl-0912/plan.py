#!/usr/bin/env python3
"""Run freedl.plan.start and dump the resulting capture plan."""
import json, sys
sys.path.insert(0, "/Users/jaa/dev/utils/update-downloads/.dev/freedl-0912")
from agent import Agent, SCRATCH

a = Agent()
init = a.call("session.initialize", {"protocol_version": 2})
print(f"protocol {init['protocol_version']} build {init.get('build')}", file=sys.stderr)

limit = int(sys.argv[1]) if len(sys.argv) > 1 else 50
res = a.call("freedl.plan.start", {"job_id": "upgrade-09-12", "plan_limit": limit})
run_id = res["run_id"]
print(f"run {run_id} limit {limit}", file=sys.stderr)

plan = {}
def on_frame(frame):
    m = frame.get("method")
    if m == "freedl.plan.event":
        ev = frame["params"]["event"]
        k = ev["kind"]
        if k == "stage":
            print(f"  [{ev['stage']}] {ev['status']} {ev.get('detail','')}", file=sys.stderr)
        elif k == "done":
            plan.update(ev["plan"])
        elif k == "failed":
            print(f"  PLAN FAILED: {ev.get('error')}", file=sys.stderr)
    elif m == "run.finished":
        return frame["params"]
    return None

fin = a.pump(on_frame)
print(f"finished: {json.dumps(fin)}", file=sys.stderr)
a.close()

if not plan:
    sys.exit("no plan produced")
with open(f"{SCRATCH}/logs/plan-latest.json", "w") as fh:
    json.dump(plan, fh, indent=2)
rows = plan["rows"]
print(f"{'#':>3} {'status':<17} {'host':<22} {'local kbps':>10}  title")
for r in rows:
    q = r.get("local_quality") or {}
    kbps = q.get("effective_bitrate") or q.get("bitrate") or 0
    print(f"{r['index']:>3} {r['free_dl_probe'].get('status',''):<17} "
          f"{r['free_dl_probe'].get('host',''):<22} "
          f"{(kbps//1000 if kbps else 0):>7}{'' if r.get('local_path') else ' NOLOC':>3}  {r['title']}")
sel = [r for r in rows if r.get("selected")]
print(f"\nrows={len(rows)} selected={len(sel)}")
print(f"plan run_id={plan['run_id']} buffer={plan['buffer_root']}")
