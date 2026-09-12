#!/usr/bin/env python3
"""Build (and optionally apply) the Free DL promotion plan for a capture run."""
import json, sys
sys.path.insert(0, "/Users/jaa/dev/utils/update-downloads/.dev/freedl-0912")
from agent import Agent

capture_run = sys.argv[1]
apply = "--apply" in sys.argv

a = Agent()
a.call("session.initialize", {"protocol_version": 2})

def finish(rid):
    def on_frame(f):
        if f.get("method") == "run.finished" and f["params"]["run_id"] == rid:
            return f["params"]
        return None
    return a.pump(on_frame)

res = a.call("freedl.promotionPlan.build",
             {"job_id": "upgrade-09-12", "capture_run_id": capture_run, "target_format": "auto"})
fin = finish(res["run_id"])
if fin.get("error"):
    sys.exit(f"plan build failed: {fin['error']}")
plan = fin["result"]

print(f"{'#':>3} {'action':<13} {'score':>5} {'orig':>6} {'new':>6} {'sel':<4} title")
for r in plan["rows"]:
    oq = (r.get("original_quality") or {}).get("effective_bitrate") or 0
    sq = (r.get("source_quality") or {})
    sqb = sq.get("effective_bitrate") or 0
    newq = "LOSSLESS" if sq.get("lossless") else f"{sqb//1000}k"
    print(f"{r['index']:>3} {r['action']:<13} {r['score']:>5} {oq//1000:>5}k {newq:>6} "
          f"{'yes' if r['selected'] else 'no':<4} {r['title']}  {r.get('reason','')}")
sel = [r for r in plan["rows"] if r["selected"]]
print(f"\nrows={len(plan['rows'])} selected={len(sel)} backup_root={plan['backup_root']}")

if apply and sel:
    res = a.call("freedl.promote.apply", {"plan": plan})
    fin = finish(res["run_id"])
    print("\nAPPLY:", json.dumps(fin, indent=2)[:3000])
a.close()
