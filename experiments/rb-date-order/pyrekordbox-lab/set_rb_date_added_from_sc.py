#!/usr/bin/env python3
"""Rewrite Rekordbox Date Added (djmdContent.created_at) from SoundCloud likes order.

The script is intentionally conservative:
- dry-run by default
- run manifests are always written
- commit uses pyrekordbox commit guard (fails if Rekordbox is running)
"""

from __future__ import annotations

import argparse
import csv
import json
import shutil
import subprocess
import sys
import unicodedata
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path
from typing import Dict, Iterable, List, Optional, Tuple

from pyrekordbox import Rekordbox6Database
from pyrekordbox.db6 import tables


@dataclass(frozen=True)
class LikeEntry:
    index: int
    sc_id: str
    title: str
    url: str


@dataclass
class Match:
    row: tables.DjmdContent
    file_path: str
    local_title: str
    sc_index: int
    sc_id: str
    sc_title: str
    sc_url: str
    match_mode: str
    old_created_at: Optional[datetime]
    new_created_at: Optional[datetime] = None


@dataclass
class Unmatched:
    file_path: str
    local_title: str
    reason: str


def normalize_title(raw: str) -> str:
    text = unicodedata.normalize("NFKC", raw or "")
    text = text.replace("：", ":")
    text = " ".join(text.lower().strip().split())
    return text


def parse_anchor_datetime(raw: str) -> datetime:
    candidates = [
        "%Y-%m-%dT%H:%M:%S",
        "%Y-%m-%d %H:%M:%S",
        "%Y-%m-%dT%H:%M",
        "%Y-%m-%d %H:%M",
    ]
    for fmt in candidates:
        try:
            return datetime.strptime(raw, fmt)
        except ValueError:
            continue
    raise ValueError(
        "Invalid --anchor-datetime format. Use one of: "
        "YYYY-MM-DDTHH:MM:SS, YYYY-MM-DD HH:MM:SS, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM"
    )


def ensure_millis_precision(dt: datetime) -> datetime:
    # pyrekordbox's DateTime serializer keeps second precision only when fractional
    # seconds are present; keep a fixed .900 milliseconds suffix for deterministic
    # ordering at second granularity.
    if dt.microsecond == 0:
        return dt.replace(microsecond=900_000)
    return dt


def require_cmd(name: str) -> None:
    if shutil.which(name):
        return
    raise RuntimeError(f"Missing required command: {name}")


def fetch_likes(likes_url: str) -> List[LikeEntry]:
    require_cmd("yt-dlp")
    cmd = ["yt-dlp", "--flat-playlist", "-J", likes_url]
    proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if proc.returncode != 0:
        raise RuntimeError(
            f"yt-dlp failed ({proc.returncode}) while fetching likes URL '{likes_url}'.\n"
            f"stderr: {proc.stderr.strip()}"
        )
    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Failed to parse yt-dlp JSON output: {exc}") from exc

    entries = payload.get("entries") or []
    likes: List[LikeEntry] = []
    for i, entry in enumerate(entries, start=1):
        if not entry:
            continue
        title = (entry.get("title") or "").strip()
        if not title:
            continue
        sc_id = str(entry.get("id") or "")
        url = str(entry.get("url") or entry.get("webpage_url") or "")
        likes.append(LikeEntry(index=i, sc_id=sc_id, title=title, url=url))
    if not likes:
        raise RuntimeError(f"No likes entries found at: {likes_url}")
    return likes


def build_likes_maps(
    likes: Iterable[LikeEntry],
) -> Tuple[Dict[str, LikeEntry], Dict[str, LikeEntry]]:
    by_exact: Dict[str, LikeEntry] = {}
    by_norm: Dict[str, LikeEntry] = {}
    for item in likes:
        by_exact.setdefault(item.title, item)
        norm = normalize_title(item.title)
        if norm:
            by_norm.setdefault(norm, item)
    return by_exact, by_norm


def find_local_title(row: tables.DjmdContent) -> str:
    if row.Title:
        return str(row.Title)
    path = Path(str(row.FolderPath))
    return path.stem


def match_rows_to_likes(
    rows: Iterable[tables.DjmdContent], by_exact: Dict[str, LikeEntry], by_norm: Dict[str, LikeEntry]
) -> Tuple[List[Match], List[Unmatched]]:
    matched: List[Match] = []
    unmatched: List[Unmatched] = []
    for row in rows:
        file_path = str(row.FolderPath)
        local_title = find_local_title(row)
        basename_stem = Path(file_path).stem

        candidates = [local_title, basename_stem]
        item: Optional[LikeEntry] = None
        mode = ""

        for candidate in candidates:
            if candidate in by_exact:
                item = by_exact[candidate]
                mode = "exact"
                break

        if item is None:
            for candidate in candidates:
                norm = normalize_title(candidate)
                if norm and norm in by_norm:
                    item = by_norm[norm]
                    mode = "normalized"
                    break

        if item is None:
            unmatched.append(
                Unmatched(file_path=file_path, local_title=local_title, reason="not-found-in-likes")
            )
            continue

        matched.append(
            Match(
                row=row,
                file_path=file_path,
                local_title=local_title,
                sc_index=item.index,
                sc_id=item.sc_id,
                sc_title=item.title,
                sc_url=item.url,
                match_mode=mode,
                old_created_at=row.created_at,
            )
        )
    return matched, unmatched


def write_tsv(path: Path, header: List[str], rows: Iterable[List[str]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f, delimiter="\t")
        writer.writerow(header)
        for row in rows:
            writer.writerow(row)


def format_dt(dt: Optional[datetime]) -> str:
    if dt is None:
        return ""
    return dt.strftime("%Y-%m-%d %H:%M:%S.%f")


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Apply SoundCloud likes order to Rekordbox Date Added (created_at)."
    )
    parser.add_argument("--db-dir", required=True, help="Rekordbox DB directory containing master.db")
    parser.add_argument("--target-dir", required=True, help="Target track folder inside Rekordbox collection")
    parser.add_argument("--likes-url", required=True, help="SoundCloud likes URL")
    parser.add_argument(
        "--step-seconds",
        type=int,
        default=1,
        help="Seconds between tracks in assigned Date Added timeline (default: 1)",
    )
    parser.add_argument(
        "--anchor-datetime",
        default="",
        help="Top timestamp for newest liked track (local time), e.g. 2026-02-27T12:00:00. "
        "Default: current max created_at in matched rows",
    )
    parser.add_argument(
        "--use-like-index-offset",
        action="store_true",
        help="Use absolute SoundCloud index gaps for offsets instead of dense sequential offsets",
    )
    parser.add_argument(
        "--run-root",
        default="",
        help="Directory for run manifests (default: <script-dir>/runs)",
    )
    parser.add_argument(
        "--apply",
        action="store_true",
        help="Persist changes to master.db. Without this flag, script is dry-run.",
    )
    args = parser.parse_args(argv)

    db_dir = Path(args.db_dir).expanduser().resolve()
    target_dir = Path(args.target_dir).expanduser().resolve()
    run_root = (
        Path(args.run_root).expanduser().resolve()
        if args.run_root
        else (Path(__file__).resolve().parent / "runs")
    )
    run_id = datetime.now().strftime("%Y%m%d-%H%M%S")
    run_dir = run_root / run_id
    run_dir.mkdir(parents=True, exist_ok=True)

    run_meta_path = run_dir / "run_meta.txt"
    likes_tsv = run_dir / "sc_likes.tsv"
    matched_tsv = run_dir / "matched.tsv"
    unmatched_tsv = run_dir / "unmatched.tsv"
    plan_tsv = run_dir / "planned_updates.tsv"
    applied_tsv = run_dir / "applied_updates.tsv"

    if args.step_seconds < 1:
        print("--step-seconds must be >= 1", file=sys.stderr)
        return 2

    db_path = db_dir / "master.db"
    if not db_path.exists():
        print(f"master.db not found at: {db_path}", file=sys.stderr)
        return 2
    if not target_dir.exists():
        print(f"target-dir does not exist: {target_dir}", file=sys.stderr)
        return 2

    print("Fetching SoundCloud likes order...")
    likes = fetch_likes(args.likes_url)
    by_exact, by_norm = build_likes_maps(likes)
    write_tsv(
        likes_tsv,
        ["sc_index", "sc_id", "sc_title", "sc_url"],
        [[str(x.index), x.sc_id, x.title, x.url] for x in likes],
    )

    print("Opening Rekordbox DB...")
    db = Rekordbox6Database(path=db_path, db_dir=db_dir)
    target_prefix = f"{target_dir}/"
    rows = (
        db.get_content()
        .filter(tables.DjmdContent.FolderPath.like(f"{target_prefix}%"))
        .all()
    )
    if not rows:
        db.close()
        print(f"No djmdContent rows found under target-dir prefix: {target_prefix}", file=sys.stderr)
        return 3

    matched, unmatched = match_rows_to_likes(rows, by_exact, by_norm)
    write_tsv(
        matched_tsv,
        [
            "file_path",
            "local_title",
            "sc_index",
            "sc_id",
            "sc_title",
            "match_mode",
            "old_created_at",
        ],
        [
            [
                m.file_path,
                m.local_title,
                str(m.sc_index),
                m.sc_id,
                m.sc_title,
                m.match_mode,
                format_dt(m.old_created_at),
            ]
            for m in sorted(matched, key=lambda x: x.sc_index)
        ],
    )
    write_tsv(
        unmatched_tsv,
        ["file_path", "local_title", "reason"],
        [[u.file_path, u.local_title, u.reason] for u in unmatched],
    )

    if not matched:
        db.close()
        print("No tracks matched SoundCloud likes titles; nothing to do.", file=sys.stderr)
        return 3

    if args.anchor_datetime:
        anchor = parse_anchor_datetime(args.anchor_datetime)
    else:
        anchor = max(m.old_created_at for m in matched if m.old_created_at is not None)
        if anchor is None:
            anchor = datetime.now()
    anchor = ensure_millis_precision(anchor)

    matched_sorted = sorted(matched, key=lambda x: x.sc_index)
    for pos, m in enumerate(matched_sorted):
        if args.use_like_index_offset:
            offset_steps = m.sc_index - 1
        else:
            offset_steps = pos
        m.new_created_at = ensure_millis_precision(anchor - timedelta(seconds=offset_steps * args.step_seconds))
        m.row.created_at = m.new_created_at

    write_tsv(
        plan_tsv,
        [
            "file_path",
            "local_title",
            "sc_index",
            "old_created_at",
            "new_created_at",
            "match_mode",
        ],
        [
            [
                m.file_path,
                m.local_title,
                str(m.sc_index),
                format_dt(m.old_created_at),
                format_dt(m.new_created_at),
                m.match_mode,
            ]
            for m in matched_sorted
        ],
    )

    with run_meta_path.open("w", encoding="utf-8") as f:
        f.write(f"run_id={run_id}\n")
        f.write(f"db_dir={db_dir}\n")
        f.write(f"db_path={db_path}\n")
        f.write(f"target_dir={target_dir}\n")
        f.write(f"likes_url={args.likes_url}\n")
        f.write(f"step_seconds={args.step_seconds}\n")
        f.write(f"use_like_index_offset={int(bool(args.use_like_index_offset))}\n")
        f.write(f"anchor_datetime={format_dt(anchor)}\n")
        f.write(f"apply={int(bool(args.apply))}\n")
        f.write(f"matched={len(matched)}\n")
        f.write(f"unmatched={len(unmatched)}\n")
        f.write(f"started_at={datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n")

    if args.apply:
        print("Applying changes to master.db...")
        try:
            db.commit(autoinc=True)
        except RuntimeError as exc:
            db.rollback()
            db.close()
            print(
                "Commit blocked. Close Rekordbox and retry with --apply.\n"
                f"Reason: {exc}",
                file=sys.stderr,
            )
            return 4
    else:
        db.rollback()

    # Re-read to capture final stored values (including timezone-normalized strings)
    rows_after = (
        db.get_content()
        .filter(tables.DjmdContent.FolderPath.like(f"{target_prefix}%"))
        .all()
    )
    by_path = {str(r.FolderPath): r for r in rows_after}
    write_tsv(
        applied_tsv,
        ["file_path", "local_title", "sc_index", "new_created_at_planned", "created_at_in_db"],
        [
            [
                m.file_path,
                m.local_title,
                str(m.sc_index),
                format_dt(m.new_created_at),
                format_dt(by_path[m.file_path].created_at),
            ]
            for m in matched_sorted
            if m.file_path in by_path
        ],
    )

    db.close()

    print("Done.")
    print(f"run_dir={run_dir}")
    print(f"likes_tsv={likes_tsv}")
    print(f"matched_tsv={matched_tsv}")
    print(f"unmatched_tsv={unmatched_tsv}")
    print(f"planned_updates_tsv={plan_tsv}")
    print(f"applied_updates_tsv={applied_tsv}")
    if not args.apply:
        print("dry_run=1 (changes rolled back)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
