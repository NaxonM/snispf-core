$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$releaseDir = Join-Path $repo "release"
$checksumsPath = Join-Path $releaseDir "checksums.txt"
$manifestPath = Join-Path $releaseDir "release_manifest.json"

if (!(Test-Path $checksumsPath)) {
    throw "checksums.txt not found in release directory"
}
if (!(Test-Path $manifestPath)) {
    throw "release_manifest.json not found in release directory"
}

$lines = Get-Content $checksumsPath | Where-Object { $_.Trim().Length -gt 0 }
if ($lines.Count -eq 0) {
    throw "checksums.txt is empty"
}

$hashMap = @{}
foreach ($line in $lines) {
    $parts = $line -split "\s+", 2
    if ($parts.Count -ne 2) {
        throw "invalid checksum line: $line"
    }
    $sha = $parts[0].Trim().ToLowerInvariant()
    $name = $parts[1].Trim()
    $hashMap[$name] = $sha
}

$manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
if ($manifest.project -ne "snispf-core") {
    throw "unexpected manifest project: $($manifest.project)"
}

foreach ($name in $hashMap.Keys) {
    $path = Join-Path $releaseDir $name
    if (!(Test-Path $path)) {
        throw "artifact missing: $name"
    }

    $actualSha = (Get-FileHash -Path $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualSha -ne $hashMap[$name]) {
        throw "checksum mismatch for $name"
    }

    $item = Get-Item $path
    $m = $manifest.artifacts | Where-Object { $_.name -eq $name } | Select-Object -First 1
    if ($null -eq $m) {
        throw "manifest missing artifact: $name"
    }
    if ($m.sha256.ToLowerInvariant() -ne $actualSha) {
        throw "manifest hash mismatch for $name"
    }
    if ([int64]$m.bytes -ne [int64]$item.Length) {
        throw "manifest size mismatch for $name"
    }
}

Write-Host "Release verification passed."
