#!/usr/bin/env python3
"""Minimal NDJSON driver for `udl agent`, used to run the Free DL workflow headlessly."""
import json, os, subprocess, sys, threading, queue

REPO = "/Users/jaa/dev/utils/update-downloads"
SCRATCH = os.path.expanduser("~/dev/music-down/statefiles/freedl/upgrade-09-12")


class Agent:
    def __init__(self, extra_args=None):
        args = [f"{REPO}/bin/udl",
                "-c", f"{SCRATCH}/config.yaml",
                "--freedl-config", f"{SCRATCH}/freedl.yaml",
                "agent", "--working-dir", SCRATCH]
        env = dict(os.environ)
        env.setdefault("UDL_FREEDL_BROWSER_IDLE_TIMEOUT", "300s")
        env.update(extra_args or {})
        self.p = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=open(f"{SCRATCH}/logs/agent.stderr.log", "ab"),
                                  text=True, bufsize=1, env=env)
        self.q = queue.Queue()
        self._id = 0
        threading.Thread(target=self._reader, daemon=True).start()

    def _reader(self):
        for line in self.p.stdout:
            line = line.strip()
            if line:
                self.q.put(json.loads(line))
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
            if frame.get("id") == want and ("result" in frame or "error" in frame):
                if "error" in frame:
                    raise RuntimeError(f"{method} failed: {frame['error']}")
                return frame["result"]
            if on_notify:
                on_notify(frame)

    def pump(self, on_frame):
        """Pump frames until on_frame returns a value."""
        while True:
            frame = self.q.get()
            if frame is None:
                raise RuntimeError("agent closed the connection")
            out = on_frame(frame)
            if out is not None:
                return out

    def close(self):
        try:
            self.call("session.shutdown", {})
        except Exception:
            pass
        self.p.terminate()
