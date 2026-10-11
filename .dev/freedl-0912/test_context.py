"""Isolated reusable-job and provenance tests; no user state/browser interaction."""
import argparse
import copy
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from context import Context, REPO, add_context, read_connection, resolve_context
import intake
import preserve
import status
from audit import baseline_backups, track_key


def fixture(root, job_id='job-a', run='capture-1'):
    # Independent volumes/parents, nested library files, no conventional scratch.
    job = {'id': job_id, 'source_url': 'https://soundcloud.com/example/likes',
           'state_file': job_id+'.scdl', 'library_dir': str(root/'music'/job_id),
           'buffer_dir': str(root/'downloads'/job_id), 'backup_dir': str(root/'originals'/job_id),
           'log_dir': str(root/'evidence'/job_id)}
    local = Path(job['library_dir'])/'nested'/'Track.m4a'
    local.parent.mkdir(parents=True, exist_ok=True)
    local.write_bytes(b'original audio')
    plan = {'run_id': run, 'job': job, 'buffer_root': str(Path(job['buffer_dir'])/run),
            'log_dir': str(Path(job['log_dir'])/run),
            'rows': [{'remote_id': '123', 'index': 1, 'title': 'Exact track',
                      'local_path': str(local), 'remote_url': 'https://soundcloud.com/example/track',
                      'selectable': True, 'selected': True,
                      'free_dl_probe': {'status': 'available', 'purchase_url': 'https://hypeddit.com/abc', 'host': 'hypeddit.com'}}]}
    path = Path(plan['log_dir'])/'capture-plan.json'
    path.parent.mkdir(parents=True)
    path.write_text(json.dumps(plan))
    return Context.from_plan(path)


def options(ctx):
    return SimpleNamespace(plan=ctx.plan_path, job=None, config=None, freedl_config=None,
                           udl=None, working_dir=None)


class ContextTests(unittest.TestCase):
    def test_two_jobs_independent_roots_and_no_globals(self):
        with tempfile.TemporaryDirectory() as d:
            a, b = fixture(Path(d), 'alpha'), fixture(Path(d), 'beta')
            self.assertEqual(resolve_context(options(a)).job_id, 'alpha')
            self.assertEqual(resolve_context(options(b)).job_id, 'beta')
            self.assertEqual(intake.verify_row(b.plan, '123')[1], b.library/'nested/Track.m4a')
            with self.assertRaisesRegex(ValueError, 'conflict'):
                a.same_job(b)

    def test_backend_drift_rejected_before_mutation(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            class FakeAgent:
                def call(self, method):
                    return {'config': {'jobs': [dict(ctx.job, library_dir=str(Path(d)/'other'))]}}
            (Path(d)/'other').mkdir()
            with self.assertRaisesRegex(ValueError, 'roots conflict'):
                resolve_context(options(ctx), FakeAgent())

    def test_missing_or_unsafe_roots_fail(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            for value in ('', 'relative/path', '/'):
                with self.subTest(value=value), self.assertRaises(ValueError):
                    Context.from_job(dict(ctx.job, backup_dir=value))
            with self.assertRaisesRegex(ValueError, 'Overlapping'):
                Context.from_job(dict(ctx.job, backup_dir=str(ctx.library/'backups')))

    def test_conflicting_plan_roots_and_path_traversal_fail(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            for key, value in [('run_id', '../escape'), ('buffer_root', str(Path(d)/'escape'))]:
                bad = dict(ctx.plan, **{key: value})
                ctx.plan_path.write_text(json.dumps(bad))
                with self.subTest(key=key), self.assertRaises(ValueError):
                    Context.from_plan(ctx.plan_path)
            bad = copy.deepcopy(ctx.plan)
            bad['rows'][0]['local_path'] = str(Path(d)/'outside.m4a')
            ctx.plan_path.write_text(json.dumps(bad))
            with self.assertRaisesRegex(ValueError, 'Outside'):
                Context.from_plan(ctx.plan_path)

    def test_symlink_escape_and_duplicate_identity_fail(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            ctx.buffer.mkdir(parents=True)
            (ctx.buffer/ctx.plan['run_id']).symlink_to(Path(d)/'outside')
            with self.assertRaisesRegex(ValueError, 'Outside'):
                Context.from_plan(ctx.plan_path)
            (ctx.buffer/ctx.plan['run_id']).unlink()
            ctx.plan['rows'].append(ctx.plan['rows'][0])
            ctx.plan_path.write_text(json.dumps(ctx.plan))
            with self.assertRaisesRegex(ValueError, 'duplicate'):
                Context.from_plan(ctx.plan_path)

    def test_legacy_plan_and_connection_provenance(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d), 'upgrade-09-12', '20260912-205500-manual')
            args = options(ctx)
            saved = {'job_id': ctx.job_id, 'config': str(Path(d)/'old-main.yaml'),
                     'freedl_config': str(Path(d)/'old-freedl.yaml'), 'working_dir': d}
            (ctx.plan_path.parent/'maintenance-context.json').write_text(json.dumps(saved))
            self.assertEqual(read_connection(args)['config'], Path(saved['config']).resolve())
            args.config = Path(d)/'override.yaml'
            self.assertEqual(read_connection(args)['config'], args.config.resolve())
            self.assertEqual(read_connection(args)['udl'], REPO/'bin/udl')
            saved['job_id'] = 'different'
            (ctx.plan_path.parent/'maintenance-context.json').write_text(json.dumps(saved))
            with self.assertRaisesRegex(ValueError, 'another job'):
                read_connection(args)

    def test_intake_derives_identity_and_paths_and_preserves_filename(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            source = Path(d)/'browser'/'Name  with spaces.wav'
            source.parent.mkdir()
            source.write_bytes(b'artist original')
            args = options(ctx)
            args.__dict__.update(remote_id='123', title=None, gate_url=None, source=source,
                                 run_id='new-capture', final_url=None, expected_bytes=None, expected_sha256=None)
            quality = {'duration_seconds': 10, 'codec': 'pcm_s16le', 'sample_rate': 44100,
                       'channels': 2, 'audio_bitrate': 1411200}
            with patch.object(intake, 'media_probe', return_value=quality):
                result = intake.intake(args)
            self.assertEqual(result['record']['download_name'], source.name)
            self.assertEqual(result['record']['staged_name'], '123.wav')
            created = ctx.logs/'new-capture'/'capture-plan.json'
            self.assertEqual(Context.from_plan(created).job_id, ctx.job_id)
            self.assertEqual((ctx.buffer/'new-capture/downloads/123.wav').read_bytes(), source.read_bytes())
            self.assertEqual((ctx.logs/'new-capture/capture.sync.scdl').read_text(), 'soundcloud 123 123.wav\n')
            with self.assertRaisesRegex(ValueError, 'already exists'):
                intake.intake(args)

    def test_intake_rejects_wrong_title_gate_or_source_identity(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            for remote, title, gate in [('999', None, None), ('123', 'Wrong title', None), ('123', None, 'https://hypeddit.com/wrong')]:
                with self.subTest(remote=remote, title=title, gate=gate), self.assertRaises(ValueError):
                    intake.verify_row(ctx.plan, remote, title, gate)

    def test_status_combines_single_track_intake_with_full_plan(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            extra = dict(ctx.plan['rows'][0], remote_id='456', local_path=str(ctx.library/'second.m4a'))
            Path(extra['local_path']).write_bytes(b'second')
            ctx.plan['rows'].append(extra)
            ctx.plan_path.write_text(json.dumps(ctx.plan))
            latest = copy.deepcopy(ctx.plan)
            latest['run_id'] = 'single-intake'
            latest['buffer_root'] = str(ctx.buffer/'single-intake')
            latest['log_dir'] = str(ctx.logs/'single-intake')
            latest['rows'] = [latest['rows'][0]]
            path = ctx.logs/'single-intake/capture-plan.json'
            path.parent.mkdir()
            path.write_text(json.dumps(latest))
            self.assertEqual(status.report(ctx)['counts']['pending'], 2)

    def test_nested_same_names_have_distinct_audit_baselines(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            rows = []
            for folder in ('a', 'b'):
                local = ctx.library/folder/'Track.m4a'
                local.parent.mkdir(); local.write_bytes(folder.encode())
                backup = ctx.backups/folder/'Track.m4a'
                backup.parent.mkdir(parents=True); backup.write_bytes(folder.encode())
                rows.append({'status': 'replaced', 'library_path': str(local), 'backup_path': str(backup)})
            (ctx.plan_path.parent/'promotion-result.json').write_text(json.dumps({'rows': rows}))
            self.assertEqual(set(baseline_backups(ctx=ctx)), {'a/Track.m4a', 'b/Track.m4a'})

    def test_restore_rejects_unrelated_backup_even_inside_scope(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            backup = ctx.backups/'unrelated.m4a'
            backup.parent.mkdir(parents=True); backup.write_bytes(b'wrong')
            args = SimpleNamespace(action='restore-tags', backup=backup)
            with self.assertRaisesRegex(ValueError, 'pair is absent'):
                preserve.verify_identity(ctx, args, Path(ctx.plan['rows'][0]['local_path']).resolve())

    def test_mp3_install_requires_exact_remote_capture_state(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            source = ctx.buffer/ctx.plan['run_id']/'downloads/123.mp3'
            source.parent.mkdir(parents=True); source.write_bytes(b'audio')
            args = SimpleNamespace(action='install-mp3', remote_id='123', source=source)
            state = ctx.logs/ctx.plan['run_id']/'capture.sync.scdl'
            state.write_text('soundcloud 456 123.mp3\n')
            with self.assertRaisesRegex(ValueError, 'exact remote ID'):
                preserve.verify_identity(ctx, args, Path(ctx.plan['rows'][0]['local_path']).resolve())
            state.write_text('soundcloud 123 123.mp3\n')
            preserve.verify_identity(ctx, args, Path(ctx.plan['rows'][0]['local_path']).resolve())

    def test_status_accepts_new_plan_after_recorded_extension_migration(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            old = Path(ctx.plan['rows'][0]['local_path']).resolve()
            new = old.with_suffix('.mp3')
            old.rename(new)
            migration = ctx.logs/'migration/preservation-result.json'
            migration.parent.mkdir()
            migration.write_text(json.dumps({'action': 'install-mp3', 'before': {'path': str(old)}, 'after': {'path': str(new)}}))
            latest = copy.deepcopy(ctx.plan)
            latest['run_id'] = 'after-migration'
            latest['buffer_root'] = str(ctx.buffer/latest['run_id'])
            latest['log_dir'] = str(ctx.logs/latest['run_id'])
            latest['rows'][0]['local_path'] = str(new)
            path = Path(latest['log_dir'])/'capture-plan.json'
            path.parent.mkdir(); path.write_text(json.dumps(latest))
            result = status.report(ctx)
            self.assertEqual(result['counts']['upgraded'], 1)
            self.assertEqual(result['upgraded'][0]['local_path'], str(new))

    def test_distinct_extensions_do_not_share_audit_identity(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            self.assertNotEqual(track_key(ctx.library/'track.mp3', ctx), track_key(ctx.library/'track.m4a', ctx))

    def test_migration_keeps_earliest_native_baseline(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            original = ctx.backups/'native/original.m4a'
            migrated = ctx.backups/'migration/upgraded.m4a'
            for path in (original, migrated):
                path.parent.mkdir(parents=True); path.write_bytes(b'audio')
            target = ctx.library/'nested/Track.m4a'
            (ctx.plan_path.parent/'promotion-result.json').write_text(json.dumps({'rows': [
                {'status': 'replaced', 'library_path': str(target), 'backup_path': str(original)}]}))
            log = ctx.logs/'migration/preservation-result.json'
            log.parent.mkdir()
            log.write_text(json.dumps({'action': 'install-mp3', 'before': {'path': str(target)},
                                       'after': {'path': str(target.with_suffix('.mp3'))}, 'backup': str(migrated)}))
            self.assertEqual(baseline_backups(ctx=ctx)['nested/Track.mp3'], original)

    def test_exported_legacy_plan_supplies_status_without_discovery(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            exported = Path(d)/'exported.json'
            ctx.plan_path.rename(exported)
            self.assertEqual(status.report(Context.from_plan(exported))['counts']['pending'], 1)

    def test_relative_cli_paths_resolve_before_backend_working_directory(self):
        with tempfile.TemporaryDirectory() as d:
            ctx = fixture(Path(d))
            args = options(ctx)
            args.config = Path('relative-main.yaml')
            args.freedl_config = Path('relative-freedl.yaml')
            self.assertEqual(read_connection(args)['config'], (Path.cwd()/args.config).resolve())
            self.assertEqual(read_connection(args)['freedl_config'], (Path.cwd()/args.freedl_config).resolve())
            relative_plan = Path(os.path.relpath(ctx.plan_path))
            self.assertEqual(Context.from_plan(relative_plan).job_id, ctx.job_id)

    def test_relocated_checkout_uses_its_own_binary_and_imports(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d).resolve()
            ctx = fixture(root)
            relocated = root/'relocated checkout'
            folder = relocated/'.dev/freedl-0912'
            shutil.copytree(REPO/'.dev/freedl-0912', folder, ignore=shutil.ignore_patterns('__pycache__'))
            shutil.copy2(REPO/'.dev/freedl.py', folder.parent/'freedl.py')
            result = subprocess.run([sys.executable, str(folder.parent/'freedl.py'), 'status', '--plan', str(ctx.plan_path)],
                                    cwd=root, check=True, capture_output=True, text=True)
            self.assertEqual(json.loads(result.stdout)['counts']['pending'], 1)
            result = subprocess.run([sys.executable, '-c', 'from context import REPO; print(REPO)'],
                                    cwd=folder, check=True, capture_output=True, text=True)
            self.assertEqual(Path(result.stdout.strip()), relocated)

    def test_general_entrypoint_runs_from_other_directory(self):
        with tempfile.TemporaryDirectory() as d:
            for command in ('plan', 'capture', 'intake', 'promote', 'preserve', 'status', 'audit'):
                result = subprocess.run([sys.executable, str(REPO/'.dev/freedl.py'), command, '--help'], cwd=d, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn('--plan', result.stdout)


if __name__ == '__main__':
    unittest.main()
