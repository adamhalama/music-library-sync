"""Direct app RPC / native CLI preservation acceptance, with no Python tag repair.

Fixtures and the Mutagen oracle are test-only. Production promotion runs in Go.
Set UDL_FREEDL_TEST_BINARY to the current backend. No network or user library.
"""
import copy
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest

from agent import Agent
from audit import probe, stream_hash
from test_context import fixture
try:
    from mutagen.mp4 import MP4, MP4Cover, MP4FreeForm
    from mutagen.id3 import ID3, APIC, TXXX, UFID, TIT2, COMM
except ImportError:
    MP4 = None

BINARY = os.environ.get('UDL_FREEDL_TEST_BINARY')
PROOF = []

@unittest.skipUnless(BINARY and MP4 and shutil.which('ffmpeg') and shutil.which('ffprobe'),
                     'requires current backend, Mutagen oracle and ffmpeg/ffprobe')
class NativePromotionTests(unittest.TestCase):
    def audio(self, path, codec, frequency=440, extra=()):
        subprocess.run(['ffmpeg', '-v', 'error', '-y', '-f', 'lavfi', '-i',
                        f'sine=frequency={frequency}:duration=3', '-c:a', codec,
                        '-metadata', 'title=Exact track', *extra, str(path)], check=True, capture_output=True)

    def cover(self, root):
        art=root/'cover.jpg'
        subprocess.run(['ffmpeg','-v','error','-y','-f','lavfi','-i','color=blue:s=32x32',
                        '-frames:v','1','-update','1',str(art)],check=True,capture_output=True)
        return art.read_bytes()

    def setup_job(self, root, name='native-app', extension='.m4a'):
        ctx=fixture(root,name)
        old=Path(ctx.plan['rows'][0]['local_path'])
        target=old.with_suffix(extension)
        if target!=old:
            old.unlink()
            ctx.plan['rows'][0]['local_path']=str(target)
            ctx.plan_path.write_text(json.dumps(ctx.plan))
        source=ctx.buffer/ctx.plan['run_id']/'downloads/123.flac'
        source.parent.mkdir(parents=True)
        self.audio(source,'flac',880)
        (ctx.plan_path.parent/'capture.sync.scdl').write_text('soundcloud 123 123.flac\n')
        main=root/'main.yaml'
        main.write_text('version: 1\ndefaults:\n  state_dir: '+str(root/'state')+'\nsources: []\n')
        config=root/(name+'.json')
        config.write_text(json.dumps({'version':1,'jobs':[dict(ctx.job,enabled=True,plan_limit=10,
                          target_format='wav' if extension=='.wav' else 'auto', download_order='oldest_first',
                          min_match_score=72,ambiguity_gap=8)]}))
        options=SimpleNamespace(plan=None,config=main,freedl_config=config,working_dir=root,udl=Path(BINARY))
        return ctx,target,source,options

    def finished(self, agent, request):
        rid=request['run_id']
        return agent.pump(lambda frame: frame['params'] if frame.get('method')=='run.finished'
                          and frame['params']['run_id']==rid else None)

    def build(self, agent, ctx):
        fin=self.finished(agent,agent.call('freedl.promotionPlan.build',{'job_id':ctx.job_id,'capture_run_id':ctx.plan['run_id']}))
        self.assertEqual(fin.get('exit_code'),0,fin)
        return fin['result']

    def apply(self, agent, plan):
        return self.finished(agent,agent.call('freedl.promote.apply',{'plan':plan}))

    def assert_report(self, row, target):
        report=row['preservation']
        self.assertTrue(all(report[k] for k in ('metadata_verified','artwork_verified','decode_verified')))
        self.assertEqual(row['promoted_sha256'],probe(target)['sha256'])
        self.assertEqual(report['file_sha256'],row['promoted_sha256'])
        self.assertEqual(report['audio_sha256'].split('=')[-1],stream_hash(target).split('=')[-1])

    def test_app_rpc_preserves_complete_mp4_tags_and_creation_time(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();ctx,target,source,options=self.setup_job(root)
            self.audio(target,'aac',extra=('-movflags','+faststart'))
            tags=MP4(target)
            tags['----:com.apple.iTunes:WWWAUDIOFILE']=[MP4FreeForm(b'https://soundcloud.com/example/track')]
            tags['----:test.owner:opaque']=[MP4FreeForm(b'\x00\xffbinary'),MP4FreeForm(b'other')]
            tags['\xa9ART']=['First artist','Second artist']
            tags['covr']=[MP4Cover(self.cover(root),imageformat=MP4Cover.FORMAT_JPEG)]
            tags.save();expected=copy.deepcopy(tags.tags)
            if sys.platform=='darwin':
                from preserve import restore_birthtime
                restore_birthtime(target,1600000000.123456)
            original=target.read_bytes();birth=probe(target).get('birthtime')
            with Agent(options) as agent:
                plan=self.build(agent,ctx)
                self.assertEqual(target.read_bytes(),original,'plan must not mutate library')
                fin=self.apply(agent,plan)
            self.assertEqual(fin['exit_code'],0,fin);self.assertEqual(fin['result']['replaced'],1)
            row=fin['result']['rows'][0];self.assert_report(row,target)
            PROOF.append({'route':'app RPC','container':'MP4','job':ctx.job_id,
                'backup_sha256':probe(Path(row['backup_path']))['sha256'], 'original_audio_sha256':stream_hash(Path(row['backup_path'])),
                'preservation':row['preservation'],'original_creation_time':birth,
                'installed_creation_time':probe(target).get('birthtime'),'artwork':probe(target)['artwork'],
                'independent_complete_tags_equal':MP4(target).tags==expected})
            self.assertEqual(MP4(target).tags,expected)
            self.assertNotEqual(stream_hash(target),stream_hash(Path(row['backup_path'])))
            self.assertEqual(Path(row['backup_path']).read_bytes(),original)
            self.assertTrue(probe(target,True)['decode_ok'])
            if sys.platform=='darwin':
                self.assertAlmostEqual(probe(target)['birthtime'],birth,places=6)
                self.assertAlmostEqual(probe(Path(row['backup_path']))['birthtime'],birth,places=6)
            ledger=json.loads((Path(plan['log_dir'])/'promotion-result.json').read_text())
            self.assertEqual(ledger['rows'][0]['preservation'],row['preservation'])
            print('PROOF app RPC: complete MP4 freeforms/multiple values/artwork; upgraded audio; exact backup; creation time; native result hashes',flush=True)

    def test_app_rpc_preserves_id3_private_frames_and_artwork(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();ctx,target,source,options=self.setup_job(root,'native-mp3','.mp3')
            self.audio(target,'libmp3lame')
            tags=ID3(target);tags.add(TIT2(encoding=3,text=['Exact track']))
            tags.add(TXXX(encoding=3,desc='custom field',text=['one','two']))
            tags.add(UFID(owner='test.owner',data=b'\x00\xffprivate'))
            tags.add(COMM(encoding=3,lang='eng',desc='original',text=['do not drop']))
            tags.add(APIC(encoding=3,mime='image/jpeg',type=3,desc='front',data=self.cover(root)))
            tags.save(target,v2_version=4);expected=copy.deepcopy(ID3(target));original=target.read_bytes()
            with Agent(options) as agent:
                plan=self.build(agent,ctx);fin=self.apply(agent,plan)
            self.assertEqual(fin['exit_code'],0,fin);row=fin['result']['rows'][0];self.assert_report(row,target)
            PROOF.append({'route':'app RPC','container':'MP3','preservation':row['preservation'],
                'independent_complete_tags_equal':ID3(target)==expected})
            self.assertEqual(ID3(target),expected);self.assertEqual(Path(row['backup_path']).read_bytes(),original)
            self.assertTrue(probe(target,True)['decode_ok'])
            print('PROOF app RPC: ID3 private/binary/custom frames and artwork preserved',flush=True)

    def test_app_rpc_preserves_wav_unknown_metadata_chunks(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();ctx,target,source,options=self.setup_job(root,'native-wav','.wav')
            self.audio(target,'pcm_s16le')
            payload=b'<BWFXML><COMMENT>original metadata</COMMENT></BWFXML>'
            chunk=b'iXML'+struct.pack('<I',len(payload))+payload+(b'\0' if len(payload)%2 else b'')
            raw=bytearray(target.read_bytes()+chunk);raw[4:8]=struct.pack('<I',len(raw)-8);target.write_bytes(raw)
            with Agent(options) as agent: fin=self.apply(agent,self.build(agent,ctx))
            self.assertEqual(fin['exit_code'],0,fin);self.assertIn(chunk,target.read_bytes());self.assert_report(fin['result']['rows'][0],target)
            PROOF.append({'route':'app RPC','container':'WAV','preservation':fin['result']['rows'][0]['preservation'],
                'unknown_ixml_chunk_retained':chunk in target.read_bytes()})
            print('PROOF app RPC: WAV unknown iXML metadata retained with upgraded PCM',flush=True)

    def test_app_rpc_install_failure_keeps_library_and_verified_backup(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();ctx,target,source,options=self.setup_job(root,'install-failure')
            self.audio(target,'aac');tags=MP4(target)
            tags['----:owner:private']=[MP4FreeForm(b'original private tag')];tags.save()
            original=target.read_bytes()
            with Agent(options) as agent:
                plan=self.build(agent,ctx)
                # A directory at the rollback-file location forces installation failure.
                Path(str(target)+'.udl.bak').mkdir()
                fin=self.apply(agent,plan)
            self.assertNotEqual(fin['exit_code'],0);self.assertEqual(fin['result']['failed'],1)
            row=fin['result']['rows'][0]
            self.assertEqual(row['status'],'failed');self.assertNotIn('preservation',row)
            self.assertEqual(target.read_bytes(),original)
            self.assertEqual(Path(row['backup_path']).read_bytes(),original)
            self.assertFalse(list(target.parent.glob('.udl-freedl-promote-*')))
            PROOF.append({'route':'app RPC','failure':'installation refused','library_unchanged':True,
                          'original_backup_exact':True,'failed_rows':1,'replaced_rows':0,'stages_cleaned':True})
            print('PROOF app RPC failure: original library and backup exact; failed row visible; no staged files left',flush=True)

    def test_legacy_cli_preserves_tags_without_python_repair(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();ctx,target,source,options=self.setup_job(root,'legacy-cli')
            self.audio(target,'aac');tags=MP4(target)
            tags['----:com.apple.iTunes:WWWAUDIOFILE']=[MP4FreeForm(b'https://soundcloud.com/example/track')]
            tags['covr']=[MP4Cover(self.cover(root),imageformat=MP4Cover.FORMAT_JPEG)]
            tags.save();expected=copy.deepcopy(tags.tags)
            result=subprocess.run([BINARY,'-c',str(options.config),'promote-freedl','--free-dl-dir',str(source.parent),
                    '--library-dir',str(ctx.library),'--apply'],check=True,capture_output=True,text=True,timeout=45)
            self.assertIn('replaced=1',result.stdout)
            self.assertEqual(MP4(target).tags,expected);self.assertTrue(probe(target,True)['decode_ok'])
            print('PROOF legacy CLI: complete MP4 tags/artwork preserved without Python restoration',flush=True)

    def test_cli_rejects_unrepresentable_container_conversion_without_output_write(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d).resolve();ctx,target,source,options=self.setup_job(root,'conversion')
            self.audio(target,'aac');tags=MP4(target);tags['----:owner:private']=[MP4FreeForm(b'\0\xff')];tags.save()
            original=target.read_bytes();destination=root/'converted';destination.mkdir()
            existing=destination/'nested/Track.mp3';existing.parent.mkdir();existing.write_bytes(b'previous output')
            result=subprocess.run([BINARY,'-c',str(options.config),'promote-freedl','--free-dl-dir',str(source.parent),
                    '--library-dir',str(ctx.library),'--write-dir',str(destination),'--overwrite',
                    '--target-format','mp3-320','--apply'],capture_output=True,text=True,timeout=45)
            self.assertNotEqual(result.returncode,0);self.assertIn('same container',result.stderr)
            self.assertEqual(existing.read_bytes(),b'previous output');self.assertEqual(target.read_bytes(),original)
            print('PROOF native CLI: cross-container opaque-tag loss refused; existing output and original untouched',flush=True)

if __name__=='__main__':
    result=unittest.main(verbosity=2,exit=False).result
    if os.environ.get('UDL_FREEDL_PROOF_JSON'):
        Path(os.environ['UDL_FREEDL_PROOF_JSON']).write_text(json.dumps({
            'tests_run':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),
            'skipped':len(result.skipped),'evidence':PROOF},indent=2)+'\n')
    sys.exit(not result.wasSuccessful())
