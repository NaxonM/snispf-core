#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

RELEASE_DIR="${REPO_ROOT}/release"
mkdir -p "${RELEASE_DIR}"
DEFAULT_CONFIG_PATH="${RELEASE_DIR}/default_config.json"

WINDOWS_BUNDLE_DIR="${RELEASE_DIR}/snispf_windows_amd64_bundle"
LINUX_AMD64_BUNDLE_DIR="${RELEASE_DIR}/snispf_linux_amd64_bundle"
LINUX_ARM64_BUNDLE_DIR="${RELEASE_DIR}/snispf_linux_arm64_bundle"

echo "Running tests..."
go test ./...

echo "Running vet..."
go vet ./...

echo "Generating default config for bundles..."
go run ./cmd/snispf --generate-config "${DEFAULT_CONFIG_PATH}"

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

echo "Packaging release bundles..."
rm -rf "${WINDOWS_BUNDLE_DIR}" "${LINUX_AMD64_BUNDLE_DIR}" "${LINUX_ARM64_BUNDLE_DIR}"

create_bundle_layout() {
	local bundle_dir="$1"
	local platform="$2"
	mkdir -p "${bundle_dir}/configs/examples"

	cp -f "${DEFAULT_CONFIG_PATH}" "${bundle_dir}/config.json"

	if [[ -d "${REPO_ROOT}/configs/examples" ]]; then
		cp -f "${REPO_ROOT}/configs/examples/"*.json "${bundle_dir}/configs/examples/" 2>/dev/null || true
	fi

	cat > "${bundle_dir}/README_BUNDLE.txt" <<'EOF'
SNISPF bundled release

Contents:
- Core binary for this platform
- config.json starter template
- configs/examples with additional profiles

Quick start:
1) Edit config.json for your upstream endpoint and SNI.
2) Run the binary with --config ./config.json.
3) On Windows strict wrong_seq mode requires WinDivert.dll and WinDivert64.sys next to the exe.
EOF

	if [[ "${platform}" == "linux" ]]; then
		cp -f "${REPO_ROOT}/scripts/install_linux_service.sh" "${bundle_dir}/install_linux_service.sh"
		cp -f "${REPO_ROOT}/scripts/snispf.service" "${bundle_dir}/snispf.service"
		chmod +x "${bundle_dir}/install_linux_service.sh"
	fi
}

create_bundle_layout "${WINDOWS_BUNDLE_DIR}" "windows"
create_bundle_layout "${LINUX_AMD64_BUNDLE_DIR}" "linux"
create_bundle_layout "${LINUX_ARM64_BUNDLE_DIR}" "linux"

cp -f "${RELEASE_DIR}/snispf_windows_amd64.exe" "${WINDOWS_BUNDLE_DIR}/snispf_windows_amd64.exe"
cp -f "${RELEASE_DIR}/snispf_linux_amd64" "${LINUX_AMD64_BUNDLE_DIR}/snispf_linux_amd64"
cp -f "${RELEASE_DIR}/snispf_linux_arm64" "${LINUX_ARM64_BUNDLE_DIR}/snispf_linux_arm64"

if [[ -f "${RELEASE_DIR}/WinDivert.dll" ]]; then
	cp -f "${RELEASE_DIR}/WinDivert.dll" "${WINDOWS_BUNDLE_DIR}/WinDivert.dll"
fi
if [[ -f "${RELEASE_DIR}/WinDivert64.sys" ]]; then
	cp -f "${RELEASE_DIR}/WinDivert64.sys" "${WINDOWS_BUNDLE_DIR}/WinDivert64.sys"
fi

pushd "${RELEASE_DIR}" >/dev/null
if command -v zip >/dev/null 2>&1; then
	rm -f snispf_windows_amd64_bundle.zip
	zip -rq snispf_windows_amd64_bundle.zip "$(basename "${WINDOWS_BUNDLE_DIR}")"
elif command -v python3 >/dev/null 2>&1; then
	python3 - <<'PY'
import pathlib
import zipfile

release = pathlib.Path('.')
bundle = release / 'snispf_windows_amd64_bundle'
archive = release / 'snispf_windows_amd64_bundle.zip'
if archive.exists():
    archive.unlink()
with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as zf:
    for p in bundle.rglob('*'):
        if p.is_file():
            zf.write(p, p.relative_to(release))
PY
else
	echo "Neither zip nor python3 is available to create Windows bundle archive" >&2
	exit 1
fi

tar -czf snispf_linux_amd64_bundle.tar.gz "$(basename "${LINUX_AMD64_BUNDLE_DIR}")"
tar -czf snispf_linux_arm64_bundle.tar.gz "$(basename "${LINUX_ARM64_BUNDLE_DIR}")"
popd >/dev/null

rm -rf "${WINDOWS_BUNDLE_DIR}" "${LINUX_AMD64_BUNDLE_DIR}" "${LINUX_ARM64_BUNDLE_DIR}"

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
	"snispf_windows_amd64_bundle.zip"
	"snispf_linux_amd64_bundle.tar.gz"
	"snispf_linux_arm64_bundle.tar.gz"
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
