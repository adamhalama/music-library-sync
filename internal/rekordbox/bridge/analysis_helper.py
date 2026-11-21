"""Rekordbox analysis metadata and file publication; no audio processing here."""
from __future__ import annotations

import datetime
import math
import os
from pathlib import Path
import re
import secrets
import shutil
import tempfile
import unicodedata
import uuid


EXTENSIONS = ("DAT", "EXT", "2EX")
USN_TABLES = ("djmdContent", "djmdPlaylist", "djmdSongPlaylist", "djmdSongHotCueBanklist")


def analysis_file(share, relative):
    """Treat database paths as share-relative, and reject traversal/symlinks out."""
    share = Path(share).resolve()
    target = (share / relative.lstrip("/")).resolve()
    if share not in target.parents or target.name != "ANLZ0000.DAT":
        raise ValueError(f"unsafe analysis path: {relative!r}")
    return target


def normal_path(value):
    return unicodedata.normalize("NFC", os.path.normpath(str(value)))


def inspect_connection(conn, share, req):
    ids = {str(x) for x in req.get("content_ids") or []}
    prefixes = [normal_path(Path(p).expanduser().absolute()) for p in req.get("path_prefixes") or []]
    selected = set(ids)
    playlist = str(req.get("playlist") or "")
    if not (ids or prefixes or playlist or req.get("all")):
        raise ValueError("at least one analysis selector is required")
    if playlist:
        rows = conn.execute("SELECT ID FROM djmdPlaylist WHERE ID = ? AND rb_local_deleted = 0", (playlist,)).fetchall()
        if not rows:
            rows = conn.execute("SELECT ID FROM djmdPlaylist WHERE Name = ? AND rb_local_deleted = 0", (playlist,)).fetchall()
        if len(rows) != 1:
            raise ValueError("playlist not found or ambiguous; use its ID")
        selected.update(str(r[0]) for r in conn.execute("SELECT ContentID FROM djmdSongPlaylist WHERE PlaylistID = ? AND rb_local_deleted = 0", (rows[0][0],)))
    contents = []
    found = set()
    for row in conn.execute("SELECT ID, FolderPath, UUID, AnalysisDataPath, Analysed FROM djmdContent WHERE rb_local_deleted = 0 ORDER BY ID"):
        content_id, path, track_uuid, relative, analysed = row
        content_id, path = str(content_id), str(path or "")
        found.add(content_id)
        key = normal_path(path)
        if not (req.get("all") or content_id in selected or any(key == p or key.startswith(p.rstrip(os.sep) + os.sep) for p in prefixes)):
            continue
        relative = str(relative or "")
        existing = analysis_file(share, relative) if relative else None
        contents.append(dict(id=content_id, path=path, uuid=str(track_uuid or ""), analysis_data_path=relative,
                             existing_dir=str(existing.parent) if existing else "", anlz_exists=bool(existing and existing.is_file()), analysed=int(analysed or 0)))
    missing = ids - found
    if missing:
        raise ValueError("content IDs not found: " + ", ".join(sorted(missing)))
    # Count distinct live lookup names, so duplicate rows do not bias detection.
    names = {str(r[0]) for r in conn.execute("SELECT ScaleName FROM djmdKey WHERE rb_local_deleted = 0") if r[0]}
    alpha = sum(bool(re.fullmatch(r"(?:[1-9]|1[0-2])[AB]", n)) for n in names)
    classic = sum(bool(re.fullmatch(r"[A-G](?:b|#)?m?", n)) for n in names)
    return dict(contents=contents, key_notation="alphanumeric" if alpha > classic else "classic")


def analysis_inspect(req):
    # SQLite mode=ro can still create/write WAL and SHM sidecars. Read a private
    # DB/WAL copy instead so even planning an open library leaves it untouched.
    import sqlcipher3
    from pyrekordbox.db6.database import BLOB
    from pyrekordbox.utils import deobfuscate
    db_dir = Path(req["db_dir"]).expanduser().resolve()
    with tempfile.TemporaryDirectory(prefix="udl-rbl-plan-") as temporary:
        snapshot = Path(temporary) / "master.db"
        def fingerprint(path):
            try:
                stat = path.stat()
                return (stat.st_ino, stat.st_size, stat.st_mtime_ns)
            except FileNotFoundError:
                return None
        sources = (db_dir / "master.db", db_dir / "master.db-wal")
        for attempt in range(3):
            before = [fingerprint(p) for p in sources]
            if before[0] is None:
                raise FileNotFoundError(sources[0])
            shutil.copyfile(sources[0], snapshot)
            wal = snapshot.with_name("master.db-wal")
            if before[1] is not None:
                try:
                    shutil.copyfile(sources[1], wal)
                except FileNotFoundError:
                    continue
            elif wal.exists():
                wal.unlink()
            if before == [fingerprint(p) for p in sources]:
                break
        else:
            raise ValueError("library changed while taking a read-only snapshot; retry when idle")
        conn = sqlcipher3.connect(snapshot.as_uri() + "?mode=ro", uri=True)
        try:
            conn.execute("PRAGMA key='" + deobfuscate(BLOB).replace("'", "''") + "'")
            conn.execute("PRAGMA query_only=ON")
            return inspect_connection(conn, db_dir / "share", req)
        finally:
            conn.close()


def next_usn(conn):
    rows = conn.execute("SELECT int_1 FROM agentRegistry WHERE registry_id = 'localUpdateCount'").fetchall()
    if len(rows) != 1:
        raise ValueError("missing or duplicate local update counter")
    highest = int(rows[0][0] or 0)
    for table in USN_TABLES:
        if conn.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)).fetchone():
            highest = max(highest, int(conn.execute(f"SELECT COALESCE(MAX(rb_local_usn),0) FROM {table}").fetchone()[0]))
    if highest >= 2**63 - 1:
        raise ValueError("local update counter overflow")
    return highest + 1


def counter(conn, usn, stamp):
    conn.execute("UPDATE agentRegistry SET int_1=?, updated_at=? WHERE registry_id='localUpdateCount'", (usn, stamp))


def write_metadata(conn, planned, result, relative):
    """Independent SQL implementation of RBxport's observed library fields."""
    row = conn.execute("SELECT FolderPath, UUID, AnalysisDataPath, Analysed FROM djmdContent WHERE ID=? AND rb_local_deleted=0", (planned["id"],)).fetchone()
    if not row or tuple(str(v or "") for v in row[:3]) != (planned["path"], planned["uuid"], planned["analysis_data_path"]) or int(row[3] or 0) != planned["analysed"]:
        raise ValueError("track changed since analysis planning")
    if int(row[3] or 0) & 128:
        raise ValueError("analysis is locked")
    bpm, duration = float(result["bpm"]), float(result["duration_seconds"])
    if not all(math.isfinite(x) and x >= 0 for x in (bpm, duration)):
        raise ValueError("invalid analyser BPM or duration")
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f")[:-3] + " +00:00"
    key_id = None
    name = str(result.get("key") or "").strip()
    if name:
        key = conn.execute("SELECT ID FROM djmdKey WHERE ScaleName=? AND rb_local_deleted=0", (name,)).fetchone()
        if key:
            key_id = str(key[0])
        else:
            while True:
                key_id = str(secrets.randbelow(2**28 - 1) + 1)
                if not conn.execute("SELECT 1 FROM djmdKey WHERE ID=?", (key_id,)).fetchone():
                    break
            usn = next_usn(conn)
            conn.execute("INSERT INTO djmdKey (ID, ScaleName, UUID, rb_data_status, rb_local_data_status, rb_local_deleted, rb_local_synced, usn, rb_local_usn, created_at, updated_at) VALUES (?,?,?,0,0,0,0,NULL,?,?,?)", (key_id, name, str(uuid.uuid4()), usn, stamp, stamp))
            counter(conn, usn, stamp)
    usn = next_usn(conn)
    analysed = int(row[3] or 0)
    if analysed & 127 in (0, 1):
        analysed = (analysed & 128) | 105
    # Round halves up, matching Rust f64::round for nonnegative values.
    bpm_x100 = min(2**32 - 1, math.floor(bpm * 100 + 0.5))
    seconds = min(2**32 - 1, math.floor(duration + 0.5))
    conn.execute("UPDATE djmdContent SET BPM=?, KeyID=COALESCE(?,KeyID), AnalysisDataPath=?, Length=?, Analysed=?, ContentLink=COALESCE(ContentLink,?), AnalysisUpdated=CAST(COALESCE(AnalysisUpdated,'0') AS INTEGER)+1, rb_local_usn=?, updated_at=? WHERE ID=?", (bpm_x100, key_id, relative, seconds, analysed, 0x2c0600, usn, stamp, planned["id"]))
    counter(conn, usn, stamp)


def publish_track(conn, db_dir, item, commit, rollback):
    planned = item["content"]
    relative = planned["analysis_data_path"]
    if not relative:
        track_uuid = planned["uuid"]
        if len(track_uuid) <= 3 or not re.fullmatch(r"[A-Za-z0-9-]+", track_uuid):
            raise ValueError("track has no UUID to derive an analysis path from")
        relative = f"/PIONEER/USBANLZ/{track_uuid[:3]}/{track_uuid[3:]}/ANLZ0000.DAT"
    target = analysis_file(db_dir / "share", relative).parent
    output = Path(item["output_dir"])
    # Validate the complete output before changing any existing sibling.
    for ext in EXTENSIONS:
        with (output / f"ANLZ0000.{ext}").open("rb") as file:
            if file.read(4) != b"PMAI":
                raise ValueError(f"invalid or missing ANLZ0000.{ext}")
    target.mkdir(parents=True, exist_ok=True)
    staged = Path(tempfile.mkdtemp(prefix=".udl-analysis-", dir=target.parent))
    published = []
    try:
        for ext in EXTENSIONS:
            name = f"ANLZ0000.{ext}"
            shutil.copyfile(output / name, staged / name)
            if (target / name).exists():
                shutil.copyfile(target / name, staged / (name + ".previous"))
        write_metadata(conn, planned, item["result"], relative)
        for ext in EXTENSIONS:
            name = f"ANLZ0000.{ext}"
            os.replace(staged / name, target / name)
            published.append(name)
        commit()
        return dict(id=planned["id"], analysis_data_path=relative)
    except Exception:
        rollback()
        for name in reversed(published):
            previous = staged / (name + ".previous")
            if previous.exists():
                os.replace(previous, target / name)
            else:
                (target / name).unlink()
        raise
    finally:
        shutil.rmtree(staged)


def analysis_apply(req):
    from pyrekordbox import Rekordbox6Database
    db_dir = Path(req["db_dir"]).expanduser().resolve()
    rows = []
    # One transaction per track isolates failures and keeps the registry counter
    # with its row. Use raw SQL through pyrekordbox's SQLCipher connection; no ORM
    # objects are mutated. Commit the session directly (equivalent to autoinc=False)
    # so pyrekordbox neither assigns another USN nor rewrites playlist XML.
    db = Rekordbox6Database(path=db_dir / "master.db", db_dir=db_dir)
    try:
        for item in req.get("rows") or []:
            try:
                connection = db.session.connection().connection.driver_connection
                connection.execute("BEGIN IMMEDIATE")
                rows.append(publish_track(connection, db_dir, item, db.session.commit, db.rollback))
            except Exception as exc:
                db.rollback()
                rows.append(dict(id=item["content"]["id"], error=str(exc)))
        return dict(rows=rows)
    finally:
        db.close()
