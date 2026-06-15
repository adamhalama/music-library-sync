#!/usr/bin/env python3
from __future__ import annotations

import datetime
import json
import sys
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


def apply_playlist(req: Dict[str, Any]) -> Dict[str, Any]:
    db_dir = Path(str(req["db_dir"])).expanduser().resolve()
    db_path = db_dir / "master.db"
    target_id = str(req.get("target_playlist_id") or "")
    target_name = str(req.get("target_playlist_name") or "")
    create_if_missing = bool(req.get("create_playlist_if_missing"))
    target_parent_id = str(req.get("target_parent_id") or "")
    expected_current = [str(x) for x in req.get("expected_current_content_ids") or []]
    final_content_ids = [str(x) for x in req.get("final_content_ids") or []]

    db = Rekordbox6Database(path=db_path, db_dir=db_dir)
    created = False
    try:
        if target_id:
            playlist = db.get_playlist(ID=target_id)
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
            parent = target_parent_id or None
            playlist = db.create_playlist(target_name, parent=parent)
            created = True
            current = []
        else:
            if int(playlist.Attribute or 0) != 0:
                raise RuntimeError(f"Target playlist {playlist.Name!r} is not a normal playlist")
            current = playlist_content_ids(db, str(playlist.ID))

        if current != expected_current:
            raise RuntimeError("Target playlist membership changed since plan generation")

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
        db.commit(autoinc=True)

        final = playlist_content_ids(db, str(playlist.ID))
        return {
            "playlist_id": str(playlist.ID),
            "playlist_name": str(playlist.Name or ""),
            "parent_id": str(playlist.ParentID or ""),
            "final_content_ids": final,
            "final_track_count": len(final),
            "created_playlist": created,
        }
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


def apply_playlist_in_open_db(db: Rekordbox6Database, req: Dict[str, Any]) -> Dict[str, Any]:
    target_id = str(req.get("target_playlist_id") or "")
    target_name = str(req.get("target_playlist_name") or "")
    target_parent_id = str(req.get("target_parent_id") or "")
    create_if_missing = bool(req.get("create_playlist_if_missing"))
    expected_current = [str(x) for x in req.get("expected_current_content_ids") or []]
    final_content_ids = [str(x) for x in req.get("final_content_ids") or []]

    if target_id:
        playlist = db.get_playlist(ID=target_id)
    else:
        matches = db.get_playlist(Name=target_name).all()
        if target_parent_id:
            matches = [row for row in matches if str(row.ParentID or "") == target_parent_id]
        if len(matches) > 1:
            raise RuntimeError(f"Multiple Rekordbox playlists named {target_name!r}; use playlist ID")
        playlist = matches[0] if matches else None

    created = False
    if playlist is None:
        if not create_if_missing:
            raise RuntimeError(f"Rekordbox playlist {target_name!r} not found")
        playlist = db.create_playlist(target_name, parent=(target_parent_id or None))
        created = True
        current = []
    else:
        if int(playlist.Attribute or 0) != 0:
            raise RuntimeError(f"Target playlist {playlist.Name!r} is not a normal playlist")
        if target_parent_id and str(playlist.ParentID or "") != target_parent_id:
            raise RuntimeError(f"Target playlist {playlist.Name!r} is not under the planned folder")
        current = playlist_content_ids(db, str(playlist.ID))

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
    final = playlist_content_ids(db, str(playlist.ID))
    return {
        "playlist_id": str(playlist.ID),
        "playlist_name": str(playlist.Name or ""),
        "parent_id": str(playlist.ParentID or ""),
        "final_content_ids": final,
        "final_track_count": len(final),
        "created_playlist": created,
    }


def apply_batch(req: Dict[str, Any]) -> Dict[str, Any]:
    db_dir = Path(str(req["db_dir"])).expanduser().resolve()
    db_path = db_dir / "master.db"
    folder_id = str(req.get("target_folder_id") or "")
    folder_name = str(req.get("target_folder_name") or "")
    create_folder = bool(req.get("create_folder_if_missing"))
    create_playlist = bool(req.get("create_playlist_if_missing"))
    operations = list(req.get("operations") or [])

    db = Rekordbox6Database(path=db_path, db_dir=db_dir)
    created_folder = False
    try:
        folder, created_folder = resolve_folder(db, folder_id, folder_name, create_folder)
        responses = []
        for op in operations:
            op["target_parent_id"] = str(folder.ID)
            if "create_playlist_if_missing" not in op:
                op["create_playlist_if_missing"] = create_playlist
            responses.append(apply_playlist_in_open_db(db, op))
        db.commit(autoinc=True)
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
        else:
            raise RuntimeError(f"Unsupported helper operation: {op!r}")
        print(json.dumps(response, ensure_ascii=False, separators=(",", ":")))
        return 0
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
