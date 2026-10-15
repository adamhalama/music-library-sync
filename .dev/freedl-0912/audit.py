#!/usr/bin/env python3
"""Read-only evidence: audio-stream rates, full decode, tags/art and earliest backups."""
import argparse, concurrent.futures, hashlib, json, subprocess
from datetime import datetime, timezone
from pathlib import Path

from context import add_context, resolve_context, scoped
MEDIA = {'.m4a', '.mp3', '.wav', '.flac', '.aiff', '.aif', '.opus'}
IGNORED_TAGS = {'major_brand', 'minor_version', 'compatible_brands', 'encoder'}

def run(args):
    return subprocess.run(args, check=True, capture_output=True)

def probe(path, decode=False):
    data = json.loads(run(['ffprobe', '-v', 'error', '-show_format', '-show_streams', '-of', 'json', str(path)]).stdout)
    audio = next(s for s in data['streams'] if s['codec_type'] == 'audio')
    art = [s for s in data['streams'] if s.get('disposition', {}).get('attached_pic')]
    result = {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
              'birthtime': getattr(path.stat(), 'st_birthtime', None), 'codec': audio['codec_name'], 'audio_kbps': round(int(audio.get('bit_rate', 0))/1000, 1),
              'sample_rate': int(audio['sample_rate']), 'channels': audio['channels'],
              'duration': float(audio.get('duration', data['format']['duration'])),
              'tags': {k:v for k,v in data['format'].get('tags', {}).items() if k not in IGNORED_TAGS},
              'artwork': []}
    for s in art:
        payload = run(['ffmpeg', '-v', 'error', '-i', str(path), '-map', '0:'+str(s['index']), '-c', 'copy', '-f', 'image2pipe', '-']).stdout
        result['artwork'].append(hashlib.sha256(payload).hexdigest())
    if decode:
        p = subprocess.run(['ffmpeg', '-v', 'error', '-i', str(path), '-map', '0:a:0', '-f', 'null', '-'], capture_output=True)
        result['decode_ok'] = p.returncode == 0 and not p.stderr.strip()
        result['decode_errors'] = p.stderr.decode(errors='replace')
    return result

def stream_hash(path):
    return run(['ffmpeg', '-v', 'error', '-i', str(path), '-map', '0:a:0', '-c', 'copy', '-f', 'hash', '-hash', 'sha256', '-']).stdout.decode().strip()

def track_key(path, ctx=None):
    if ctx:
        return str(scoped(path, ctx.library).relative_to(ctx.library))
    return Path(path).stem


def ledger_files(root, name):
    return sorted((scoped(p, root) for p in root.rglob(name)), key=lambda p: (p.stat().st_mtime_ns, str(p)))


def promotion_ledger(scratch=None, ctx=None):
    promoted = {}
    logs = ctx.logs if ctx else scratch/'logs'
    for log in ledger_files(logs, 'promotion-result.json'):
        for row in json.loads(log.read_text()).get('rows') or []:
            if row.get('status') == 'replaced':
                promoted[track_key(row['library_path'], ctx)] = str(log)
    for log in ledger_files(logs, 'preservation-result.json'):
        record = json.loads(log.read_text())
        if record.get('action') == 'install-mp3':
            promoted[track_key(record['after']['path'], ctx)] = str(log)
    return promoted

def baseline_backups(scratch=None, ctx=None):
    """Resolve original audio from explicit mutation records, never folder names.

    Capture run IDs use local time while metadata repairs use UTC, so lexical
    backup ordering can select a later, already-upgraded metadata-repair copy.
    """
    candidates = []
    logs = ctx.logs if ctx else scratch/'logs'
    backup_root = ctx.backups if ctx else scratch/'backups'
    for log in ledger_files(logs, 'promotion-result.json'):
        for row in json.loads(log.read_text()).get('rows') or []:
            if row.get('status') == 'replaced' and row.get('backup_path'):
                candidates.append((log.stat().st_mtime_ns, str(log), track_key(row['library_path'], ctx),
                                   Path(row['backup_path']), None))
    for log in ledger_files(logs, 'preservation-result.json'):
        record = json.loads(log.read_text())
        if record.get('action') == 'install-mp3' and record.get('backup'):
            candidates.append((log.stat().st_mtime_ns, str(log), track_key(record['after']['path'], ctx),
                               Path(record['backup']), track_key(record['before']['path'], ctx) if ctx else None))
    backups = {}
    for _, _, stem, backup, previous in sorted(candidates):
        # A recorded but missing original must fail visibly; substituting a
        # metadata-repair copy would silently turn an upgrade into "unchanged".
        if stem not in backups:
            if not backup.is_file():
                raise FileNotFoundError(f'Promotion baseline missing: {backup}')
            if not backup.resolve().is_relative_to(backup_root.resolve()):
                raise ValueError(f'Promotion baseline outside scratch backups: {backup}')
            backups[stem] = backups.get(previous, backup)
    return backups

def classify_change(before, current, promotion_evidence):
    changed_audio = before['encoded_audio_sha256'] != current['encoded_audio_sha256']
    return {'changed_audio': changed_audio,
            'upgraded': bool(promotion_evidence) and changed_audio,
            'promotion_evidence': promotion_evidence,
            'birthtime_preserved': (before['birthtime'] is None or current['birthtime'] is None or
                                   abs(before['birthtime']-current['birthtime']) < 0.000001)}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_context(parser, require_plan=True)
    parser.add_argument('--output', type=Path, help='Report prefix (default: job log_dir/quality-audit)')
    args = parser.parse_args()
    ctx = resolve_context(args)
    args.output = args.output or ctx.logs/'quality-audit'
    paths = sorted(scoped(p, ctx.library) for p in ctx.library.rglob('*') if p.is_file() and p.suffix.lower() in MEDIA)
    backups = baseline_backups(ctx=ctx)
    plans = ctx.plans()
    definitions = {}
    for p in plans:
        for r in p.get('rows') or []:
            if r.get('local_path'):
                definitions[track_key(r['local_path'], ctx)] = r
    for log in ledger_files(ctx.logs, 'preservation-result.json'):
        migration = json.loads(log.read_text())
        if migration.get('action') == 'install-mp3':
            old = track_key(migration['before']['path'], ctx)
            new = track_key(migration['after']['path'], ctx)
            if old in definitions and new not in definitions:
                definitions[new] = definitions[old]
    promoted = promotion_ledger(ctx=ctx)
    def inspect(path):
        key = track_key(path, ctx)
        current = probe(path, True)
        before = probe(backups[key]) if key in backups else current
        current['encoded_audio_sha256'] = stream_hash(path)
        before['encoded_audio_sha256'] = stream_hash(backups[key]) if key in backups else current['encoded_audio_sha256']
        change = classify_change(before, current, promoted.get(key))
        row = definitions.get(key, {})
        gate = row.get('free_dl_probe', {})
        return {'track': key, 'before': before, 'after': current, **change,
                'path_preserved': Path(before['path']).name == path.name,
                'tags_preserved': all(current['tags'].get(k) == v for k,v in before['tags'].items()),
                'artwork_preserved': current['artwork'] == before['artwork'],
                'gate': gate, 'remote_id': row.get('remote_id')}
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        rows = list(pool.map(inspect, paths))
    output = {'generated_at': datetime.now(timezone.utc).isoformat(), 'library': str(ctx.library), 'job_id': ctx.job_id,
              'note': 'Bitrates are audio-stream measurements; higher rate alone cannot prove perceptual quality. Before uses the original backup referenced by the earliest successful promotion/migration record (ordered by ledger file time), otherwise current file (no independent baseline). Upgrades require both promotion evidence and changed encoded audio.',
              'count': len(rows), 'upgraded': sum(r['upgraded'] for r in rows), 'rows': rows}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.with_suffix('.json').write_text(json.dumps(output, indent=2)+'\n')
    lines = ['# Free DL quality audit', '', output['note'], '',
             '| Track | Before → now (audio kbps) | Sample rate | Decode | Tags / art | Path | Created |',
             '|---|---|---:|---|---|---|---|']
    for r in rows:
        a,b = r['before'],r['after']
        quality = f"{a['codec']} {a['audio_kbps']} → {b['codec']} {b['audio_kbps']}" if r['changed_audio'] else f"{b['codec']} {b['audio_kbps']} (unchanged)"
        lines.append('| '+ ' | '.join([r['track'].replace('|','\\|'), quality, str(b['sample_rate']), 'Pass' if b['decode_ok'] else 'FAIL', ('Pass' if r['tags_preserved'] else 'CHANGED')+' / '+('Pass' if r['artwork_preserved'] else 'CHANGED'), 'Preserved' if r['path_preserved'] else 'Extension changed', 'Preserved' if r['birthtime_preserved'] else 'CHANGED'])+' |')
    args.output.with_suffix('.md').write_text('\n'.join(lines)+'\n')
    print(json.dumps({k:v for k,v in output.items() if k != 'rows'}, indent=2))
    if any(not r['after']['decode_ok'] or not r['tags_preserved'] or not r['artwork_preserved'] or not r['birthtime_preserved'] for r in rows):
        raise RuntimeError('Quality audit failed; inspect the JSON/Markdown reports')

if __name__ == '__main__':
    main()
