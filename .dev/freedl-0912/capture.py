#!/usr/bin/env python3
"""Capture selected remote IDs from a saved Free DL plan (opens authorized gates)."""
import argparse
import json
import sys
from agent import Agent
from context import add_context, resolve_context


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_context(parser, require_plan=True)
    parser.add_argument('--remote-id', action='append', help='Select an exact remote ID; repeat for multiple rows')
    args = parser.parse_args()
    ctx = resolve_context(args)
    plan = ctx.plan
    known = {r['remote_id'] for r in plan['rows'] or [] if r.get('selectable')}
    if args.remote_id:
        unknown = set(args.remote_id)-known
        if unknown:
            raise ValueError(f'Unknown/unselectable remote IDs: {sorted(unknown)}')
        for row in plan['rows']:
            row['selected'] = row['remote_id'] in args.remote_id and row.get('selectable', False)
    selected = [r['remote_id'] for r in plan['rows'] or [] if r.get('selected') and r.get('selectable')]
    if not selected:
        raise ValueError('Nothing selected')
    with Agent(args) as agent:
        resolve_context(args, agent)  # Fail on configuration drift before capture.
        run = agent.call('freedl.capture.start', {'plan': plan, 'selected_remote_ids': selected})['run_id']
        def event(frame):
            method = frame.get('method', '')
            if method == 'run.finished' and frame['params']['run_id'] == run:
                return frame['params']
            if method in ('sync.event', 'sync.progress'):
                p = frame.get('params') or {}
                message = (p.get('event') or p).get('message')
                if message:
                    print(message, file=sys.stderr)
            elif method.startswith('ui.'):
                raise RuntimeError(f'Backend requested {method}; use the native app for interactive capture')
        finish = agent.pump(event)
        print(json.dumps(finish, indent=2))
        if finish.get('exit_code', 0) or finish.get('error'):
            raise RuntimeError(f'Capture failed: {finish.get("error") or finish.get("exit_code")}')


if __name__ == '__main__':
    main()
