#!/usr/bin/env python3
"""Minimal NDJSON driver for `udl agent`, used to run the Free DL workflow headlessly."""
import json, os, subprocess, sys, threading, queue
from collections import deque

from pathlib import Path
from context import read_connection


class Agent:
    def __init__(self, options, extra_args=None):
        self.options = options
        self.connection = read_connection(options)
        working, binary = self.connection['working_dir'], self.connection['udl']
        args = [str(binary), '-c', str(self.connection['config'])]
        if self.connection['freedl_config']:
            args += ['--freedl-config', str(self.connection['freedl_config'])]
        args += ['agent', '--working-dir', str(working)]
        env = dict(os.environ)
        env.setdefault('UDL_FREEDL_BROWSER_IDLE_TIMEOUT', '300s')
        env.update(extra_args or {})
        # Inherit stderr instead of creating a log in an assumed scratch root.
        self.p = subprocess.Popen(args, cwd=working, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  text=True, bufsize=1, env=env)
        self.q = queue.Queue()
        self.pending = deque()
        self._id = 0
        threading.Thread(target=self._reader, daemon=True).start()

    def _reader(self):
        for line in self.p.stdout:
            line = line.strip()
            if line:
                try:
                    self.q.put(json.loads(line))
                except json.JSONDecodeError:
                    self.q.put({'error': {'message': 'Backend emitted an invalid JSON frame'}})
        self.q.put(None)

    def send(self, method, params=None):
        self._id += 1
        frame = {"jsonrpc": "2.0", "id": self._id, "method": method}
        if params is not None:
            frame["params"] = params
        self.p.stdin.write(json.dumps(frame) + "\n")
        self.p.stdin.flush()
        return self._id

    def reply(self, rid, result):
        self.p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": rid, "result": result}) + "\n")
        self.p.stdin.flush()

    def call(self, method, params=None, on_notify=None):
        """Send a request and pump frames until its response arrives."""
        want = self.send(method, params)
        while True:
            frame = self.q.get()
            if frame is None:
                raise RuntimeError("agent closed the connection")
            if 'error' in frame and 'id' not in frame:
                raise RuntimeError(frame['error']['message'])
            if frame.get("id") == want and ("result" in frame or "error" in frame):
                if "error" in frame:
                    raise RuntimeError(f"{method} failed: {frame['error']}")
                return frame["result"]
            if on_notify:
                on_notify(frame)
            else:
                # StartRun can emit lifecycle/terminal events before its response.
                # Preserve those frames for the workflow's subsequent pump.
                self.pending.append(frame)

    def pump(self, on_frame):
        """Pump frames until on_frame returns a value."""
        while True:
            frame = self.pending.popleft() if self.pending else self.q.get()
            if frame is None:
                raise RuntimeError("agent closed the connection")
            if 'error' in frame and 'id' not in frame:
                raise RuntimeError(frame['error']['message'])
            out = on_frame(frame)
            if out is not None:
                return out

    def close(self):
        if self.p.poll() is None:
            self.p.terminate()
            try:
                self.p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.p.kill()
                self.p.wait(timeout=5)
        self.p.stdin.close()
        self.p.stdout.close()

    def __enter__(self):
        try:
            self.call('session.initialize', {'protocol_version': 2})
            return self
        except BaseException:
            self.close()
            raise

    def __exit__(self, *args):
        self.close()

    def save_context(self, run_dir):
        from context import Context
        from preserve import write_evidence
        ctx = Context.from_plan(run_dir/'capture-plan.json')
        write_evidence(run_dir/'maintenance-context.json',
                       {'job_id': ctx.job_id, **{k: str(v) if v else None for k, v in self.connection.items()
                                               if k != 'udl' or self.options.udl is not None}})
