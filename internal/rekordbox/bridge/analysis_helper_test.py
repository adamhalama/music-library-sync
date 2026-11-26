"""Standard-library sqlite fixtures for analysis planning and publication."""
import copy
import os
from pathlib import Path
import signal
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

from analysis_helper import inspect_connection, publish_track, write_metadata, analysis_file


class AnalysisTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.db = sqlite3.connect(":memory:")
        self.db.executescript("""
        CREATE TABLE djmdContent(ID TEXT PRIMARY KEY, FolderPath TEXT, UUID TEXT,
          AnalysisDataPath TEXT, Analysed INTEGER, BPM INTEGER, KeyID TEXT, Length INTEGER,
          ContentLink INTEGER, AnalysisUpdated TEXT, rb_local_usn INTEGER,
          rb_local_deleted INTEGER DEFAULT 0, updated_at TEXT);
        CREATE TABLE djmdKey(ID TEXT PRIMARY KEY, ScaleName TEXT, UUID TEXT,
          rb_data_status INTEGER, rb_local_data_status INTEGER, rb_local_deleted INTEGER DEFAULT 0,
          rb_local_synced INTEGER, usn INTEGER, rb_local_usn INTEGER, created_at TEXT, updated_at TEXT);
        CREATE TABLE djmdPlaylist(ID TEXT, Name TEXT, rb_local_deleted INTEGER DEFAULT 0, rb_local_usn INTEGER, Attribute INTEGER DEFAULT 0, SmartList TEXT);
        CREATE TABLE djmdSongPlaylist(PlaylistID TEXT, ContentID TEXT, rb_local_deleted INTEGER DEFAULT 0, rb_local_usn INTEGER);
        CREATE TABLE agentRegistry(registry_id TEXT, int_1 INTEGER, updated_at TEXT);
        INSERT INTO agentRegistry VALUES ('localUpdateCount', 5, 'old');
        INSERT INTO djmdPlaylist(ID,Name,rb_local_deleted,rb_local_usn) VALUES ('p1','Mix',0,50);
        INSERT INTO djmdSongPlaylist VALUES ('p1','1',0,3);
        INSERT INTO djmdContent(ID,FolderPath,UUID,AnalysisDataPath,Analysed,rb_local_usn)
          VALUES ('1','/music/one.mp3','abcdef-123456','',0,8),
                 ('2','/music-two/two.mp3','abcdef-234567','',1,7);
        """)
        self.db.commit()
        self.content = dict(id="1", path="/music/one.mp3", uuid="abcdef-123456", analysis_data_path="", analysed=0, anlz_exists=False)
        self.result = dict(bpm=127.555, key="Ebm", duration_seconds=100.5)
        self.out = self.root / "out"
        self.out.mkdir()
        for ext in ("DAT", "EXT", "2EX"):
            (self.out / f"ANLZ0000.{ext}").write_bytes(b"PMAInew" + ext.encode())
        self.item = dict(content=self.content, result=self.result, output_dir=str(self.out))

    def tearDown(self):
        self.db.close()
        self.temp.cleanup()

    def test_selectors_union_and_folder_boundaries(self):
        req = dict(path_prefixes=["/music"])
        response = inspect_connection(self.db, self.root / "share", req)
        self.assertEqual([r["id"] for r in response["contents"]], ["1"])
        req["content_ids"] = ["2"]
        self.assertEqual(len(inspect_connection(self.db, self.root, req)["contents"]), 2)
        self.assertEqual(len(inspect_connection(self.db, self.root, dict(playlist="Mix"))["contents"]), 1)
        with self.assertRaises(ValueError):
            inspect_connection(self.db, self.root, dict(content_ids=["missing"]))
        with self.assertRaises(ValueError):
            inspect_connection(self.db, self.root, {})

    def test_notation_and_existing_files(self):
        self.assertEqual(inspect_connection(self.db,self.root,dict(all=True))["key_notation"], "classic")
        self.db.execute("INSERT INTO djmdKey(ID,ScaleName) VALUES ('k','2A')")
        self.assertEqual(inspect_connection(self.db,self.root,dict(all=True))["key_notation"], "alphanumeric")
        relative = "/PIONEER/USBANLZ/abc/def/ANLZ0000.DAT"
        dat = analysis_file(self.root / "share",relative)
        dat.parent.mkdir(parents=True)
        dat.write_bytes(b"PMAI")
        self.db.execute("UPDATE djmdContent SET AnalysisDataPath=?,Analysed=233 WHERE ID='1'",(relative,))
        contents = inspect_connection(self.db,self.root / "share",dict(all=True))["contents"]
        self.assertTrue(contents[0]["anlz_exists"])
        self.assertEqual(contents[0]["analysed"],233)

    def test_metadata_and_counter_match_rbxport(self):
        published = publish_track(self.db,self.root,self.item,self.db.commit,self.db.rollback)
        row = self.db.execute("SELECT BPM,Length,Analysed,ContentLink,AnalysisUpdated,rb_local_usn,AnalysisDataPath FROM djmdContent WHERE ID='1'").fetchone()
        self.assertEqual(row[:6], (12756,101,105,0x2c0600,"1",52))
        self.assertEqual(row[6],"/PIONEER/USBANLZ/abc/def-123456/ANLZ0000.DAT")
        self.assertEqual(self.db.execute("SELECT int_1 FROM agentRegistry").fetchone()[0],52)
        self.assertEqual(self.db.execute("SELECT ScaleName,rb_local_usn FROM djmdKey").fetchone(),("Ebm",51))
        target = analysis_file(self.root / "share",published["analysis_data_path"])
        self.assertEqual(target.read_bytes(), b"PMAInewDAT")
        # A completed state and a non-NULL content link survive reanalysis.
        self.db.execute("UPDATE djmdContent SET Analysed=17, ContentLink=99 WHERE ID='1'")
        self.content.update(analysis_data_path=published["analysis_data_path"],analysed=17,anlz_exists=True)
        publish_track(self.db,self.root,self.item,self.db.commit,self.db.rollback)
        self.assertEqual(self.db.execute("SELECT Analysed,ContentLink,AnalysisUpdated FROM djmdContent WHERE ID='1'").fetchone(),(17,99,"2"))

    def test_lock_and_stale_plan_refusal(self):
        self.db.execute("UPDATE djmdContent SET Analysed=128 WHERE ID='1'")
        self.content["analysed"]=128
        with self.assertRaisesRegex(ValueError,"locked"):
            write_metadata(self.db,self.content,self.result,"/path")
        self.content["analysed"]=0
        with self.assertRaisesRegex(ValueError,"changed"):
            write_metadata(self.db,self.content,self.result,"/path")

    def test_incomplete_analyser_output_never_published(self):
        (self.out / "ANLZ0000.EXT").unlink()
        with self.assertRaises(FileNotFoundError):
            publish_track(self.db,self.root,self.item,self.db.commit,self.db.rollback)
        self.assertFalse((self.root / "share").exists())
        self.assertEqual(self.db.execute("SELECT int_1 FROM agentRegistry").fetchone()[0],5)

    def test_publish_error_restores_old_files_and_database(self):
        relative = "/PIONEER/USBANLZ/abc/def/ANLZ0000.DAT"
        target = analysis_file(self.root / "share",relative).parent
        target.mkdir(parents=True)
        for ext in ("DAT","EXT","2EX"):
            (target / f"ANLZ0000.{ext}").write_bytes(b"PMAIold")
        self.db.execute("UPDATE djmdContent SET AnalysisDataPath=? WHERE ID='1'",(relative,))
        self.db.commit()
        self.content.update(analysis_data_path=relative, anlz_exists=True)
        replace = os.replace
        calls = []
        def fail_second(source, dest):
            calls.append(source)
            if len(calls)==2:
                raise OSError("disk full")
            replace(source,dest)
        with patch("analysis_helper.os.replace",side_effect=fail_second):
            with self.assertRaisesRegex(OSError,"disk full"):
                publish_track(self.db,self.root,self.item,self.db.commit,self.db.rollback)
        for ext in ("DAT","EXT","2EX"):
            self.assertEqual((target / f"ANLZ0000.{ext}").read_bytes(),b"PMAIold")
        self.assertEqual(self.db.execute("SELECT int_1 FROM agentRegistry").fetchone()[0],5)
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM djmdKey").fetchone()[0],0)

    def test_interrupt_after_replace_restores_files_and_database(self):
        relative = "/PIONEER/USBANLZ/abc/def/ANLZ0000.DAT"
        target = analysis_file(self.root / "share", relative).parent
        target.mkdir(parents=True)
        (target / "ANLZ0000.DAT").write_bytes(b"PMAIoldDAT")
        (target / "ANLZ0000.2EX").write_bytes(b"PMAIold2EX")
        self.db.execute("UPDATE djmdContent SET AnalysisDataPath=? WHERE ID='1'", (relative,))
        self.db.commit()
        self.content.update(analysis_data_path=relative, anlz_exists=True)
        replace = os.replace

        def interrupt_after_ext(source, dest):
            replace(source, dest)
            if source.name == "ANLZ0000.EXT":
                raise KeyboardInterrupt

        with patch("analysis_helper.os.replace", side_effect=interrupt_after_ext):
            with self.assertRaises(KeyboardInterrupt):
                publish_track(self.db, self.root, self.item, self.db.commit, self.db.rollback)
        self.assertEqual((target / "ANLZ0000.DAT").read_bytes(), b"PMAIoldDAT")
        self.assertFalse((target / "ANLZ0000.EXT").exists())
        self.assertEqual((target / "ANLZ0000.2EX").read_bytes(), b"PMAIold2EX")
        self.assertEqual(self.db.execute("SELECT BPM FROM djmdContent WHERE ID='1'").fetchone()[0], None)
        self.assertEqual(self.db.execute("SELECT int_1 FROM agentRegistry").fetchone()[0], 5)
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM djmdKey").fetchone()[0], 0)

    def test_interrupt_after_commit_keeps_files_and_database(self):
        def interrupt_after_commit():
            self.db.commit()
            signal.raise_signal(signal.SIGINT)

        with self.assertRaises(KeyboardInterrupt):
            publish_track(self.db, self.root, self.item, interrupt_after_commit, self.db.rollback)

        relative, bpm = self.db.execute("SELECT AnalysisDataPath, BPM FROM djmdContent WHERE ID='1'").fetchone()
        self.assertEqual(bpm, 12756)
        target = analysis_file(self.root / "share", relative).parent
        for ext in ("DAT", "EXT", "2EX"):
            self.assertEqual((target / f"ANLZ0000.{ext}").read_bytes(), b"PMAInew" + ext.encode())

    def test_analysis_files_appearing_after_plan_require_reanalyse(self):
        dat = analysis_file(self.root / "share", "/PIONEER/USBANLZ/abc/def-123456/ANLZ0000.DAT")
        dat.parent.mkdir(parents=True)
        dat.write_bytes(b"PMAIold")

        self.db.execute("BEGIN IMMEDIATE")
        with self.assertRaisesRegex(ValueError, "analysis files appeared since planning; rerun, or use --reanalyse"):
            publish_track(self.db, self.root, self.item, self.db.commit, self.db.rollback)
        self.assertEqual(dat.read_bytes(), b"PMAIold")
        self.assertEqual(self.db.execute("SELECT AnalysisDataPath FROM djmdContent WHERE ID='1'").fetchone()[0], "")

        self.db.execute("BEGIN IMMEDIATE")
        publish_track(self.db, self.root, self.item, self.db.commit, self.db.rollback, reanalyse=True)
        self.assertEqual(dat.read_bytes(), b"PMAInewDAT")

    def test_missing_uuid_and_unsafe_paths_refused(self):
        self.content["uuid"]=""
        with self.assertRaisesRegex(ValueError,"UUID"):
            publish_track(self.db,self.root,self.item,self.db.commit,self.db.rollback)
        for path in ("/../../ANLZ0000.DAT", "/wrong-name"):
            with self.assertRaises(ValueError):
                analysis_file(self.root / "share",path)


if __name__ == "__main__":
    unittest.main()
