$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$releaseDir = Join-Path $repo "release"
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null
$defaultConfigPath = Join-Path $releaseDir "default_config.json"

$windowsBundleDir = Join-Path $releaseDir "snispf_windows_amd64_bundle"
$linuxAmd64BundleDir = Join-Path $releaseDir "snispf_linux_amd64_bundle"
$linuxArm64BundleDir = Join-Path $releaseDir "snispf_linux_arm64_bundle"

Write-Host "Running tests..."
go test ./...

Write-Host "Running vet..."
go vet ./...

Write-Host "Generating default config for bundles..."
go run ./cmd/snispf --generate-config $defaultConfigPath

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

Write-Host "Packaging release bundles..."
Remove-Item -Path $windowsBundleDir, $linuxAmd64BundleDir, $linuxArm64BundleDir -Recurse -Force -ErrorAction SilentlyContinue

function Initialize-BundleLayout {
    param(
        [Parameter(Mandatory = $true)][string]$BundleDir,
        [Parameter(Mandatory = $true)][bool]$IncludeLinuxServiceFiles
    )

    New-Item -ItemType Directory -Path $BundleDir -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $BundleDir "configs\examples") -Force | Out-Null

    Copy-Item -Path $defaultConfigPath -Destination (Join-Path $BundleDir "config.json") -Force

    $examplesDir = Join-Path $repo "configs\examples"
    if (Test-Path $examplesDir) {
        Get-ChildItem -Path $examplesDir -File -Filter *.json -ErrorAction SilentlyContinue |
            ForEach-Object {
                Copy-Item -Path $_.FullName -Destination (Join-Path $BundleDir "configs\examples\$($_.Name)") -Force
            }
    }

    @"
SNISPF bundled release

Contents:
- Core binary for this platform
- config.json starter template
- configs/examples with additional profiles

Quick start:
1) Edit config.json for your upstream endpoint and SNI.
2) Run the binary with --config ./config.json.
3) On Windows strict wrong_seq mode requires WinDivert.dll and WinDivert64.sys next to the exe.
"@ | Set-Content -Path (Join-Path $BundleDir "README_BUNDLE.txt") -Encoding ascii

    if ($IncludeLinuxServiceFiles) {
        Copy-Item -Path (Join-Path $repo "scripts\install_linux_service.sh") -Destination (Join-Path $BundleDir "install_linux_service.sh") -Force
        Copy-Item -Path (Join-Path $repo "scripts\snispf.service") -Destination (Join-Path $BundleDir "snispf.service") -Force
    }
}

Initialize-BundleLayout -BundleDir $windowsBundleDir -IncludeLinuxServiceFiles $false
Initialize-BundleLayout -BundleDir $linuxAmd64BundleDir -IncludeLinuxServiceFiles $true
Initialize-BundleLayout -BundleDir $linuxArm64BundleDir -IncludeLinuxServiceFiles $true

Copy-Item -Path (Join-Path $releaseDir "snispf_windows_amd64.exe") -Destination (Join-Path $windowsBundleDir "snispf_windows_amd64.exe") -Force
Copy-Item -Path (Join-Path $releaseDir "snispf_linux_amd64") -Destination (Join-Path $linuxAmd64BundleDir "snispf_linux_amd64") -Force
Copy-Item -Path (Join-Path $releaseDir "snispf_linux_arm64") -Destination (Join-Path $linuxArm64BundleDir "snispf_linux_arm64") -Force

if (Test-Path (Join-Path $releaseDir "WinDivert.dll")) {
    Copy-Item -Path (Join-Path $releaseDir "WinDivert.dll") -Destination (Join-Path $windowsBundleDir "WinDivert.dll") -Force
}
if (Test-Path (Join-Path $releaseDir "WinDivert64.sys")) {
    Copy-Item -Path (Join-Path $releaseDir "WinDivert64.sys") -Destination (Join-Path $windowsBundleDir "WinDivert64.sys") -Force
}

$windowsBundleZip = Join-Path $releaseDir "snispf_windows_amd64_bundle.zip"
if (Test-Path $windowsBundleZip) {
    Remove-Item -Path $windowsBundleZip -Force
}
Compress-Archive -Path $windowsBundleDir -DestinationPath $windowsBundleZip -Force

$tarCmd = Get-Command tar -ErrorAction SilentlyContinue
if ($null -eq $tarCmd) {
    throw "tar is required to package Linux tar.gz bundles"
}

$linuxAmd64Tar = Join-Path $releaseDir "snispf_linux_amd64_bundle.tar.gz"
$linuxArm64Tar = Join-Path $releaseDir "snispf_linux_arm64_bundle.tar.gz"
if (Test-Path $linuxAmd64Tar) { Remove-Item -Path $linuxAmd64Tar -Force }
if (Test-Path $linuxArm64Tar) { Remove-Item -Path $linuxArm64Tar -Force }

& $tarCmd.Source -czf $linuxAmd64Tar -C $releaseDir (Split-Path -Leaf $linuxAmd64BundleDir)
& $tarCmd.Source -czf $linuxArm64Tar -C $releaseDir (Split-Path -Leaf $linuxArm64BundleDir)

Remove-Item -Path $windowsBundleDir, $linuxAmd64BundleDir, $linuxArm64BundleDir -Recurse -Force

$artifacts = @(
    Join-Path $releaseDir "snispf_windows_amd64.exe"
    Join-Path $releaseDir "snispf_linux_amd64"
    Join-Path $releaseDir "snispf_linux_arm64"
    Join-Path $releaseDir "snispf_windows_amd64_bundle.zip"
    Join-Path $releaseDir "snispf_linux_amd64_bundle.tar.gz"
    Join-Path $releaseDir "snispf_linux_arm64_bundle.tar.gz"
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
