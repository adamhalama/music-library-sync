#!/usr/bin/env python3
"""Build/apply a batch promotion, then verify and restore original MP4 metadata."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
from agent import Agent
from audit import LIB, SCRATCH
from preserve import scoped


def repair_replaced_rows(frame):
    """Repair successful rows even when other rows or the run failed."""
    result = frame.get('result') or {}
    failures = []
    for row in result.get('rows') or []:
        if row.get('status') != 'replaced':
            continue
        try:
            backup = scoped(Path(row['backup_path']), SCRATCH/'backups')
            target = scoped(Path(row['library_path']), LIB)
            if not backup.is_file() or not target.is_file():
                raise FileNotFoundError(f'Backup or promoted target missing: {backup} -> {target}')
            subprocess.run([sys.executable, str(Path(__file__).with_name('preserve.py')),
                            'restore-tags', str(backup), str(target)], check=True)
        except (KeyError, ValueError, OSError, subprocess.CalledProcessError) as exc:
            failures.append(f"{row.get('library_path', '?')}: metadata preservation failed: {exc}")
    failed_rows = [r for r in result.get('rows') or [] if r.get('status') == 'failed']
    if frame.get('error') or frame.get('exit_code', 0) != 0 or result.get('failed', 0) or failed_rows:
        failures.append(f"Promotion failed or partially completed: {frame.get('error') or failed_rows or frame.get('exit_code')}")
    if not isinstance(frame.get('result'), dict):
        failures.append('Promotion returned no result; inspect its native result ledger before continuing')
    if failures:
        raise RuntimeError('\n'.join(failures))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('capture_run')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    a = Agent()
    try:
        a.call('session.initialize', {'protocol_version': 2})
        def finish(rid):
            def on_frame(f):
                if f.get('method') == 'run.finished' and f['params']['run_id'] == rid:
                    return f['params']
                return None
            return a.pump(on_frame)
        res = a.call('freedl.promotionPlan.build',
                     {'job_id': 'upgrade-09-12', 'capture_run_id': args.capture_run, 'target_format': 'auto'})
        fin = finish(res['run_id'])
        if fin.get('error') or fin.get('exit_code', 0) != 0:
            raise RuntimeError(f"Plan build failed: {fin.get('error') or fin.get('exit_code')}")
        plan = fin['result']
        print(f"{'#':>3} {'action':<13} {'score':>5} {'orig':>6} {'new':>6} {'sel':<4} title")
        for r in plan['rows']:
            oq = (r.get('original_quality') or {}).get('effective_bitrate') or 0
            sq = r.get('source_quality') or {}
            sqb = sq.get('effective_bitrate') or 0
            newq = 'LOSSLESS' if sq.get('lossless') else f'{sqb//1000}k'
            print(f"{r['index']:>3} {r['action']:<13} {r['score']:>5} {oq//1000:>5}k {newq:>6} "
                  f"{'yes' if r['selected'] else 'no':<4} {r['title']}  {r.get('reason','')}")
        selected = [r for r in plan['rows'] if r['selected']]
        print(f"\nrows={len(plan['rows'])} selected={len(selected)} backup_root={plan['backup_root']}")
        if args.apply and selected:
            # Check the local preservation dependency before the native mutation.
            sys.path.insert(0, str(SCRATCH/'pythonlib'))
            import mutagen.mp4  # noqa: F401
            res = a.call('freedl.promote.apply', {'plan': plan})
            fin = finish(res['run_id'])
            print('\nAPPLY:', json.dumps(fin, indent=2)[:3000])
            repair_replaced_rows(fin)
    finally:
        a.close()


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, KeyError, ImportError) as exc:
        sys.exit(str(exc))
