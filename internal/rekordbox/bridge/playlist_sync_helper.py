#!/usr/bin/env python3
from __future__ import annotations

import datetime
import json
import os
import sys
import unicodedata
import uuid
from pathlib import Path
from typing import Any, Dict, List

from pyrekordbox import Rekordbox6Database
from pyrekordbox.db6 import tables


def playlist_content_ids(db: Rekordbox6Database, playlist_id: str) -> List[str]:
    rows = (
        db.get_playlist_songs(PlaylistID=str(playlist_id))
        .order_by(tables.DjmdSongPlaylist.TrackNo)
        .all()
    )
    return [str(row.ContentID) for row in rows]


def inspect_db(db_dir_raw: str) -> Dict[str, Any]:
    db_dir = Path(db_dir_raw).expanduser().resolve()
    db_path = db_dir / "master.db"
    db = Rekordbox6Database(path=db_path, db_dir=db_dir)
    try:
        playlists = []
        for pl in db.get_playlist().order_by(tables.DjmdPlaylist.Seq).all():
            playlists.append(
                {
                    "id": str(pl.ID),
                    "name": str(pl.Name or ""),
                    "attribute": int(pl.Attribute or 0),
                    "parent_id": str(pl.ParentID or ""),
                    "content_ids": playlist_content_ids(db, str(pl.ID)),
                }
            )

        contents = []
        for row in db.get_content().all():
            contents.append(
                {
                    "id": str(row.ID),
                    "title": str(row.Title or ""),
                    "folder_path": str(row.FolderPath or ""),
                }
            )

        return {"playlists": playlists, "contents": contents}
    finally:
        db.close()


def resolve_target_playlist(
    db: Rekordbox6Database,
    target_id: str,
    target_name: str,
    target_parent_id: str,
    create_if_missing: bool,
):
    """Resolve the playlist a plan targets, returning (playlist, created, current).

    An explicit target ID that no longer exists is an error, never a create: the
    plan was built against that playlist, so creating a fresh one under the same
    name would silently mirror into the wrong place. Only the name lookup may
    fall through to creation.
    """
    if target_id:
        playlist = db.get_playlist(ID=target_id)
        if playlist is None:
            raise RuntimeError(f"Rekordbox playlist ID {target_id!r} not found")
    else:
        matches = db.get_playlist(Name=target_name).all()
        if target_parent_id:
            matches = [row for row in matches if str(row.ParentID or "") == target_parent_id]
        if len(matches) > 1:
            raise RuntimeError(f"Multiple Rekordbox playlists named {target_name!r}; use playlist ID")
        playlist = matches[0] if matches else None

    if playlist is None:
        if not create_if_missing:
            raise RuntimeError(f"Rekordbox playlist {target_name!r} not found")
        return db.create_playlist(target_name, parent=(target_parent_id or None)), True, []

    if int(playlist.Attribute or 0) != 0:
        raise RuntimeError(f"Target playlist {playlist.Name!r} is not a normal playlist")
    if target_parent_id and str(playlist.ParentID or "") != target_parent_id:
        raise RuntimeError(f"Target playlist {playlist.Name!r} is not under the planned folder")
    return playlist, False, playlist_content_ids(db, str(playlist.ID))


def rewrite_playlist(db: Rekordbox6Database, req: Dict[str, Any]):
    """Replace one playlist's contents with final_content_ids.

    Validates preconditions, then stages the rewrite. Does not commit, so the
    caller controls transaction scope for both single and batch applies.
    """
    target_id = str(req.get("target_playlist_id") or "")
    target_name = str(req.get("target_playlist_name") or "")
    target_parent_id = str(req.get("target_parent_id") or "")
    create_if_missing = bool(req.get("create_playlist_if_missing"))
    expected_current = [str(x) for x in req.get("expected_current_content_ids") or []]
    final_content_ids = [str(x) for x in req.get("final_content_ids") or []]

    playlist, created, current = resolve_target_playlist(
        db, target_id, target_name, target_parent_id, create_if_missing
    )

    if current != expected_current:
        raise RuntimeError(f"Target playlist {target_name!r} membership changed since plan generation")

    missing_content = []
    for content_id in final_content_ids:
        if db.get_content(ID=content_id) is None:
            missing_content.append(content_id)
    if missing_content:
        raise RuntimeError(f"Planned content IDs no longer exist: {', '.join(missing_content)}")

    existing_songs = (
        db.get_playlist_songs(PlaylistID=str(playlist.ID))
        .order_by(tables.DjmdSongPlaylist.TrackNo)
        .all()
    )
    for song in existing_songs:
        db.delete(song)
    db.flush()

    for content_id in final_content_ids:
        db.add_to_playlist(playlist, content_id)

    playlist.updated_at = datetime.datetime.now()
    return playlist, created


def playlist_response(db: Rekordbox6Database, playlist, created: bool) -> Dict[str, Any]:
    """Report a playlist's contents. Call only after commit, so the caller's
    post-apply verification compares committed state and not session state."""
    final = playlist_content_ids(db, str(playlist.ID))
    return {
        "playlist_id": str(playlist.ID),
        "playlist_name": str(playlist.Name or ""),
        "parent_id": str(playlist.ParentID or ""),
        "final_content_ids": final,
        "final_track_count": len(final),
        "created_playlist": created,
    }


def apply_playlist(req: Dict[str, Any]) -> Dict[str, Any]:
    db_dir = Path(str(req["db_dir"])).expanduser().resolve()
    db_path = db_dir / "master.db"

    db = Rekordbox6Database(path=db_path, db_dir=db_dir)
    try:
        playlist, created = rewrite_playlist(db, req)
        db.commit(autoinc=True)
        return playlist_response(db, playlist, created)
    except Exception:
        db.rollback()
        raise
    finally:
        db.close()


def resolve_folder(db: Rekordbox6Database, folder_id: str, folder_name: str, create_if_missing: bool):
    created = False
    folder = None
    if folder_id:
        folder = db.get_playlist(ID=folder_id)
        if folder is None:
            raise RuntimeError(f"Rekordbox folder ID {folder_id!r} not found")
    else:
        matches = [row for row in db.get_playlist(Name=folder_name).all() if str(row.ParentID or "") == "root"]
        if len(matches) > 1:
            raise RuntimeError(f"Multiple root Rekordbox folders named {folder_name!r}; use folder ID")
        folder = matches[0] if matches else None
    if folder is None:
        if not create_if_missing:
            raise RuntimeError(f"Rekordbox folder {folder_name!r} not found")
        folder = db.create_playlist_folder(folder_name)
        created = True
    if int(folder.Attribute or 0) != 1:
        raise RuntimeError(f"Target {folder.Name!r} is not a Rekordbox folder")
    return folder, created


def apply_batch(req: Dict[str, Any]) -> Dict[str, Any]:
    db_dir = Path(str(req["db_dir"])).expanduser().resolve()
    db_path = db_dir / "master.db"
    folder_id = str(req.get("target_folder_id") or "")
    folder_name = str(req.get("target_folder_name") or "")
    create_folder = bool(req.get("create_folder_if_missing"))
    operations = list(req.get("operations") or [])

    db = Rekordbox6Database(path=db_path, db_dir=db_dir)
    created_folder = False
    try:
        folder, created_folder = resolve_folder(db, folder_id, folder_name, create_folder)
        applied = []
        for op in operations:
            op["target_parent_id"] = str(folder.ID)
            applied.append(rewrite_playlist(db, op))
        db.commit(autoinc=True)
        # Read every playlist back only after the commit so the reported order is
        # what actually landed in the database.
        responses = [playlist_response(db, playlist, created) for playlist, created in applied]
        return {
            "folder_id": str(folder.ID),
            "folder_name": str(folder.Name or ""),
            "created_folder": created_folder,
            "responses": responses,
            "final_playlist_count": len(responses),
            "final_track_count": sum(int(row.get("final_track_count") or 0) for row in responses),
        }
    except Exception:
        db.rollback()
        raise
    finally:
        db.close()


TIMESTAMP_FORMAT = "%Y-%m-%dT%H:%M:%S"


def format_utc(value) -> str:
    """created_at as UTC with milliseconds, the precision Rekordbox stores.

    pyrekordbox hands back naive local datetimes, so astimezone() reads them
    as local time before converting.
    """
    if value is None:
        return ""
    utc = value.astimezone(datetime.timezone.utc)
    return utc.strftime(TIMESTAMP_FORMAT) + ".%03dZ" % (utc.microsecond // 1000)


def parse_utc(raw: str) -> datetime.datetime:
    text = str(raw).strip()
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    value = datetime.datetime.fromisoformat(text)
    if value.tzinfo is None:
        raise RuntimeError(f"timestamp {raw!r} has no time zone")
    if value.microsecond < 1000:
        # pyrekordbox writes milliseconds only and drops the seconds entirely
        # when the fraction is zero; never hand it such a value.
        raise RuntimeError(f"timestamp {raw!r} needs a non-zero millisecond fraction")
    return value


def nfc(value: str) -> str:
    return unicodedata.normalize("NFC", value)


def source_path_key(path: str) -> str:
    # Resolve aliases even when the file is gone but its parent still exists.
    return nfc(os.path.realpath(path) if os.path.isabs(path) else path)


def source_folder_prefix(req: Dict[str, Any]) -> str:
    folder = str(req.get("folder_prefix") or "")
    if not folder:
        raise RuntimeError("folder_prefix is required")
    return source_path_key(folder).rstrip("/") + "/"


def source_import_inspect(req: Dict[str, Any]) -> Dict[str, Any]:
    db_dir = Path(str(req["db_dir"])).expanduser().resolve()
    prefix = source_folder_prefix(req)
    db = Rekordbox6Database(path=db_dir / "master.db", db_dir=db_dir)
    try:
        # Stored paths can use a symlink alias or a different Unicode form.
        rows = [
            row
            for row in db.get_content().order_by(tables.DjmdContent.ID).all()
            if source_path_key(str(row.FolderPath or "")).startswith(prefix)
        ]
        contents = [
            {
                "id": str(row.ID),
                "folder_path": str(row.FolderPath or ""),
                "created_at": format_utc(row.created_at),
                "stock_date": str(row.StockDate or ""),
            }
            for row in rows
        ]
        return {"contents": contents}
    finally:
        db.close()


def lookup_or_add(db: Rekordbox6Database, table, adder, name: str, cache: Dict[str, str]) -> str:
    name = (name or "").strip()
    if not name:
        return None
    key = f"{table.__tablename__}:{name}"
    if key in cache:
        return cache[key]
    row = db.query(table).filter_by(Name=name).order_by(table.ID).first()
    value = str(row.ID) if row is not None else str(adder(name).ID)
    cache[key] = value
    return value


def import_content(db: Rekordbox6Database, row: Dict[str, Any], cache: Dict[str, str]):
    path = Path(str(row["path"]))
    if not path.is_file():
        raise RuntimeError(f"file to import no longer exists: {path}")
    tags = row.get("tags") or {}
    kwargs: Dict[str, Any] = {
        "Title": str(tags.get("title") or path.stem),
        "FileNameS": "",
        "Commnt": str(tags.get("comment") or ""),
        "Length": int(tags.get("duration_sec") or 0),
        "BitRate": int(tags.get("bit_rate_kbps") or 0),
        "BitDepth": int(tags.get("bit_depth") or 0),
        "SampleRate": int(tags.get("sample_rate") or 0),
        "ReleaseYear": int(tags.get("year") or 0),
        "TrackNo": int(tags.get("track_no") or 0),
        "DiscNo": int(tags.get("disc_no") or 0),
        "Rating": 0,
        "ColorID": 0,
        "DJPlayCount": 0,
    }
    artist_id = lookup_or_add(db, tables.DjmdArtist, db.add_artist, tags.get("artist"), cache)
    if artist_id:
        kwargs["ArtistID"] = artist_id
    album_id = lookup_or_add(db, tables.DjmdAlbum, db.add_album, tags.get("album"), cache)
    if album_id:
        kwargs["AlbumID"] = album_id
    genre_id = lookup_or_add(db, tables.DjmdGenre, db.add_genre, tags.get("genre"), cache)
    if genre_id:
        kwargs["GenreID"] = genre_id
    label_id = lookup_or_add(db, tables.DjmdLabel, db.add_label, tags.get("label"), cache)
    if label_id:
        kwargs["LabelID"] = label_id
    return create_content(db, path, **kwargs)


def create_content(db: Rekordbox6Database, path: Path, **kwargs: Any):
    """Rekordbox6Database.add_content with text primary keys.

    pyrekordbox's add_content stores integer IDs while rows loaded from the
    database have text IDs; SQLAlchemy then cannot order a flush that updates
    both kinds (commit's USN bump touches every changed row). Rekordbox itself
    writes text, so the new row does too. Otherwise this matches add_content.
    """
    path_string = str(path)
    file_type_name = path.suffix.lstrip(".").upper()
    file_type = getattr(tables.FileType, file_type_name, None)
    if file_type is None:
        raise RuntimeError(f"unsupported audio file type: {path.suffix}")
    content_id = str(db.generate_unused_id(tables.DjmdContent))
    file_id = str(db.generate_unused_id(tables.DjmdContent, id_field_name="rb_file_id"))
    content_link = db.get_menu_items(Name="TRACK").one()
    device = db.get_device().first()
    today = datetime.date.today()
    content = tables.DjmdContent.create(
        ID=content_id,
        UUID=str(uuid.uuid4()),
        ContentLink=content_link.rb_local_usn,
        DateCreated=today,
        DeviceID=device.ID,
        FileNameL=path.name,
        FileSize=path.stat().st_size,
        FileType=file_type.value,
        FolderPath=path_string,
        HotCueAutoLoad="on",
        MasterDBID=device.MasterDBID,
        MasterSongID=content_id,
        StockDate=today,
        rb_file_id=file_id,
        **kwargs,
    )
    db.add(content)
    db.flush()
    return content


def source_import_apply(req: Dict[str, Any]) -> Dict[str, Any]:
    """Import new files and rewrite Date Added in one transaction.

    Every row carries the state it was planned against: an update names the
    content ID, its path and the created_at it had; an import names a path the
    collection must not hold yet. Any drift aborts the whole batch.
    """
    db_dir = Path(str(req["db_dir"])).expanduser().resolve()
    rows = list(req.get("rows") or [])
    db = Rekordbox6Database(path=db_dir / "master.db", db_dir=db_dir)
    cache: Dict[str, str] = {}
    try:
        for row in rows:
            if row.get("action") not in ("import", "update"):
                raise RuntimeError(f"unsupported source import action: {row.get('action')!r}")
            parse_utc(row["created_at"])

        if req.get("append_only"):
            if any(row["action"] != "import" for row in rows):
                raise RuntimeError("append-only source import cannot update existing rows")
            prefix = source_folder_prefix(req)
            if prefix == "/":
                raise RuntimeError("append-only source import needs a folder_prefix below the filesystem root")
            existing = [
                content for content in db.get_content().all()
                if source_path_key(str(content.FolderPath or "")).startswith(prefix)
            ]
            latest = max((format_utc(content.created_at) for content in existing), default="")
            previous = datetime.datetime.fromisoformat(latest.replace("Z", "+00:00")) if latest else None
            for row in rows:
                if not source_path_key(str(row["path"])).startswith(prefix):
                    raise RuntimeError("append-only import path is outside the source folder")
                created = parse_utc(row["created_at"])
                if previous is not None and created <= previous:
                    raise RuntimeError("append-only Date Added must follow every existing source row")
                previous = created

        import_rows = [row for row in rows if row["action"] == "import"]
        if import_rows:
            existing_paths = {
                source_path_key(str(folder or ""))
                for (folder,) in db.query(tables.DjmdContent.FolderPath).all()
            }
            for row in import_rows:
                path_key = source_path_key(str(row["path"]))
                if path_key in existing_paths:
                    raise RuntimeError(f"{row['path']} is already in the Rekordbox collection")
                existing_paths.add(path_key)

        # pyrekordbox gives new rows integer primary keys while loaded rows
        # carry text keys, and SQLAlchemy cannot flush a mix of both for one
        # table. Imports therefore go first and are flushed on their own;
        # existing rows are only touched afterwards.
        content_ids: Dict[int, str] = {}
        for index, row in enumerate(rows):
            if row["action"] != "import":
                continue
            content = import_content(db, row, cache)
            content.created_at = parse_utc(row["created_at"])
            content.StockDate = str(row["stock_date"])
            db.flush()
            content_ids[index] = str(content.ID)

        for index, row in enumerate(rows):
            if row["action"] != "update":
                continue
            content = db.get_content(ID=str(row["content_id"]))
            if content is None:
                raise RuntimeError(f"Rekordbox track {row['content_id']} no longer exists")
            if nfc(str(content.FolderPath or "")) != nfc(str(row["path"])):
                raise RuntimeError(f"Rekordbox track {row['content_id']} moved since planning")
            if format_utc(content.created_at) != str(row.get("expected_created_at") or ""):
                raise RuntimeError(f"Rekordbox track {row['content_id']} Date Added changed since planning")
            content.created_at = parse_utc(row["created_at"])
            content.StockDate = str(row["stock_date"])
            content_ids[index] = str(content.ID)
        db.flush()
        db.commit(autoinc=True)

        results = []
        for index, row in enumerate(rows):
            key = str(row.get("key") or "")
            stored = db.get_content(ID=content_ids[index])
            if stored is None:
                raise RuntimeError(f"Rekordbox track {content_ids[index]} missing after commit")
            results.append(
                {
                    "key": key,
                    "content_id": str(stored.ID),
                    "path": str(stored.FolderPath or ""),
                    "created_at": format_utc(stored.created_at),
                    "stock_date": str(stored.StockDate or ""),
                }
            )
        return {"rows": results}
    except Exception:
        db.rollback()
        raise
    finally:
        db.close()


def main() -> int:
    try:
        request = json.load(sys.stdin)
        op = request.get("op")
        if op == "inspect":
            response = inspect_db(str(request["db_dir"]))
        elif op == "apply":
            response = apply_playlist(request["request"])
        elif op == "apply_batch":
            response = apply_batch(request["request"])
        elif op == "source_import_inspect":
            response = source_import_inspect(request["request"])
        elif op == "source_import_apply":
            response = source_import_apply(request["request"])
        else:
            raise RuntimeError(f"Unsupported helper operation: {op!r}")
        print(json.dumps(response, ensure_ascii=False, separators=(",", ":")))
        return 0
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
