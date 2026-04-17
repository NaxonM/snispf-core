#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
CHECKSUMS_FILE="${RELEASE_DIR}/checksums.txt"
MANIFEST_FILE="${RELEASE_DIR}/release_manifest.json"

if [[ ! -f "${CHECKSUMS_FILE}" ]]; then
  echo "checksums.txt not found" >&2
  exit 1
fi
if [[ ! -f "${MANIFEST_FILE}" ]]; then
  echo "release_manifest.json not found" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required for manifest validation" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  SHA_TOOL="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA_TOOL="shasum -a 256"
else
  echo "No SHA256 tool found" >&2
  exit 1
fi

while read -r hash name; do
  [[ -z "${hash}" || -z "${name}" ]] && continue
  artifact="${RELEASE_DIR}/${name}"
  if [[ ! -f "${artifact}" ]]; then
    echo "artifact missing: ${name}" >&2
    exit 1
  fi
  actual="$(eval "${SHA_TOOL} \"${artifact}\"" | awk '{print tolower($1)}')"
  expected="$(echo "${hash}" | tr '[:upper:]' '[:lower:]')"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "checksum mismatch: ${name}" >&2
    exit 1
  fi
done < "${CHECKSUMS_FILE}"

export RELEASE_DIR
python3 - <<'PY'
import json
import pathlib
import hashlib
import os

release = pathlib.Path(os.environ["RELEASE_DIR"])
manifest = json.loads((release / "release_manifest.json").read_text(encoding="utf-8"))
checksums = {}
for line in (release / "checksums.txt").read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if not line:
        continue
    h, name = line.split(maxsplit=1)
    checksums[name] = h.lower()

if manifest.get("project") != "snispf-core":
    raise SystemExit("manifest project mismatch")

artifacts = {a["name"]: a for a in manifest.get("artifacts", [])}
for name, expected in checksums.items():
    p = release / name
    if not p.exists():
        raise SystemExit(f"manifest check missing artifact: {name}")
    actual = hashlib.sha256(p.read_bytes()).hexdigest()
    if actual != expected:
        raise SystemExit(f"manifest check hash mismatch: {name}")
    a = artifacts.get(name)
    if not a:
        raise SystemExit(f"manifest missing artifact entry: {name}")
    if a.get("sha256", "").lower() != actual:
        raise SystemExit(f"manifest sha mismatch: {name}")
    if int(a.get("bytes", -1)) != p.stat().st_size:
        raise SystemExit(f"manifest size mismatch: {name}")
print("Release verification passed.")
PY
