#!/usr/bin/env python3
"""Verify a browser original in a fresh, single-track September 12 capture run.

Example:
  python3 intake.py 20260912-205500-manual \
    --remote-id 2352435620 \
    --title 'Paolo Doldo x DJ HOTMAIL - Sonic Boom (FREE DOWNLOAD )' \
    --gate-url http://gaterush.me/5Zaimw \
    --source ~/Downloads/helium-downloads/PaoloDoldoxDJHOTMAIL-SonicBoom1.wav \
    --expected-bytes 69699596

The positional run is a read-only plan template. Every intake creates a new run;
the remote ID/title/gate must agree with that plan. The media name is retained as
evidence, never used to infer which library track it belongs to.
"""
import argparse
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from urllib.parse import urlsplit


LIB = (Path.home() / 'Music/downloaded/sc-likes-today-09-12').resolve()
SCRATCH = (Path.home() / 'dev/music-down/statefiles/freedl/upgrade-09-12').resolve()
JOB_ID = 'upgrade-09-12'
MEDIA = {'.wav', '.flac', '.aiff', '.aif', '.mp3', '.m4a', '.opus'}


def inside(path, root):
    return path == root or root in path.parents


def gate_key(url):
    parts = urlsplit(url)
    if parts.scheme not in {'http', 'https'} or not parts.hostname:
        raise ValueError(f'Invalid gate URL: {url}')
    return (parts.hostname.lower().removeprefix('www.'), parts.path.rstrip('/'),
            parts.query, parts.fragment)


def verify_row(plan, remote_id, title, gate_url):
    job = plan.get('job') or {}
    if job.get('id') != JOB_ID or Path(job.get('library_dir', '')).expanduser().resolve() != LIB:
        raise ValueError('Saved plan is not for the isolated September 12 library')
    for field, expected in [('buffer_dir', SCRATCH/'buffer'),
                            ('backup_dir', SCRATCH/'backups'), ('log_dir', SCRATCH/'logs')]:
        if Path(job.get(field, '')).expanduser().resolve() != expected:
            raise ValueError(f'Saved plan {field} is outside the isolated batch')
    rows = [r for r in plan.get('rows', []) if str(r.get('remote_id')) == remote_id]
    if len(rows) != 1:
        raise ValueError(f'Expected exactly one saved plan row for remote ID {remote_id}')
    row = rows[0]
    if row.get('title') != title:
        raise ValueError(f'Title disagrees with saved plan: {row.get("title")!r}')
    local = Path(row.get('local_path') or '').resolve()
    if local.parent != LIB or not local.is_file():
        raise ValueError(f'Planned track is absent or outside the 46-file batch: {local}')
    probe = row.get('free_dl_probe') or {}
    if not row.get('selectable') or probe.get('status') != 'available':
        raise ValueError('Saved plan row is not an available Free DL candidate')
    if gate_key(probe.get('purchase_url', '')) != gate_key(gate_url):
        raise ValueError('Gate URL disagrees with the saved plan')
    return row, local


def media_probe(path, decode=False):
    cmd = ['ffprobe', '-v', 'error', '-show_format', '-show_streams', '-of', 'json', str(path)]
    report = json.loads(subprocess.run(cmd, check=True, capture_output=True).stdout)
    audio = next((s for s in report['streams'] if s.get('codec_type') == 'audio'), None)
    if audio is None:
        raise ValueError(f'No audio stream in {path}')
    duration = float(audio.get('duration') or report['format'].get('duration') or 0)
    if duration <= 0:
        raise ValueError(f'No measurable duration in {path}')
    result = {'codec': audio['codec_name'], 'duration_seconds': duration,
              'sample_rate': int(audio['sample_rate']), 'channels': int(audio['channels']),
              'audio_bitrate': int(audio.get('bit_rate') or 0)}
    if decode:
        decoded = subprocess.run(['ffmpeg', '-v', 'error', '-xerror', '-i', str(path),
                                  '-map', '0:a:0', '-f', 'null', '-'], capture_output=True)
        if decoded.returncode or decoded.stderr:
            raise ValueError(f'Full audio decode failed: {decoded.stderr.decode(errors="replace")[:500]}')
        result['decode_complete'] = True
    return result


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def atomic_text(path, data):
    fd, name = tempfile.mkstemp(prefix='.intake-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def parse_state(path):
    lines = path.read_text().splitlines() if path.exists() else []
    entries = {}
    paths = {}
    for line in lines:
        fields = line.split(None, 2)
        if len(fields) == 3 and fields[0] == 'soundcloud':
            if fields[1] in entries and entries[fields[1]] != fields[2]:
                raise ValueError(f'Conflicting capture entries for {fields[1]}')
            if fields[2] in paths and paths[fields[2]] != fields[1]:
                raise ValueError(f'Download name already mapped to another track: {fields[2]}')
            entries[fields[1]] = fields[2]
            paths[fields[2]] = fields[1]
    return lines, entries, paths


def stage_copy(source, destination, expected_bytes, expected_sha256, local_duration):
    initial = source.stat()
    if expected_bytes is not None and initial.st_size != expected_bytes:
        raise ValueError(f'Download has {initial.st_size} bytes; expected {expected_bytes}')
    fd, temp_name = tempfile.mkstemp(prefix='.intake-', suffix=source.suffix, dir=destination.parent)
    temp = Path(temp_name)
    try:
        with source.open('rb') as src, os.fdopen(fd, 'wb') as dst:
            shutil.copyfileobj(src, dst, 1024 * 1024)
            dst.flush()
            os.fsync(dst.fileno())
        final = source.stat()
        if (initial.st_size, initial.st_mtime_ns, initial.st_ino) != (final.st_size, final.st_mtime_ns, final.st_ino):
            raise ValueError('Download changed while being copied')
        if temp.stat().st_size != initial.st_size:
            raise ValueError('Staged copy is incomplete')
        digest = sha256(temp)
        if expected_sha256 and digest.lower() != expected_sha256.lower():
            raise ValueError('Downloaded file hash differs from expected SHA-256')
        quality = media_probe(temp, decode=True)
        if abs(quality['duration_seconds'] - local_duration) > 2:
            raise ValueError('Original and planned local track differ by more than two seconds')
        if destination.exists():
            if destination.is_symlink() or not destination.is_file():
                raise ValueError(f'Staged path is not a regular file: {destination}')
            if sha256(destination) != digest:
                raise ValueError(f'Different file already staged at {destination}')
        else:
            os.link(temp, destination)  # Exclusive publish; never overwrite another original.
        return digest, initial.st_size, quality
    finally:
        temp.unlink(missing_ok=True)


def intake(args):
    if not re.fullmatch(r'[0-9A-Za-z][0-9A-Za-z-]*', args.template_run):
        raise ValueError('Invalid template run ID')
    if not re.fullmatch(r'\d+', args.remote_id):
        raise ValueError('Remote ID must be numeric')
    template_dir = (SCRATCH/'logs'/args.template_run).resolve()
    if template_dir.parent != SCRATCH/'logs' or not template_dir.is_dir():
        raise ValueError('Plan template is outside the isolated batch or does not exist')
    plan = json.loads((template_dir/'capture-plan.json').read_text())
    row, local = verify_row(plan, args.remote_id, args.title, args.gate_url)
    source = args.source.expanduser().resolve(strict=True)
    if not source.is_file() or source.suffix.lower() not in MEDIA or source.name.endswith('.crdownload'):
        raise ValueError('Source must be a completed supported audio file')
    if inside(source, LIB) or inside(source, SCRATCH):
        raise ValueError('Browser original must be outside the library and scratch buffer')
    if any(char.isspace() and char not in ' ' for char in source.name):
        raise ValueError('Download name contains a tab or newline, which capture state cannot encode')
    run_id = args.run_id or datetime.now().strftime('%Y%m%d-%H%M%S-%f')+'-'+args.remote_id
    if not re.fullmatch(r'[0-9A-Za-z][0-9A-Za-z-]*', run_id) or run_id == args.template_run:
        raise ValueError('New run ID is invalid or equals the plan template')
    run_dir = SCRATCH/'logs'/run_id
    downloads = SCRATCH/'buffer'/run_id/'downloads'
    if run_dir.exists() or downloads.parent.exists() or (SCRATCH/'backups'/run_id).exists():
        raise ValueError(f'Run {run_id} already exists; each intake needs a fresh run')
    run_dir.mkdir(parents=True)
    downloads.mkdir(parents=True)
    plan['run_id'] = run_id
    plan['created_at'] = datetime.now(timezone.utc).isoformat()
    plan['rows'] = [dict(candidate, selected=str(candidate.get('remote_id')) == args.remote_id)
                    for candidate in plan['rows']]
    for key, value in [('buffer_root', downloads.parent), ('log_dir', run_dir)]:
        if key in plan:
            plan[key] = str(value)
    atomic_text(run_dir/'capture-plan.json', json.dumps(plan, indent=2, ensure_ascii=False)+'\n')
    # The native capture-state reader tokenizes whitespace; a browser filename
    # with repeated spaces would no longer resolve to the staged file.
    staged_name = f'{args.remote_id}{source.suffix.lower()}'
    destination = downloads/staged_name
    state_path = run_dir/'capture.sync.scdl'
    evidence_path = run_dir/'manual-capture-evidence.json'
    with (run_dir/'.intake.lock').open('a+') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        lines, entries, paths = parse_state(state_path)
        if args.remote_id in entries and entries[args.remote_id] != staged_name:
            raise ValueError('Remote ID already captured under another download name')
        if staged_name in paths and paths[staged_name] != args.remote_id:
            raise ValueError('Download name already maps to another remote ID')
        evidence = json.loads(evidence_path.read_text()) if evidence_path.exists() else {
            'run_id': run_id, 'plan_template_run_id': args.template_run,
            'provenance': 'verified browser gate originals',
            'verified': True, 'target_scope': str(LIB), 'tracks': []}
        if evidence.get('run_id') != run_id or Path(evidence.get('target_scope', '')).resolve() != LIB:
            raise ValueError('Existing evidence belongs to another run or library')
        previous = [r for r in evidence.get('tracks', []) if str(r.get('remote_id')) == args.remote_id]
        if len(previous) > 1:
            raise ValueError('Duplicate evidence rows for this remote ID')
        local_quality = media_probe(local)
        digest, size, source_quality = stage_copy(source, destination, args.expected_bytes,
                                                  args.expected_sha256, local_quality['duration_seconds'])
        record = {
            'remote_id': args.remote_id, 'title': row['title'],
            'remote_url': row.get('remote_url'), 'gate_url': args.gate_url,
            'gate_final_url': args.final_url, 'source_file': str(source),
            'download_name': source.name, 'staged_name': staged_name,
            'staged_path': str(destination),
            'library_path': str(local), 'sha256': digest, 'size_bytes': size,
            'duration_seconds': source_quality['duration_seconds'],
            'local_duration_seconds': local_quality['duration_seconds'],
            'codec': source_quality['codec'], 'sample_rate': source_quality['sample_rate'],
            'channels': source_quality['channels'], 'audio_bitrate': source_quality['audio_bitrate'],
            'decode_complete': True, 'verified_at': datetime.now(timezone.utc).isoformat()}
        if previous:
            if previous[0].get('sha256') != digest or previous[0].get('staged_name') != staged_name:
                raise ValueError('Prior evidence for this track differs from the downloaded file')
        else:
            evidence['tracks'].append(record)
            atomic_text(evidence_path, json.dumps(evidence, indent=2, ensure_ascii=False)+'\n')
        if args.remote_id not in entries:
            lines.append(f'soundcloud {args.remote_id} {staged_name}')
            atomic_text(state_path, '\n'.join(lines)+'\n')
        return {'run_id': run_id, 'record': record}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('template_run', help='Saved run whose capture plan supplies identity and scope')
    parser.add_argument('--run-id', help='New unique run ID; defaults to timestamp plus remote ID')
    parser.add_argument('--remote-id', required=True)
    parser.add_argument('--title', required=True, help='Exact title from saved capture plan')
    parser.add_argument('--gate-url', required=True, help='Gate URL from saved capture plan')
    parser.add_argument('--final-url', help='Verified browser download endpoint, if available')
    parser.add_argument('--source', required=True, type=Path, help='Completed browser download')
    parser.add_argument('--expected-bytes', type=int)
    parser.add_argument('--expected-sha256')
    args = parser.parse_args()
    try:
        print(json.dumps(intake(args), indent=2, ensure_ascii=False))
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError, StopIteration) as exc:
        parser.exit(1, f'intake: {exc}\n')


if __name__ == '__main__':
    main()
