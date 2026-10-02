"""Regression tests for evidence classification and rollback; no library writes."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from audit import baseline_backups, classify_change, promotion_ledger
import preserve
import promote


class QualityTests(unittest.TestCase):
    def test_metadata_only_change_is_not_upgrade(self):
        before = {'sha256':'old', 'encoded_audio_sha256':'same', 'birthtime':10}
        after = dict(before, sha256='new')
        result = classify_change(before, after, 'promotion.json')
        self.assertFalse(result['upgraded'])
        self.assertFalse(result['changed_audio'])
        self.assertTrue(result['birthtime_preserved'])

    def test_changed_audio_requires_promotion_evidence(self):
        before = {'encoded_audio_sha256':'old', 'birthtime':10}
        after = {'encoded_audio_sha256':'new', 'birthtime':20}
        self.assertFalse(classify_change(before, after, None)['upgraded'])
        result = classify_change(before, after, 'promotion.json')
        self.assertTrue(result['upgraded'])
        self.assertFalse(result['birthtime_preserved'])

    def test_metadata_repair_ledger_does_not_count(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            logs = root/'logs'/'repair'
            logs.mkdir(parents=True)
            (logs/'preservation-result.json').write_text(json.dumps({'action':'restore-tags','after':{'path':'track.m4a'}}))
            self.assertEqual(promotion_ledger(root), {})

    def test_evidence_failure_restores_old_path_and_removes_new(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            target, output, stage, backup = [root/p for p in ('track.m4a','track.mp3','stage.mp3','backup.m4a')]
            target.write_bytes(b'old audio'); backup.write_bytes(b'old audio'); stage.write_bytes(b'new audio')
            original = preserve.write_evidence
            def fail_commit(path, result):
                if path.name == 'preservation-result.json':
                    raise OSError('simulated full disk')
                original(path, result)
            with patch.object(preserve, 'write_evidence', fail_commit):
                with self.assertRaisesRegex(OSError, 'full disk'):
                    preserve.install_verified(stage,target,output,backup,None,root,{'after':{}})
            self.assertEqual(target.read_bytes(), b'old audio')
            self.assertFalse(output.exists())
            self.assertEqual(json.loads((root/'transaction.json').read_text())['status'], 'prepared')

    def test_success_records_actual_installed_birthtime(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            target, stage, backup = [root/p for p in ('track.m4a','stage.m4a','backup.m4a')]
            target.write_bytes(b'old audio'); backup.write_bytes(b'old audio'); stage.write_bytes(b'new audio')
            result = {'after':{'birthtime':-1}}
            preserve.install_verified(stage,target,target,backup,None,root,result)
            recorded = json.loads((root/'preservation-result.json').read_text())
            self.assertEqual(recorded['after']['birthtime'],getattr(target.stat(),'st_birthtime',None))
            self.assertEqual(target.read_bytes(), b'new audio')

    def test_partial_promotion_repairs_successful_rows_before_error(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            backup = root/'backups'/'track.m4a'
            backup.parent.mkdir()
            backup.write_bytes(b'original')
            target = root/'track.m4a'
            target.write_bytes(b'promoted')
            frame = {'exit_code':1,'result':{'failed':1,'rows':[
                {'status':'replaced','backup_path':str(backup),'library_path':str(target)},
                {'status':'failed','library_path':str(root/'other.m4a')}]}}
            with patch.object(promote,'LIB',root), patch.object(promote,'SCRATCH',root), patch.object(promote.subprocess,'run') as run:
                with self.assertRaisesRegex(RuntimeError,'partially completed'):
                    promote.repair_replaced_rows(frame)
                self.assertEqual(run.call_count,1)
                self.assertEqual(run.call_args.args[0][-3:],['restore-tags',str(backup.resolve()),str(target.resolve())])

    def test_promotion_missing_backup_reports_failure(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            frame = {'result':{'rows':[{'status':'replaced','backup_path':str(root/'backups/missing.m4a'),'library_path':str(root/'track.m4a')}]}}
            with patch.object(promote,'LIB',root), patch.object(promote,'SCRATCH',root), patch.object(promote.subprocess,'run') as run:
                with self.assertRaisesRegex(RuntimeError,'missing'):
                    promote.repair_replaced_rows(frame)
                run.assert_not_called()

    def test_promotion_skipped_rows_do_not_trigger_repair(self):
        with patch.object(promote.subprocess,'run') as run:
            promote.repair_replaced_rows({'exit_code':0,'result':{'rows':[{'status':'skipped'}]}})
            run.assert_not_called()

    def test_original_baseline_wins_over_earlier_named_metadata_repair(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            original = root/'backups'/'20260929-213700-local'/'nested'/'track.m4a'
            repair = root/'backups'/'20260929-193700-utc-repair'/'track.m4a'
            for path in (original, repair):
                path.parent.mkdir(parents=True)
                path.write_bytes(b'original' if path == original else b'upgraded')
            log = root/'logs'/'nested'/'run'/'promotion-result.json'
            log.parent.mkdir(parents=True)
            log.write_text(json.dumps({'rows':[{'status':'replaced','library_path':'track.m4a','backup_path':str(original)}]}))
            self.assertEqual(baseline_backups(root), {'track':original})
            self.assertIn('track', promotion_ledger(root))

    def test_missing_original_does_not_fall_back_to_repair(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            log = root/'logs'/'run'/'promotion-result.json'
            log.parent.mkdir(parents=True)
            log.write_text(json.dumps({'rows':[{'status':'replaced','library_path':'track.m4a','backup_path':str(root/'backups/missing.m4a')}]}))
            with self.assertRaisesRegex(FileNotFoundError,'baseline missing'):
                baseline_backups(root)

    def test_migration_baseline_uses_original_extension(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            backup = root/'backups'/'old.m4a'
            backup.parent.mkdir()
            backup.write_bytes(b'original')
            log = root/'logs'/'run'/'preservation-result.json'
            log.parent.mkdir(parents=True)
            log.write_text(json.dumps({'action':'install-mp3','after':{'path':'old.mp3'},'backup':str(backup)}))
            self.assertEqual(baseline_backups(root), {'old':backup})


if __name__ == '__main__':
    unittest.main()
