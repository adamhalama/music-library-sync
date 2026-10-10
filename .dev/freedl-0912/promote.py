#!/usr/bin/env python3
"""Preview/apply promotion for a saved capture, then preserve original metadata."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
from agent import Agent
from context import Context, add_context, resolve_context, context_args, scoped


def repair_replaced_rows(frame, ctx):
    """Repair successes even when another row failed; retain final preservation ledger."""
    result = frame.get('result') or {}
    failures = []
    for row in result.get('rows') or []:
        if row.get('status') != 'replaced':
            continue
        try:
            backup = scoped(row['backup_path'], ctx.backups)
            target = scoped(row['library_path'], ctx.library)
            if not backup.is_file() or not target.is_file():
                raise FileNotFoundError(f'Backup or promoted target missing: {backup} -> {target}')
            subprocess.run([sys.executable, str(Path(__file__).with_name('preserve.py')),
                            *context_args(ctx), 'restore-tags', str(backup), str(target), '--apply'], check=True, capture_output=True, text=True)
        except (KeyError, ValueError, OSError, subprocess.CalledProcessError) as exc:
            detail = exc.stderr.strip() if isinstance(exc, subprocess.CalledProcessError) and exc.stderr else str(exc)
            failures.append(f"{row.get('library_path', '?')}: metadata preservation failed: {detail}")
    failed_rows = [r for r in result.get('rows') or [] if r.get('status') == 'failed']
    if frame.get('error') or frame.get('exit_code', 0) != 0 or result.get('failed', 0) or failed_rows:
        failures.append(f"Promotion failed or partially completed: {frame.get('error') or failed_rows or frame.get('exit_code')}")
    if not isinstance(frame.get('result'), dict):
        failures.append('Promotion returned no result; inspect its native ledger')
    if failures:
        raise RuntimeError('\n'.join(failures))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_context(parser, require_plan=True)
    parser.add_argument('--apply', action='store_true', help='Apply selected upgrades and metadata preservation')
    parser.add_argument('--target-format', choices=('auto', 'aac-256', 'mp3-320', 'wav'), help='Override saved job format')
    args = parser.parse_args()
    ctx = resolve_context(args)
    with Agent(args) as agent:
        resolve_context(args, agent)
        def finish(run):
            def event(frame):
                if frame.get('method') == 'run.finished' and frame['params']['run_id'] == run:
                    return frame['params']
            return agent.pump(event)
        params = {'job_id': ctx.job_id, 'capture_run_id': ctx.plan['run_id']}
        if args.target_format:
            params['target_format'] = args.target_format
        fin = finish(agent.call('freedl.promotionPlan.build', params)['run_id'])
        if fin.get('error') or fin.get('exit_code', 0):
            raise RuntimeError(f"Plan build failed: {fin.get('error') or fin.get('exit_code')}")
        plan = fin['result']
        promotion = Context.from_plan(ctx.logs/plan['run_id']/'promotion-plan.json')
        ctx.same_job(promotion)
        # Expose exact paths/action/qualities in one JSON document.
        selected = [r for r in plan['rows'] or [] if r.get('selected')]
        if args.apply and selected:
            from mutagen.mp4 import MP4  # dependency check before native mutation
            fin = finish(agent.call('freedl.promote.apply', {'plan': plan})['run_id'])
            try:
                repair_replaced_rows(fin, promotion)
            finally:
                print(json.dumps({'plan': plan, 'result': fin}, indent=2))
        else:
            print(json.dumps(plan, indent=2))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, OSError, KeyError, ImportError) as exc:
        sys.exit(str(exc))
