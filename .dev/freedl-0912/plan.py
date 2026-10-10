#!/usr/bin/env python3
"""Build a Free DL capture plan for an explicitly selected configured job."""
import argparse
import json
import sys
from agent import Agent
from context import Context, add_context, resolve_context


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_context(parser)
    parser.add_argument('--limit', type=int, help='Override job plan_limit')
    parser.add_argument('--playlist-id', help='Use a saved standalone playlist snapshot')
    args = parser.parse_args()
    if args.limit is not None and args.limit < 1:
        parser.error('--limit must be positive')
    with Agent(args) as agent:
        ctx = resolve_context(args, agent)
        params = {'job_id': ctx.job_id}
        if args.limit is not None:
            params['plan_limit'] = args.limit
        if args.playlist_id:
            params['playlist_id'] = args.playlist_id
        run = agent.call('freedl.plan.start', params)['run_id']
        plans = []
        def event(frame):
            if frame.get('method') == 'freedl.plan.event' and frame['params']['run_id'] == run:
                ev = frame['params']['event']
                if ev['kind'] == 'done':
                    plans.append(ev['plan'])
                elif ev['kind'] in ('stage', 'failed'):
                    print(ev.get('detail') or ev.get('error') or ev.get('stage'), file=sys.stderr)
            if frame.get('method') == 'run.finished' and frame['params']['run_id'] == run:
                return frame['params']
        finish = agent.pump(event)
        if finish.get('exit_code', 0) or finish.get('error') or not plans:
            raise RuntimeError(f'No completed plan: {finish.get("error") or finish.get("exit_code")}')
        path = ctx.capture_plan(plans[-1]['run_id'])
        saved = Context.from_plan(path)
        ctx.same_job(saved)
        agent.save_context(path.parent)
        print(json.dumps({'job_id': ctx.job_id, 'run_id': saved.plan['run_id'], 'plan': str(path),
                          'rows': len(saved.plan['rows'] or []),
                          'selected': sum(bool(r.get('selected')) for r in saved.plan['rows'] or [])}, indent=2))


if __name__ == '__main__':
    main()
