#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

RELEASE_DIR="${REPO_ROOT}/release"
mkdir -p "${RELEASE_DIR}"

echo "Running tests..."
go test ./...

echo "Running vet..."
go vet ./...

echo "Building release matrix..."
GOOS=windows GOARCH=amd64 go build -o "${RELEASE_DIR}/snispf_windows_amd64.exe" ./cmd/snispf
GOOS=linux GOARCH=amd64 go build -o "${RELEASE_DIR}/snispf_linux_amd64" ./cmd/snispf
GOOS=linux GOARCH=arm64 go build -o "${RELEASE_DIR}/snispf_linux_arm64" ./cmd/snispf

copy_windivert_if_present() {
	local src="$1"
	local base
	base="$(basename "${src}")"
	if [[ -f "${src}" ]]; then
		cp -f "${src}" "${RELEASE_DIR}/${base}"
	fi
}

# Preferred source: resources/WinDivert (supports nested version folders).
if [[ -d "${REPO_ROOT}/resources/WinDivert" ]]; then
	for n in WinDivert.dll WinDivert64.sys; do
		f="$(find "${REPO_ROOT}/resources/WinDivert" -type f -name "${n}" 2>/dev/null | head -n 1 || true)"
		if [[ -n "${f}" ]]; then
			copy_windivert_if_present "${f}"
		fi
	done
fi

# Backward-compatible fallback locations.
copy_windivert_if_present "${REPO_ROOT}/WinDivert.dll"
copy_windivert_if_present "${REPO_ROOT}/WinDivert64.sys"
copy_windivert_if_present "${REPO_ROOT}/third_party/WinDivert/WinDivert.dll"
copy_windivert_if_present "${REPO_ROOT}/third_party/WinDivert/WinDivert64.sys"

if [[ ! -f "${RELEASE_DIR}/WinDivert.dll" || ! -f "${RELEASE_DIR}/WinDivert64.sys" ]]; then
	echo "Warning: WinDivert runtime files not fully found. Windows wrong_seq runtime requires WinDivert.dll and WinDivert64.sys beside the exe." >&2
fi

echo "Generating checksums..."
SHA_TOOL=""
if command -v sha256sum >/dev/null 2>&1; then
	SHA_TOOL="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	SHA_TOOL="shasum -a 256"
else
	echo "No SHA256 tool found (need sha256sum or shasum)" >&2
	exit 1
fi

CHECKSUMS_FILE="${RELEASE_DIR}/checksums.txt"
RELEASE_MANIFEST="${RELEASE_DIR}/release_manifest.json"

ARTIFACTS=(
	"snispf_windows_amd64.exe"
	"snispf_linux_amd64"
	"snispf_linux_arm64"
)

if [[ -f "${RELEASE_DIR}/WinDivert.dll" ]]; then
	ARTIFACTS+=("WinDivert.dll")
fi
if [[ -f "${RELEASE_DIR}/WinDivert64.sys" ]]; then
	ARTIFACTS+=("WinDivert64.sys")
fi

pushd "${RELEASE_DIR}" >/dev/null
eval "${SHA_TOOL} ${ARTIFACTS[*]}" > checksums.txt

python3 - <<'PY'
import json
import pathlib
from datetime import datetime, timezone

release = pathlib.Path(".")
entries = []
for line in (release / "checksums.txt").read_text(encoding="utf-8").splitlines():
    line = line.strip()
    if not line:
        continue
    sha, name = line.split(maxsplit=1)
    p = release / name
    entries.append({
        "name": name,
        "sha256": sha.lower(),
        "bytes": p.stat().st_size,
    })

manifest = {
    "project": "snispf-core",
    "generated_at_utc": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "artifacts": entries,
}
(release / "release_manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
PY
popd >/dev/null

echo "Release artifacts:"
for a in "${ARTIFACTS[@]}"; do
	ls -lh "${RELEASE_DIR}/${a}"
done
echo "Generated metadata:"
ls -lh "${CHECKSUMS_FILE}" "${RELEASE_MANIFEST}"
