#!/usr/bin/env python3
"""Repair batch metadata or install an original MP3 without lossy transcoding.

Mutagen is a local maintenance dependency, not a udl runtime dependency.
All writes are scoped to the saved Free DL job. Encoded audio and
artwork hashes plus full decode are checked before atomic replacement.
"""
import argparse, ctypes, json, os, shutil, sys, tempfile
from datetime import datetime, timezone
from pathlib import Path
from audit import probe, run, stream_hash
from context import add_context, resolve_context, scoped


def restore_birthtime(path, timestamp):
    if sys.platform != 'darwin':
        return
    class AttrList(ctypes.Structure):
        _fields_ = [('bitmapcount', ctypes.c_uint16), ('reserved', ctypes.c_uint16),
                    ('commonattr', ctypes.c_uint32), ('volattr', ctypes.c_uint32),
                    ('dirattr', ctypes.c_uint32), ('fileattr', ctypes.c_uint32), ('forkattr', ctypes.c_uint32)]
    class Timespec(ctypes.Structure):
        _fields_ = [('seconds', ctypes.c_long), ('nanoseconds', ctypes.c_long)]
    attributes = AttrList(5, 0, 0x200, 0, 0, 0, 0)
    value = Timespec(int(timestamp), int((timestamp-int(timestamp))*1e9))
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.setattrlist(os.fsencode(path), ctypes.byref(attributes), ctypes.byref(value), ctypes.sizeof(value), 0) != 0:
        raise OSError(ctypes.get_errno(), 'setattrlist failed', str(path))

def preserve(source_tags, destination):
    if source_tags.suffix.lower() == destination.suffix.lower() == '.m4a':
        # ffmpeg and ExifTool silently drop unknown iTunes freeform fields.
        # Mutagen preserves the full MP4 ilst including WWWAUDIOFILE and purl.
        from mutagen.mp4 import MP4
        original, output = MP4(source_tags), MP4(destination)
        output.tags = original.tags
        output.save()
        if MP4(destination).tags != original.tags:
            raise ValueError('Complete MP4 metadata verification failed')
        return
    # ffmpeg already maps standard tags and arbitrary TXXX fields into ID3.
    # Validation below refuses replacement if any source field is missing.


def write_evidence(path, result):
    pending = path.with_suffix('.pending')
    try:
        with pending.open('w') as handle:
            json.dump(result, handle, indent=2)
            handle.write('\n')
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(pending, path)
    finally:
        pending.unlink(missing_ok=True)

def install_verified(stage, target, output, backup, birthtime, evidence, result):
    # A durable prepared record precedes any library mutation. In-process errors
    # restore the verified backup; a hard crash leaves this recovery record.
    transaction = evidence/'transaction.json'
    write_evidence(transaction, {'status':'prepared', 'target':str(target),
                                'output':str(output), 'backup':str(backup)})
    rollback_birthtime = getattr(target.stat(), 'st_birthtime', None)
    installed = False
    try:
        os.replace(stage, output)
        installed = True
        if output != target:
            target.unlink()
        result['after']['path'] = str(output)
        result['after']['birthtime'] = getattr(output.stat(), 'st_birthtime', None)
        result['birthtime_preserved'] = birthtime is None or abs(result['after']['birthtime']-birthtime) < 0.000001
        if not result['birthtime_preserved']:
            raise ValueError('Creation time verification failed')
        write_evidence(evidence/'preservation-result.json', result)
    except BaseException:
        if installed:
            fd, name = tempfile.mkstemp(prefix='.rollback-', suffix=target.suffix, dir=target.parent)
            os.close(fd)
            rollback = Path(name)
            try:
                shutil.copy2(backup, rollback)
                if rollback_birthtime is not None:
                    restore_birthtime(rollback, rollback_birthtime)
                os.replace(rollback, target)
                if output != target:
                    output.unlink(missing_ok=True)
            finally:
                rollback.unlink(missing_ok=True)
        raise
    # Keep prepared record: preservation-result.json is the commit marker.

def verify_identity(ctx, args, target):
    if args.action == 'restore-tags':
        backup = scoped(args.backup, ctx.backups)
        pairs = [(r.get('library_path'), r.get('backup_path')) for r in ctx.plan.get('rows') or []
                 if r.get('selected') and r.get('backup_path')]
        ledger = ctx.logs/ctx.plan['run_id']/'promotion-result.json'
        if ledger.exists():
            pairs += [(r.get('library_path'), r.get('backup_path')) for r in json.loads(ledger.read_text()).get('rows') or []
                      if r.get('status') == 'replaced']
        if not any(scoped(local, ctx.library) == target and scoped(original, ctx.backups) == backup
                   for local, original in pairs if local and original):
            raise ValueError('Backup/target pair is absent from this run promotion plan or successful result')
        if not backup.is_file():
            raise FileNotFoundError(backup)
    else:
        from intake import verify_row, parse_state, sha256
        row, local = verify_row(ctx.plan, args.remote_id)
        if local != target or target.suffix.lower() != '.m4a':
            raise ValueError('MP3 migration target must be the exact planned AAC track')
        source = scoped(args.source, ctx.buffer/ctx.plan['run_id']/'downloads')
        _, entries, _ = parse_state(ctx.logs/ctx.plan['run_id']/'capture.sync.scdl')
        name = entries.get(args.remote_id)
        if not name or scoped(source.parent/name, source.parent) != source:
            raise ValueError('Source does not match the exact remote ID in native capture state')
        evidence = ctx.logs/ctx.plan['run_id']/'manual-capture-evidence.json'
        if evidence.exists():
            records = [r for r in json.loads(evidence.read_text()).get('tracks', []) if r.get('remote_id') == args.remote_id]
            if len(records) != 1 or records[0].get('sha256') != sha256(source):
                raise ValueError('Captured original differs from its provenance evidence')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_context(parser, require_plan=True)
    sub = parser.add_subparsers(dest='action', required=True)
    restore = sub.add_parser('restore-tags')
    restore.add_argument('backup', type=Path)
    restore.add_argument('target', type=Path)
    restore.add_argument('--apply', action='store_true', help='Install verified metadata restoration')
    install = sub.add_parser('install-mp3')
    install.add_argument('source', type=Path)
    install.add_argument('target', type=Path, help='Existing AAC file; becomes .mp3 explicitly')
    install.add_argument('--remote-id', required=True, help='Exact captured remote ID from the saved plan/state')
    install.add_argument('--apply', action='store_true', help='Install verified MP3 and remove old AAC')
    args = parser.parse_args()
    ctx = resolve_context(args)
    target = scoped(args.target, ctx.library)
    verify_identity(ctx, args, target)
    if not args.apply:
        print(json.dumps({'action': args.action, 'apply': False, 'job_id': ctx.job_id,
                          'target': str(target), 'source': str(args.backup if args.action == 'restore-tags' else args.source)}, indent=2))
        return
    birthtime = target.stat().st_birthtime if sys.platform == "darwin" else None
    before = probe(target)
    stamp = datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S-%f')
    evidence = ctx.logs / (stamp+'-'+args.action)
    evidence.mkdir(parents=True)
    fd, name = tempfile.mkstemp(prefix='.quality-', suffix='.mp3' if args.action == 'install-mp3' else target.suffix, dir=target.parent)
    os.close(fd)
    stage = Path(name)
    try:
        if args.action == 'restore-tags':
            tags = scoped(args.backup, ctx.backups)
            if sys.platform == 'darwin':
                birthtime = tags.stat().st_birthtime
            shutil.copy2(target, stage)
            expected_audio = stream_hash(target)
            output = target
        else:
            source = scoped(args.source, ctx.buffer/ctx.plan['run_id']/'downloads')
            source_probe = probe(source, True)
            if source_probe['codec'] != 'mp3' or not source_probe['decode_ok']:
                raise ValueError('Source must be a decodable original MP3')
            if abs(source_probe['duration']-before['duration']) > 2:
                raise ValueError('Duration mismatch requires track-specific review')
            tags = target
            output = target.with_suffix('.mp3')
            if output.exists():
                raise FileExistsError(output)
            # Preserve exact compressed audio; ffmpeg maps the AAC library artwork/tags.
            run(['ffmpeg','-v','error','-y','-i',str(source),'-i',str(target),'-map','0:a:0','-map','1:v?',
                 '-map_metadata','1','-c','copy',str(stage)])
            expected_audio = stream_hash(source)
        preserve(tags, stage)
        after = probe(stage, True)
        expected_tags = probe(tags)
        missing = {k:v for k,v in expected_tags['tags'].items() if after['tags'].get(k) != v}
        if missing:
            raise ValueError(f'Metadata differs: {missing}')
        if stream_hash(stage) != expected_audio or after['artwork'] != expected_tags['artwork'] or not after['decode_ok']:
            raise ValueError('Audio/artwork/decode verification failed')
        backup_dir = ctx.backups/(stamp+'-'+args.action)
        backup_dir.mkdir(parents=True)
        backup = backup_dir/target.name
        shutil.copy2(target, backup)
        if probe(backup)['sha256'] != before['sha256']:
            raise ValueError('Backup hash mismatch')
        if birthtime is not None:
            restore_birthtime(stage, birthtime)
            restore_birthtime(backup, birthtime)
        result = {'job_id':ctx.job_id,'capture_run_id':ctx.plan['run_id'],'action':args.action,'before':before,'after':after,'backup':str(backup),
                  'encoded_audio_sha256':expected_audio,'tags_verified':True,'artwork_verified':True}
        if args.action == 'install-mp3':
            result['source'] = source_probe
            result['note'] = 'Artist-provided VBR MP3 copied without re-encoding. Extension explicitly changed from .m4a to .mp3.'
        install_verified(stage, target, output, backup, birthtime, evidence, result)
        print(json.dumps(result,indent=2))
    finally:
        stage.unlink(missing_ok=True)

if __name__ == '__main__':
    main()
