#!/usr/bin/env python3
"""Report job-wide upgrades and pending captures from all saved plans and ledgers."""
import argparse
import json
from audit import ledger_files, promotion_ledger, track_key
from context import add_context, resolve_context, scoped
from intake import parse_state


def report(ctx):
    migrations = {}
    for log in ledger_files(ctx.logs, 'preservation-result.json'):
        record = json.loads(log.read_text())
        if record.get('action') == 'install-mp3':
            old = scoped(record['before']['path'], ctx.library)
            new = scoped(record['after']['path'], ctx.library)
            if old in migrations and migrations[old] != new:
                raise ValueError(f'Conflicting migration identity for {old}')
            migrations[old] = new
    def current_path(value):
        path = scoped(value, ctx.library)
        seen = set()
        while path in migrations:
            if path in seen:
                raise ValueError('Cyclic migration identity in preservation evidence')
            seen.add(path)
            path = migrations[path]
        return str(path)
    definitions = {}
    for plan in ctx.plans():
        for saved in plan.get('rows') or []:
            row = dict(saved)
            remote = row.get('remote_id')
            if row.get('local_path'):
                row['local_path'] = current_path(row['local_path'])
            previous = definitions.get(remote)
            if previous and previous.get('local_path'):
                if row.get('local_path') and previous['local_path'] != row['local_path']:
                    raise ValueError(f'Conflicting local identity for remote ID {remote}')
                if not row.get('local_path'):
                    row['local_path'] = previous['local_path']
            definitions[remote] = row
    captured = set()
    for path in ledger_files(ctx.logs, 'capture.sync.scdl'):
        _, entries, _ = parse_state(path)
        for remote, name in entries.items():
            staged = scoped(ctx.buffer/path.parent.name/'downloads'/name, ctx.buffer)
            if staged.is_file():
                captured.add(remote)
    promoted = promotion_ledger(ctx=ctx)
    groups = {'upgraded': [], 'pending': [], 'unavailable': []}
    for remote, row in definitions.items():
        local = row.get('local_path')
        if not local or not scoped(local, ctx.library).is_file():
            continue
        if track_key(local, ctx) in promoted:
            group = 'upgraded'
        elif (row.get('free_dl_probe') or {}).get('status') == 'available':
            group = 'pending'
        else:
            group = 'unavailable'
        row['captured'] = remote in captured
        groups[group].append(row)
    return {'job_id': ctx.job_id, 'library': str(ctx.library),
            'counts': {k: len(v) for k, v in groups.items()}, **groups}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_context(parser, require_plan=True)
    args = parser.parse_args()
    print(json.dumps(report(resolve_context(args)), indent=2))


if __name__ == '__main__':
    main()
