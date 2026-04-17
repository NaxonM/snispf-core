$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$releaseDir = Join-Path $repo "release"
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null

Write-Host "Running tests..."
go test ./...

Write-Host "Running vet..."
go vet ./...

Write-Host "Building release matrix..."
$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Out = "snispf_windows_amd64.exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; Out = "snispf_linux_amd64" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Out = "snispf_linux_arm64" }
)

foreach ($t in $targets) {
    Write-Host (" - {0}/{1} -> {2}" -f $t.GOOS, $t.GOARCH, $t.Out)
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    go build -o (Join-Path $releaseDir $t.Out) ./cmd/snispf
}

function Find-WinDivertFile {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Name
    )

    if (!(Test-Path $Root)) {
        return $null
    }

    $matches = Get-ChildItem -Path $Root -Recurse -File -Filter $Name -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending
    if ($matches -and $matches.Count -gt 0) {
        return $matches[0].FullName
    }
    return $null
}

$windivertCandidates = @()
$resourceRoot = Join-Path $repo "resources\WinDivert"
$resourceDll = Find-WinDivertFile -Root $resourceRoot -Name "WinDivert.dll"
$resourceSys = Find-WinDivertFile -Root $resourceRoot -Name "WinDivert64.sys"
if ($resourceDll) { $windivertCandidates += $resourceDll }
if ($resourceSys) { $windivertCandidates += $resourceSys }

# Backward-compatible fallback locations.
$windivertCandidates += @(
    (Join-Path $repo "WinDivert.dll"),
    (Join-Path $repo "WinDivert64.sys"),
    (Join-Path $repo "third_party\WinDivert\WinDivert.dll"),
    (Join-Path $repo "third_party\WinDivert\WinDivert64.sys")
)

$copiedWinDivert = @{}
foreach ($src in $windivertCandidates) {
    if (Test-Path $src) {
        $name = [System.IO.Path]::GetFileName($src)
        if (-not $copiedWinDivert.ContainsKey($name)) {
            Copy-Item -Path $src -Destination (Join-Path $releaseDir $name) -Force
            $copiedWinDivert[$name] = $true
        }
    }
}

if (-not $copiedWinDivert.ContainsKey("WinDivert.dll") -or -not $copiedWinDivert.ContainsKey("WinDivert64.sys")) {
    Write-Warning "WinDivert.dll / WinDivert64.sys were not found in expected locations. Windows wrong_seq runtime will require them beside the .exe."
}

$artifacts = @(
    Join-Path $releaseDir "snispf_windows_amd64.exe"
    Join-Path $releaseDir "snispf_linux_amd64"
    Join-Path $releaseDir "snispf_linux_arm64"
)

if (Test-Path (Join-Path $releaseDir "WinDivert.dll")) {
    $artifacts += (Join-Path $releaseDir "WinDivert.dll")
}
if (Test-Path (Join-Path $releaseDir "WinDivert64.sys")) {
    $artifacts += (Join-Path $releaseDir "WinDivert64.sys")
}

$hashEntries = @()
foreach ($a in $artifacts) {
    $h = Get-FileHash -Path $a -Algorithm SHA256
    $hashEntries += [PSCustomObject]@{
        name   = [System.IO.Path]::GetFileName($a)
        sha256 = $h.Hash.ToLowerInvariant()
        bytes  = (Get-Item $a).Length
    }
}

$checksumsPath = Join-Path $releaseDir "checksums.txt"
$hashEntries |
    ForEach-Object { "{0}  {1}" -f $_.sha256, $_.name } |
    Set-Content -Path $checksumsPath -Encoding ascii

$manifestPath = Join-Path $releaseDir "release_manifest.json"
$manifest = [ordered]@{
    project          = "snispf-core"
    generated_at_utc = (Get-Date).ToUniversalTime().ToString("o")
    artifacts        = $hashEntries
}
($manifest | ConvertTo-Json -Depth 5) | Set-Content -Path $manifestPath -Encoding utf8

Write-Host "Release artifacts:"
Get-Item $artifacts |
    Select-Object Name, Length, LastWriteTime
Write-Host "Generated metadata:"
Get-Item $checksumsPath, $manifestPath | Select-Object Name, Length, LastWriteTime
