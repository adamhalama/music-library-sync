#!/usr/bin/env python3
"""Report which batch tracks are upgraded, pending a gate click, or not upgradable."""
import json, os, glob, subprocess

SCRATCH = os.path.expanduser("~/dev/music-down/statefiles/freedl/upgrade-09-12")
LIB = os.path.expanduser("~/Music/downloaded/sc-likes-today-09-12")

plans = sorted(glob.glob(f"{SCRATCH}/logs/*/capture-plan.json"))
plan = json.load(open(plans[-1]))
# The likes window includes two tracks absent from this batch. Scope by actual
# local paths, not enumeration index, so the report describes the 46 files.
# Explicit original-format migrations retain identity by their logged old path.
for evidence in sorted(glob.glob(f"{SCRATCH}/logs/*/preservation-result.json")):
    migration = json.load(open(evidence))
    if migration.get("action") == "install-mp3":
        for row in plan["rows"]:
            if row.get("local_path") == migration["before"]["path"]:
                row["local_path"] = migration["after"]["path"]
rows = [r for r in plan["rows"]
        if r.get("local_path") and os.path.isfile(r["local_path"])
        and os.path.dirname(os.path.realpath(r["local_path"])) == os.path.realpath(LIB)]

captured = {}   # remote_id -> downloaded filename
for state in sorted(glob.glob(f"{SCRATCH}/logs/*/capture.sync.scdl")):
    for line in open(state):
        parts = line.strip().split(None, 2)
        if len(parts) == 3:
            captured[parts[1]] = parts[2]

promoted = set()
for res in sorted(glob.glob(f"{SCRATCH}/logs/*/promotion-result.json")):
    for r in json.load(open(res))["rows"]:
        if r["status"] == "replaced":
            promoted.add(os.path.basename(r["library_path"]))

for evidence in sorted(glob.glob(f"{SCRATCH}/logs/*/preservation-result.json")):
    migration = json.load(open(evidence))
    if migration.get("action") == "install-mp3":
        promoted.add(os.path.basename(migration["after"]["path"]))

def kbps(path):
    try:
        out = subprocess.run(["ffprobe","-hide_banner","-loglevel","error",
                              "-select_streams","a:0","-show_entries","stream=bit_rate","-of","csv=p=0",path],
                             capture_output=True, text=True, timeout=10).stdout.strip()
        return int(out)//1000 if out.isdigit() else 0
    except Exception:
        return 0

up, pend, no = [], [], []
for r in rows:
    lp = r.get("local_path") or ""
    name = os.path.basename(lp) if lp else None
    if name and name in promoted:
        up.append((r, kbps(lp)))
    elif r["free_dl_probe"]["status"] == "available":
        pend.append(r)
    else:
        no.append(r)

print(f"UPGRADED ({len(up)})")
for r, k in up:
    print(f"  {r['index']:>3}  {k:>4} kbps  {r['title']}")
print(f"\nPENDING A GATE CLICK ({len(pend)})")
for r in pend:
    got = " [captured, not yet promoted]" if r["remote_id"] in captured else ""
    print(f"  {r['index']:>3}  {r['free_dl_probe']['host']:<16} {r['title']}{got}")
    print(f"       {r['free_dl_probe']['purchase_url']}")
print(f"\nNOT UPGRADABLE ({len(no)})")
from collections import Counter
c = Counter((r["free_dl_probe"]["status"], r["free_dl_probe"].get("host","")) for r in no)
for (st, host), n in sorted(c.items(), key=lambda x: -x[1]):
    print(f"  {n:>3}  {st:<17} {host}")
