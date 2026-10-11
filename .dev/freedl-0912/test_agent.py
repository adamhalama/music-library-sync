"""Maintenance RPC ordering and planning acceptance; no network or user media."""
import io
import json
from pathlib import Path
import queue
import subprocess
import sys
import tempfile
import unittest

from agent import Agent
from context import REPO
from test_context import fixture


class AgentTests(unittest.TestCase):
    def driver(self, frames):
        agent = Agent.__new__(Agent)
        agent.q = queue.Queue()
        for frame in frames:
            agent.q.put(frame)
        agent.q.put(None)
        agent._id = 0
        agent.p = type('Transport', (), {'stdin': io.StringIO()})()
        # Match the production initialization without opening a subprocess.
        from collections import deque
        agent.pending = deque()
        return agent

    def test_completion_before_response_is_replayed_in_order(self):
        frames = [{'method': 'sync.progress', 'params': {'sequence': 1}},
                  {'method': 'run.finished', 'params': {'run_id': 'fast-run', 'exit_code': 0}},
                  {'id': 1, 'result': {'run_id': 'fast-run'}}]
        agent = self.driver(frames)
        self.assertEqual(agent.call('freedl.promote.apply')['run_id'], 'fast-run')
        seen = []
        def event(frame):
            seen.append(frame)
            if frame.get('method') == 'run.finished':
                return frame['params']
        self.assertEqual(agent.pump(event)['exit_code'], 0)
        self.assertEqual(seen, frames[:2])

    def test_explicit_notification_handler_does_not_replay_frames(self):
        notification = {'method': 'sync.progress', 'params': {}}
        agent = self.driver([notification, {'id': 1, 'result': {}}])
        seen = []
        agent.call('session.initialize', on_notify=seen.append)
        self.assertEqual(seen, [notification])
        with self.assertRaisesRegex(RuntimeError, 'closed the connection'):
            agent.pump(lambda frame: frame)

    def test_new_plan_uses_wire_events_and_saves_connection(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d).resolve()
            ctx = fixture(root)
            backend = root/'udl'
            # Emit every event before the start response, as StartRun may do.
            backend.write_text('#!/usr/bin/env python3\n' +
                'import json, sys\n' +
                'plan = ' + repr(ctx.plan) + '\n' +
                'def emit(frame):\n    print(json.dumps(frame), flush=True)\n' +
                'for line in sys.stdin:\n' +
                '    frame = json.loads(line)\n' +
                '    method = frame["method"]\n' +
                '    result = {}\n' +
                '    if method == "freedl.config.read":\n' +
                '        result = {"config": {"jobs": [plan["job"]]}}\n' +
                '    elif method == "freedl.plan.start":\n' +
                '        run = plan["run_id"]\n' +
                '        emit({"method": "freedl.planEvent", "params": {"run_id": run, "event": {"kind": "done", "plan": plan}}})\n' +
                '        emit({"method": "run.finished", "params": {"run_id": run, "exit_code": 0, "result": plan}})\n' +
                '        result = {"run_id": run}\n' +
                '    emit({"id": frame["id"], "result": result})\n')
            backend.chmod(0o755)
            main, config = root/'main.yaml', root/'freedl.yaml'
            result = subprocess.run([sys.executable, str(REPO/'.dev/freedl.py'),
                'plan', '--job', ctx.job_id, '--udl', str(backend),
                '-c', str(main), '--freedl-config', str(config), '--working-dir', str(root)],
                capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            summary = json.loads(result.stdout)
            self.assertEqual(summary['run_id'], ctx.plan['run_id'])
            self.assertEqual(summary['plan'], str(ctx.plan_path))
            self.assertEqual(summary['selected'], 1)
            saved = json.loads((ctx.plan_path.parent/'maintenance-context.json').read_text())
            self.assertEqual(saved, {'job_id': ctx.job_id, 'udl': str(backend),
                'config': str(main), 'freedl_config': str(config), 'working_dir': str(root)})


if __name__ == '__main__':
    unittest.main()
