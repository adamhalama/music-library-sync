"""Validated Free DL job/run context, shared by maintenance commands.

Saved native plans are the portable context; no YAML parser or historical paths.
"""
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re

REPO = Path(__file__).resolve().parents[2]
ROOT_FIELDS = ('library_dir', 'buffer_dir', 'backup_dir', 'log_dir')


def safe_id(value, label='run ID'):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', value):
        raise ValueError(f'Invalid {label}: expected a single safe path component')
    return value


def absolute(value, label='path'):
    if not isinstance(value, (str, Path)) or not str(value).strip():
        raise ValueError(f'Missing {label}')
    expanded = Path(os.path.expandvars(str(value))).expanduser()
    if not expanded.is_absolute() or '$' in str(expanded):
        raise ValueError(f'{label} must resolve to an absolute path: {value}')
    return expanded.resolve()


def scoped(path, root):
    path, root = absolute(path), absolute(root)
    if path == root or not path.is_relative_to(root):
        raise ValueError(f'Outside Free DL scope {root}: {path}')
    return path


@dataclass(frozen=True)
class Context:
    job: dict
    library: Path
    buffer: Path
    backups: Path
    logs: Path
    plan: dict | None = None
    plan_path: Path | None = None

    @property
    def job_id(self):
        return self.job['id']

    @classmethod
    def from_job(cls, job):
        safe_id(job.get('id'), 'job ID')
        roots = [absolute(job.get(key), key) for key in ROOT_FIELDS]
        # Metadata and originals must never overlap the mutable library, or each
        # other. Different configured storage volumes do not need a common parent.
        for i, root in enumerate(roots):
            if root == Path(root.anchor):
                raise ValueError('A Free DL storage root cannot be a filesystem root')
            for other in roots[i+1:]:
                if root.is_relative_to(other) or other.is_relative_to(root):
                    raise ValueError(f'Overlapping Free DL storage roots: {root}, {other}')
        if not job.get('source_url') or not job.get('state_file'):
            raise ValueError('Saved job is missing source_url or state_file')
        if not roots[0].is_dir():
            raise ValueError(f'Library directory does not exist: {roots[0]}')
        return cls(dict(job), *roots)

    @classmethod
    def from_plan(cls, path):
        path = Path(os.path.expandvars(str(path))).expanduser().resolve()
        if path.is_dir():
            path = path/'capture-plan.json'
        plan = json.loads(path.read_text())
        ctx = cls.from_job(plan.get('job') or {})
        run = safe_id(plan.get('run_id'))
        for root in (ctx.buffer, ctx.backups, ctx.logs):
            scoped(root/run, root)
        for key, expected in [('buffer_root', ctx.buffer/run), ('backup_root', ctx.backups/run), ('log_dir', ctx.logs/run)]:
            if key in plan and absolute(plan[key], key) != scoped(expected, expected.parent):
                raise ValueError(f'Saved plan {key} conflicts with its job/run')
        remotes = set()
        for row in plan.get('rows') or []:
            if row.get('remote_id'):
                remote = row['remote_id']
                if not isinstance(remote, str) or not remote.isdigit() or remote in remotes:
                    raise ValueError('Saved capture plan has invalid or duplicate remote IDs')
                remotes.add(remote)
            for key in ('local_path', 'library_path', 'output_path'):
                if row.get(key):
                    scoped(row[key], ctx.library)
            if row.get('backup_path'):
                scoped(row['backup_path'], ctx.backups/run)
            if row.get('free_dl_path'):
                scoped(row['free_dl_path'], ctx.buffer/run)
        return cls(ctx.job, ctx.library, ctx.buffer, ctx.backups, ctx.logs, plan, path)

    def same_job(self, other):
        for key in ('id', 'source_url', 'state_file'):
            if self.job.get(key) != other.job.get(key):
                raise ValueError(f'Saved and configured jobs conflict on {key}; regenerate the plan')
        if (self.library, self.buffer, self.backups, self.logs) != (other.library, other.buffer, other.backups, other.logs):
            raise ValueError('Saved and configured job storage roots conflict; regenerate the plan')

    def capture_plan(self, run):
        return scoped(self.logs/safe_id(run)/'capture-plan.json', self.logs)

    def plans(self):
        paths = sorted(self.logs.glob('*/capture-plan.json'), key=lambda p: (p.stat().st_mtime_ns, str(p)))
        result = []
        for path in paths:
            other = Context.from_plan(path)
            self.same_job(other)
            result.append(other.plan)
        if self.plan and any(r.get('remote_id') for r in self.plan.get('rows') or []):
            if not any(plan['run_id'] == self.plan['run_id'] for plan in result):
                result.append(self.plan)
        return result


def add_context(parser, require_plan=False):
    parser.add_argument('--plan', type=Path, required=require_plan,
                        help='Saved capture/promotion JSON or capture run directory; supplies job and roots')
    parser.add_argument('--job', help='Configured job ID; must agree with --plan when both supplied')
    parser.add_argument('-c', '--config', type=Path, help='Main udl YAML (default: user config, never checkout sample)')
    parser.add_argument('--freedl-config', type=Path, help='Free DL YAML; otherwise backend user-config discovery')
    parser.add_argument('--udl', type=Path, default=None, help='Backend binary (default: this checkout/bin/udl)')
    parser.add_argument('--working-dir', type=Path, default=None, help='Backend working directory (default: home)')


def resolve_context(args, agent=None):
    ctx = Context.from_plan(args.plan) if args.plan else None
    if ctx and args.job and args.job != ctx.job_id:
        raise ValueError('--job conflicts with the saved plan job')
    if agent is not None:
        response = agent.call('freedl.config.read')
        jobs = (response.get('config') or {}).get('jobs') or []
        wanted = args.job or (ctx.job_id if ctx else None)
        if not wanted:
            raise ValueError('--job is required for a new run; select a configured Free DL job')
        matches = [job for job in jobs if job.get('id') == wanted]
        if len(matches) != 1:
            raise ValueError(f'Expected exactly one configured Free DL job: {wanted}')
        configured = Context.from_job(matches[0])
        if ctx:
            ctx.same_job(configured)
        else:
            ctx = configured
    if not ctx:
        raise ValueError('--plan is required for offline maintenance; --job alone needs the backend')
    return ctx


def context_args(ctx):
    if not ctx.plan_path:
        raise ValueError('A saved plan is required for preservation')
    return ['--plan', str(ctx.plan_path)]


def read_connection(options):
    saved = {}
    if options.plan:
        ctx = Context.from_plan(options.plan)
        sidecar = scoped(ctx.logs/ctx.plan['run_id']/'maintenance-context.json', ctx.logs)
        if sidecar.exists():
            saved = json.loads(sidecar.read_text())
            if saved.get('job_id') != ctx.job_id:
                raise ValueError('Connection context belongs to another job')
    defaults = {'working_dir': Path.home(), 'udl': REPO/'bin/udl',
                'config': Path.home()/'.config/udl/config.yaml', 'freedl_config': None}
    resolved = {}
    for key, default in defaults.items():
        explicit = getattr(options, key, None)
        value = explicit or saved.get(key) or default
        if explicit:
            value = Path(os.path.expandvars(str(explicit))).expanduser().resolve()
        resolved[key] = absolute(value, key) if value else None
    return resolved
