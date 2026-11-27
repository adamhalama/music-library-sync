"""Exercise source import transactions without opening a Rekordbox database."""
import copy
import datetime
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

# The transaction tests use database doubles and need no installed pyrekordbox.
pyrekordbox = types.ModuleType("pyrekordbox")
pyrekordbox.Rekordbox6Database = object
pyrekordbox_db6 = types.ModuleType("pyrekordbox.db6")
pyrekordbox_db6.tables = types.SimpleNamespace()
with patch.dict(sys.modules, {"pyrekordbox": pyrekordbox, "pyrekordbox.db6": pyrekordbox_db6}):
    spec = importlib.util.spec_from_file_location("playlist_sync_helper", Path(__file__).with_name("playlist_sync_helper.py"))
    helper = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helper)


def content(identifier, path, created):
    return types.SimpleNamespace(ID=identifier, FolderPath=path, created_at=datetime.datetime.fromisoformat(created.replace("Z", "+00:00")), StockDate="2026-01-01", Title="Existing", GenreID="7", arbitrary="untouched")


class Database:
    def __init__(self, rows):
        self.rows = rows
        self.commits = 0
        self.rollbacks = 0
        self.closed = False

    def get_content(self, **kwargs):
        if "ID" in kwargs:
            return next((row for row in self.rows if row.ID == kwargs["ID"]), None)
        return types.SimpleNamespace(all=lambda: list(self.rows), order_by=lambda *_: self.get_content())

    def query(self, column):
        assert column == "FolderPath"
        return types.SimpleNamespace(all=lambda: [(row.FolderPath,) for row in self.rows])

    def flush(self):
        pass

    def commit(self, **kwargs):
        self.commits += 1

    def rollback(self):
        self.rollbacks += 1

    def close(self):
        self.closed = True


class SourceImportTests(unittest.TestCase):
    def setUp(self):
        self.db = Database([
            content("old", "/music/Old.m4a", "2026-11-21T10:00:00.900Z"),
            # Even an absent remote row with a zero fraction anchors new imports.
            content("absent", "/music/Absent.m4a", "2027-01-01T00:00:00.000Z"),
            content("other", "/music-other/Other.m4a", "2030-01-01T00:00:00.900Z"),
        ])
        self.request = {"db_dir": "/unused", "folder_prefix": "/music", "append_only": True, "rows": [
            {"key": "new-a", "action": "import", "path": "/music/A.m4a", "created_at": "2027-01-01T00:00:01.900Z", "stock_date": "2027-01-01", "tags": {"genre": "House"}},
            {"key": "new-b", "action": "import", "path": "/music/B.m4a", "created_at": "2027-01-01T00:00:02.900Z", "stock_date": "2027-01-01", "tags": {"genre": "Techno"}},
        ]}

    def apply(self, request):
        def import_content(db, row, cache):
            result = content(row["key"], row["path"], "2000-01-01T00:00:00.900Z")
            result.Genre = row["tags"]["genre"]
            db.rows.append(result)
            return result
        table = types.SimpleNamespace(FolderPath="FolderPath")
        with patch.object(helper, "Rekordbox6Database", return_value=self.db), patch.object(helper, "import_content", side_effect=import_content), patch.object(helper.tables, "DjmdContent", table, create=True):
            return helper.source_import_apply(request)

    def test_append_only_import_preserves_existing_rows_byte_for_byte(self):
        before = json.dumps([vars(row) for row in self.db.rows], default=str, sort_keys=True)
        response = self.apply(self.request)
        after = json.dumps([vars(row) for row in self.db.rows[:3]], default=str, sort_keys=True)
        self.assertEqual(before, after)
        self.assertEqual([row["key"] for row in response["rows"]], ["new-a", "new-b"])
        self.assertEqual([row["created_at"] for row in response["rows"]], [row["created_at"] for row in self.request["rows"]])
        self.assertEqual([row.Genre for row in self.db.rows[3:]], ["House", "Techno"])
        self.assertEqual(self.db.commits, 1)
        self.assertTrue(self.db.closed)

    def test_append_only_rejects_update_before_any_import(self):
        request = copy.deepcopy(self.request)
        request["rows"][1]["action"] = "update"
        with self.assertRaisesRegex(RuntimeError, "cannot update existing"):
            self.apply(request)
        self.assertEqual(len(self.db.rows), 3)
        self.assertEqual(self.db.commits, 0)
        self.assertEqual(self.db.rollbacks, 1)

    def test_append_only_rechecks_latest_source_timestamp(self):
        request = copy.deepcopy(self.request)
        request["rows"][0]["created_at"] = "2026-12-01T00:00:00.900Z"
        with self.assertRaisesRegex(RuntimeError, "must follow every existing"):
            self.apply(request)
        self.assertEqual(len(self.db.rows), 3)
        self.assertEqual(self.db.commits, 0)

    def test_append_only_rejects_outside_path_and_reversed_imports(self):
        for mutate in (lambda req: req["rows"][0].update(path="/music-other/New.m4a"), lambda req: req["rows"].reverse()):
            request = copy.deepcopy(self.request)
            mutate(request)
            with self.assertRaises(RuntimeError):
                self.apply(request)
            self.assertEqual(len(self.db.rows), 3)
            self.assertEqual(self.db.commits, 0)

    def test_symlinked_source_inspects_both_paths_and_anchors_new_imports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            target = root / "music"
            target.mkdir()
            alias = root / "alias"
            alias.symlink_to(target, target_is_directory=True)
            self.db.rows[0].FolderPath = str(target / "Old.m4a")
            self.db.rows[1].FolderPath = str(alias / "Absent.m4a")
            self.db.rows[2].FolderPath = str(root / "music-other" / "Other.m4a")
            request = copy.deepcopy(self.request)
            request["folder_prefix"] = str(target)
            for row in request["rows"]:
                row["path"] = str(target / Path(row["path"]).name)
            table = types.SimpleNamespace(ID="ID")
            with patch.object(helper, "Rekordbox6Database", return_value=self.db), patch.object(helper.tables, "DjmdContent", table, create=True):
                inspected = helper.source_import_inspect(request)["contents"]
            self.assertEqual([row["folder_path"] for row in inspected], [row.FolderPath for row in self.db.rows[:2]])

            stale = copy.deepcopy(request)
            stale["rows"][0]["created_at"] = "2026-12-01T00:00:00.900Z"
            with self.assertRaisesRegex(RuntimeError, "must follow every existing"):
                self.apply(stale)
            outside = root / "outside.m4a"
            outside.touch()
            escape = target / "escape.m4a"
            escape.symlink_to(outside)
            escaping = copy.deepcopy(request)
            escaping["rows"][0]["path"] = str(escape)
            with self.assertRaisesRegex(RuntimeError, "outside the source folder"):
                self.apply(escaping)
            duplicate = copy.deepcopy(request)
            duplicate["rows"][0]["path"] = str(target / "Absent.m4a")
            with self.assertRaisesRegex(RuntimeError, "already in the Rekordbox collection"):
                self.apply(duplicate)
            result = self.apply(request)
            self.assertEqual([row["path"] for row in result["rows"]], [row["path"] for row in request["rows"]])
            self.assertEqual(self.db.rows[1].FolderPath, str(alias / "Absent.m4a"))
            self.assertEqual(self.db.commits, 1)


if __name__ == "__main__":
    unittest.main()
