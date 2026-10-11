"""Synthetic media acceptance: metadata, native promotion, audit and MP3 migration.

Requires ffmpeg/ffprobe + Mutagen; native leg uses UDL_FREEDL_TEST_BINARY.
All files/configs are generated inside TemporaryDirectory; no network/browser.
"""
import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

from context import REPO, Context
from audit import probe, stream_hash
from test_context import fixture

try:
    from mutagen.mp4 import MP4, MP4Cover, MP4FreeForm
except ImportError:
    MP4 = None


@unittest.skipUnless(MP4 and shutil.which('ffmpeg') and shutil.which('ffprobe'), 'requires Mutagen and ffmpeg/ffprobe')
class MediaTests(unittest.TestCase):
    def invoke(self, *args):
        return subprocess.run([sys.executable, str(REPO/'.dev/freedl.py'), *map(str, args)],
                              check=True, capture_output=True, text=True, timeout=45)

    def audio(self, path, codec, frequency=440):
        subprocess.run(['ffmpeg', '-v', 'error', '-y', '-f', 'lavfi', '-i',
                        f'sine=frequency={frequency}:duration=3', '-c:a', codec,
                        '-metadata', 'title=Exact track', str(path)], check=True, capture_output=True)

    def metadata(self, path):
        art = path.parent/'cover.jpg'
        subprocess.run(['ffmpeg', '-v', 'error', '-y', '-f', 'lavfi', '-i', 'color=red:s=32x32',
                        '-frames:v', '1', '-update', '1', str(art)], check=True, capture_output=True)
        tags = MP4(path)
        tags['----:com.apple.iTunes:WWWAUDIOFILE'] = [MP4FreeForm(b'https://soundcloud.com/example/track')]
        tags['purl'] = ['https://soundcloud.com/example/track']
        tags['covr'] = [MP4Cover(art.read_bytes(), imageformat=MP4Cover.FORMAT_JPEG)]
        tags.save()
        return copy.deepcopy(tags.tags)

    def test_restore_preview_then_complete_mp4_tags_and_exact_audio(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            target = Path(ctx.plan['rows'][0]['local_path']).resolve()
            self.audio(target, 'aac')
            expected = self.metadata(target)
            original = probe(target)
            backup = ctx.backups/ctx.plan['run_id']/target.name
            backup.parent.mkdir(parents=True)
            shutil.copy2(target, backup)
            if sys.platform == 'darwin':
                from preserve import restore_birthtime
                restore_birthtime(backup, original['birthtime'])
            # Simulate upgraded audio that dropped unknown metadata/art.
            self.audio(target, 'aac', frequency=880)
            upgraded_hash = stream_hash(target)
            (ctx.plan_path.parent/'promotion-result.json').write_text(json.dumps({'rows': [
                {'status': 'replaced', 'library_path': str(target), 'backup_path': str(backup)}]}))
            unchanged = target.read_bytes()
            preview = self.invoke('preserve', '--plan', ctx.plan_path, 'restore-tags', backup, target)
            self.assertFalse(json.loads(preview.stdout)['apply'])
            self.assertEqual(target.read_bytes(), unchanged)
            self.invoke('preserve', '--plan', ctx.plan_path, 'restore-tags', backup, target, '--apply')
            self.assertEqual(MP4(target).tags, expected)
            self.assertEqual(stream_hash(target), upgraded_hash)
            self.assertTrue(probe(target, True)['decode_ok'])
            if sys.platform == 'darwin':
                self.assertAlmostEqual(target.stat().st_birthtime, original['birthtime'], places=6)
            audit = self.invoke('audit', '--plan', ctx.plan_path)
            self.assertEqual(json.loads(audit.stdout)['upgraded'], 1)

    def test_mp3_migration_exact_capture_and_status_identity(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            target = Path(ctx.plan['rows'][0]['local_path']).resolve()
            self.audio(target, 'aac')
            # MP4 unknown freeforms cannot be copied to ID3 exactly; standard tags
            # are the supported migration subset and validation refuses losses.
            source = ctx.buffer/ctx.plan['run_id']/'downloads/123.mp3'
            source.parent.mkdir(parents=True)
            self.audio(source, 'libmp3lame', frequency=880)
            (ctx.plan_path.parent/'capture.sync.scdl').write_text('soundcloud 123 123.mp3\n')
            expected_audio = stream_hash(source)
            self.invoke('preserve', '--plan', ctx.plan_path, 'install-mp3', source, target, '--remote-id', '123', '--apply')
            output = target.with_suffix('.mp3')
            self.assertFalse(target.exists())
            self.assertEqual(stream_hash(output), expected_audio)
            result = json.loads(self.invoke('status', '--plan', ctx.plan_path).stdout)
            self.assertEqual(result['counts']['upgraded'], 1)
            self.assertEqual(result['upgraded'][0]['remote_id'], '123')
            audit = json.loads(self.invoke('audit', '--plan', ctx.plan_path).stdout)
            self.assertEqual(audit['upgraded'], 1)
            full = json.loads((ctx.logs/'quality-audit.json').read_text())
            self.assertEqual(full['rows'][0]['remote_id'], '123')

    @unittest.skipUnless(os.environ.get('UDL_FREEDL_TEST_BINARY'), 'set UDL_FREEDL_TEST_BINARY to current backend')
    def test_native_promotion_for_two_jobs_with_connection_context(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d).resolve()
            contexts = [fixture(root, 'job-one'), fixture(root, 'job-two')]
            main = root/'main.yaml'
            main.write_text('version: 1\ndefaults:\n  state_dir: '+str(root/'state')+'\nsources: []\n')
            freedl = root/'freedl.yaml'
            jobs = []
            for ctx in contexts:
                jobs.append(dict(ctx.job, enabled=True, plan_limit=10, target_format='auto', download_order='oldest_first', min_match_score=72, ambiguity_gap=8))
            # JSON is valid YAML; production Go loader supplies defaults.
            freedl.write_text(json.dumps({'version': 1, 'jobs': jobs}))
            for ctx in contexts:
                target = Path(ctx.plan['rows'][0]['local_path']).resolve()
                self.audio(target, 'aac')
                expected_tags = self.metadata(target)
                source = ctx.buffer/ctx.plan['run_id']/'downloads/123.flac'
                source.parent.mkdir(parents=True)
                self.audio(source, 'flac', frequency=880)
                (ctx.plan_path.parent/'capture.sync.scdl').write_text('soundcloud 123 123.flac\n')
                (ctx.plan_path.parent/'maintenance-context.json').write_text(json.dumps({
                    'job_id': ctx.job_id, 'config': str(main), 'freedl_config': str(freedl),
                    'working_dir': str(root), 'udl': os.environ['UDL_FREEDL_TEST_BINARY']}))
                before = target.read_bytes()
                preview = json.loads(self.invoke('promote', '--plan', ctx.plan_path).stdout)
                self.assertEqual(preview['job']['id'], ctx.job_id)
                self.assertEqual(sum(bool(r['selected']) for r in preview['rows']), 1)
                self.assertEqual(target.read_bytes(), before)
                self.invoke('promote', '--plan', ctx.plan_path, '--apply')
                self.assertEqual(MP4(target).tags, expected_tags)
                self.assertTrue(probe(target, True)['decode_ok'])
                result = json.loads(self.invoke('audit', '--plan', ctx.plan_path).stdout)
                self.assertEqual(result['upgraded'], 1)
                self.assertEqual(result['job_id'], ctx.job_id)


if __name__ == '__main__':
    unittest.main()
