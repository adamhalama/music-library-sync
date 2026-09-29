#!/usr/bin/env python3
"""Repair batch metadata or install an original MP3 without lossy transcoding.

Mutagen is a local maintenance dependency, not a udl runtime dependency.
All writes are scoped to the September 12 isolated batch. Encoded audio and
artwork hashes plus full decode are checked before atomic replacement.
"""
import argparse, ctypes, json, os, shutil, sys, tempfile
from datetime import datetime, timezone
from pathlib import Path
from audit import LIB, SCRATCH, probe, run, stream_hash


def scoped(path, root):
    path = path.resolve()
    if not path.is_relative_to(root.resolve()):
        raise ValueError(f'Outside isolated batch: {path}')
    return path


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
        sys.path.insert(0, str(SCRATCH/'pythonlib'))
        from mutagen.mp4 import MP4
        original, output = MP4(source_tags), MP4(destination)
        output.tags = original.tags
        output.save()
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

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    restore = sub.add_parser('restore-tags')
    restore.add_argument('backup', type=Path)
    restore.add_argument('target', type=Path)
    install = sub.add_parser('install-mp3')
    install.add_argument('source', type=Path)
    install.add_argument('target', type=Path, help='Existing AAC file; becomes .mp3 explicitly')
    args = parser.parse_args()
    target = scoped(args.target, LIB)
    birthtime = target.stat().st_birthtime if sys.platform == "darwin" else None
    before = probe(target)
    stamp = datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S-%f')
    evidence = SCRATCH / 'logs' / (stamp+'-'+args.action)
    evidence.mkdir(parents=True)
    fd, name = tempfile.mkstemp(prefix='.quality-', suffix='.mp3' if args.action == 'install-mp3' else target.suffix, dir=LIB)
    os.close(fd)
    stage = Path(name)
    try:
        if args.action == 'restore-tags':
            tags = scoped(args.backup, SCRATCH/'backups')
            if sys.platform == 'darwin':
                birthtime = tags.stat().st_birthtime
            shutil.copy2(target, stage)
            expected_audio = stream_hash(target)
            output = target
        else:
            source = scoped(args.source, SCRATCH)
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
        backup_dir = SCRATCH/'backups'/(stamp+'-'+args.action)
        backup_dir.mkdir(parents=True)
        backup = backup_dir/target.name
        shutil.copy2(target, backup)
        if probe(backup)['sha256'] != before['sha256']:
            raise ValueError('Backup hash mismatch')
        if birthtime is not None:
            restore_birthtime(stage, birthtime)
            restore_birthtime(backup, birthtime)
        result = {'action':args.action,'before':before,'after':after,'backup':str(backup),
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
