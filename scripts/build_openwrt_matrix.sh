#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

cd "${REPO_ROOT}"

OUT_DIR="${REPO_ROOT}/release/openwrt"
mkdir -p "${OUT_DIR}"

# Ensure host tests/vet are not affected by caller-provided cross-compile env vars.
unset GOOS GOARCH GOARM GOMIPS CGO_ENABLED

echo "Running tests..."
go test ./...

echo "Running vet..."
go vet ./...

build_one() {
  local goarch="$1"
  local out="$2"
  local goarm="${3:-}"
  local gomips="${4:-}"

  echo " - linux/${goarch} -> ${out}"

  local -a env_vars=("CGO_ENABLED=0" "GOOS=linux" "GOARCH=${goarch}")
  if [[ -n "${goarm}" ]]; then
    env_vars+=("GOARM=${goarm}")
  fi
  if [[ -n "${gomips}" ]]; then
    env_vars+=("GOMIPS=${gomips}")
  fi

  env "${env_vars[@]}" go build -trimpath -ldflags "-s -w -buildid=" -o "${OUT_DIR}/${out}" ./cmd/snispf
}

echo "Building OpenWrt matrix..."
# Primary target for ipq40xx/generic devices (for example Linksys EA8300).
build_one "arm" "snispf_openwrt_armv7" "7"
# Optional fallback for older ARM devices.
build_one "arm" "snispf_openwrt_armv6" "6"
# Common legacy OpenWrt targets.
build_one "mipsle" "snispf_openwrt_mipsle_softfloat" "" "softfloat"
build_one "mips" "snispf_openwrt_mips_softfloat" "" "softfloat"
# Newer 64-bit OpenWrt devices.
build_one "arm64" "snispf_openwrt_arm64"

if ! command -v sha256sum >/dev/null 2>&1; then
  echo "sha256sum is required" >&2
  exit 1
fi

pushd "${OUT_DIR}" >/dev/null

artifacts=(
  "snispf_openwrt_armv7"
  "snispf_openwrt_armv6"
  "snispf_openwrt_mipsle_softfloat"
  "snispf_openwrt_mips_softfloat"
  "snispf_openwrt_arm64"
)

sha256sum "${artifacts[@]}" > checksums.txt

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
    "project": "snispf-core-openwrt",
    "generated_at_utc": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "artifacts": entries,
}
(release / "release_manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
PY

popd >/dev/null

echo "OpenWrt artifacts:"
for a in "${artifacts[@]}"; do
  ls -lh "${OUT_DIR}/${a}"
done
echo "Generated metadata:"
ls -lh "${OUT_DIR}/checksums.txt" "${OUT_DIR}/release_manifest.json"
