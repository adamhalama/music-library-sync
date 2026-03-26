#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "usage: $0 [output-path]" >&2
  exit 2
fi

output_path="${1:-NOTICES}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

gpl3_path="${tmpdir}/GPL-3.0.txt"
gpl2_path="${tmpdir}/GPL-2.0.txt"
scdl_pyproject_path="${tmpdir}/scdl-pyproject.toml"
scdl_authors_path="${tmpdir}/scdl-AUTHORS"

curl -fsSL "https://www.gnu.org/licenses/gpl-3.0.txt" -o "$gpl3_path"
curl -fsSL "https://www.gnu.org/licenses/old-licenses/gpl-2.0.txt" -o "$gpl2_path"
curl -fsSL "https://raw.githubusercontent.com/scdl-org/scdl/master/pyproject.toml" -o "$scdl_pyproject_path"
curl -fsSL "https://raw.githubusercontent.com/scdl-org/scdl/master/AUTHORS" -o "$scdl_authors_path"

scdl_project_authors="$(python3 - <<'PY' "$scdl_pyproject_path"
import re
import sys

text = open(sys.argv[1], "r", encoding="utf-8").read()
match = re.search(r"authors=\[(.*?)\]", text, re.S)
if not match:
    raise SystemExit("unable to parse scdl authors from pyproject.toml")
names = re.findall(r'name\s*=\s*"([^"]+)"', match.group(1))
print(", ".join(names))
PY
)"

scdl_main_developers="$(python3 - <<'PY' "$scdl_authors_path"
import sys

names = []
capture = False
for raw_line in open(sys.argv[1], "r", encoding="utf-8"):
    line = raw_line.strip()
    if line == "Main Developers":
        capture = True
        continue
    if capture and not line:
        continue
    if capture and set(line) == {"="}:
        continue
    if capture and line == "Contributors":
        break
    if capture and line.startswith("* "):
        names.append(line[2:])

print(", ".join(names))
PY
)"

python3 - <<'PY' "$output_path" "$gpl3_path" "$gpl2_path" "$scdl_project_authors" "$scdl_main_developers"
import pathlib
import sys

output_path = pathlib.Path(sys.argv[1])
gpl3_path = pathlib.Path(sys.argv[2])
gpl2_path = pathlib.Path(sys.argv[3])
scdl_project_authors = sys.argv[4]
scdl_main_developers = sys.argv[5]

gpl3_text = gpl3_path.read_text(encoding="utf-8").rstrip()
gpl2_text = gpl2_path.read_text(encoding="utf-8").rstrip()

content = f"""UDL Third-Party Notices
=======================

This repository's macOS release artifacts may bundle unmodified copies of:
- scdl
- yt-dlp

These notices are generated from official upstream and GNU license sources so
the bundled tarballs carry the required license texts alongside the binaries.

Release gate reminders
----------------------
- Verify the exact upstream copyright holder names and years before publishing.
- Review and accept the GPL redistribution obligations before setting
  ALLOW_BUNDLED_TOOL_REDISTRIBUTION=true.

yt-dlp
------
Upstream source repository: https://github.com/yt-dlp/yt-dlp
Upstream source distribution license: Unlicense

The upstream project describes its PyInstaller-bundled executables as including
GPLv3+ licensed code, so the prebuilt executable form distributed in UDL's
bundle should be treated as GPLv3+.

Included license text for bundled executable distribution:

{gpl3_text}

scdl
----
Upstream source repository: https://github.com/scdl-org/scdl
Upstream license: GPL-2.0

Upstream authorship references used for release review:
- pyproject.toml authors: {scdl_project_authors}
- AUTHORS main developers: {scdl_main_developers}

Verify the exact upstream copyright notice and year range before release.

Included license text:

{gpl2_text}
"""

output_path.write_text(content, encoding="utf-8")
PY
