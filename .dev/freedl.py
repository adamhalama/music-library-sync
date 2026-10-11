#!/usr/bin/env python3
"""Reusable Free DL maintenance: plan, capture, intake, promote, preserve, status, audit."""
import argparse
from pathlib import Path
import runpy
import sys

COMMANDS = ('plan', 'capture', 'intake', 'promote', 'preserve', 'status', 'audit')
if len(sys.argv) < 2 or sys.argv[1] in ('-h', '--help'):
    parser = argparse.ArgumentParser(description=__doc__, epilog='Run COMMAND --help for flags. Saved --plan files supply job and storage roots.')
    parser.add_argument('command', choices=COMMANDS)
    parser.print_help()
    sys.exit(0 if len(sys.argv) > 1 else 2)
command = sys.argv.pop(1)
if command not in COMMANDS:
    sys.exit(f'Unknown command {command!r}; use --help')
folder = Path(__file__).resolve().with_name('freedl-0912')
sys.path.insert(0, str(folder))
try:
    runpy.run_path(str(folder/(command+'.py')), run_name='__main__')
except (ValueError, RuntimeError, OSError, KeyError, ImportError) as exc:
    sys.exit(f'{command}: {exc}')
except KeyboardInterrupt:
    sys.exit(130)
