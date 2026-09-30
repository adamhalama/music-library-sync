#!/usr/bin/env python3
"""Read-only evidence: audio-stream rates, full decode, tags/art and earliest backups."""
import argparse, concurrent.futures, hashlib, json, subprocess
from datetime import datetime, timezone
from pathlib import Path

LIB = Path.home() / 'Music/downloaded/sc-likes-today-09-12'
SCRATCH = Path.home() / 'dev/music-down/statefiles/freedl/upgrade-09-12'
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

def promotion_ledger(scratch):
    promoted = {}
    for log in sorted((scratch/'logs').rglob('promotion-result.json')):
        for row in json.loads(log.read_text()).get('rows', []):
            if row.get('status') == 'replaced':
                promoted[Path(row['library_path']).stem] = str(log)
    for log in sorted((scratch/'logs').rglob('preservation-result.json')):
        record = json.loads(log.read_text())
        if record.get('action') == 'install-mp3':
            promoted[Path(record['after']['path']).stem] = str(log)
    return promoted

def baseline_backups(scratch):
    """Resolve original audio from explicit mutation records, never folder names.

    Capture run IDs use local time while metadata repairs use UTC, so lexical
    backup ordering can select a later, already-upgraded metadata-repair copy.
    """
    candidates = []
    for log in (scratch/'logs').rglob('promotion-result.json'):
        for row in json.loads(log.read_text()).get('rows') or []:
            if row.get('status') == 'replaced' and row.get('backup_path'):
                candidates.append((log.stat().st_mtime_ns, Path(row['library_path']).stem,
                                   Path(row['backup_path'])))
    for log in (scratch/'logs').rglob('preservation-result.json'):
        record = json.loads(log.read_text())
        if record.get('action') == 'install-mp3' and record.get('backup'):
            candidates.append((log.stat().st_mtime_ns, Path(record['after']['path']).stem,
                               Path(record['backup'])))
    backups = {}
    for _, stem, backup in sorted(candidates):
        # A recorded but missing original must fail visibly; substituting a
        # metadata-repair copy would silently turn an upgrade into "unchanged".
        if stem not in backups:
            if not backup.is_file():
                raise FileNotFoundError(f'Promotion baseline missing: {backup}')
            if not backup.resolve().is_relative_to((scratch/'backups').resolve()):
                raise ValueError(f'Promotion baseline outside scratch backups: {backup}')
            backups[stem] = backup
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
    parser.add_argument('--output', type=Path, default=SCRATCH / 'quality-audit')
    args = parser.parse_args()
    paths = sorted(p for p in LIB.iterdir() if p.suffix.lower() in MEDIA)
    backups = baseline_backups(SCRATCH)
    plans = sorted((SCRATCH / 'logs').glob('*/capture-plan.json'))
    definitions = {}
    for p in plans:
        for r in json.loads(p.read_text()).get('rows', []):
            if r.get('local_path'):
                definitions[Path(r['local_path']).stem] = r
    promoted = promotion_ledger(SCRATCH)
    def inspect(path):
        current = probe(path, True)
        before = probe(backups[path.stem]) if path.stem in backups else current
        current['encoded_audio_sha256'] = stream_hash(path)
        before['encoded_audio_sha256'] = stream_hash(backups[path.stem]) if path.stem in backups else current['encoded_audio_sha256']
        change = classify_change(before, current, promoted.get(path.stem))
        row = definitions.get(path.stem, {})
        gate = row.get('free_dl_probe', {})
        return {'track': path.stem, 'before': before, 'after': current, **change,
                'path_preserved': Path(before['path']).name == path.name,
                'tags_preserved': all(current['tags'].get(k) == v for k,v in before['tags'].items()),
                'artwork_preserved': current['artwork'] == before['artwork'],
                'gate': gate, 'remote_id': row.get('remote_id')}
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        rows = list(pool.map(inspect, paths))
    output = {'generated_at': datetime.now(timezone.utc).isoformat(), 'library': str(LIB),
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

if __name__ == '__main__':
    main()
