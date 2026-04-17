$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$releaseDir = Join-Path $repo "release"
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null

Write-Host "Running tests..."
go test ./...

Write-Host "Running vet..."
go vet ./...

Write-Host "Building Windows amd64 core binary..."
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -o (Join-Path $releaseDir "snispf_windows_amd64.exe") ./cmd/snispf

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

$copied = @{}
foreach ($src in $windivertCandidates) {
	if (Test-Path $src) {
		$name = Split-Path -Leaf $src
		if (-not $copied.ContainsKey($name)) {
			Copy-Item -Path $src -Destination (Join-Path $releaseDir $name) -Force
			$copied[$name] = $true
		}
	}
}

if (-not $copied.ContainsKey("WinDivert.dll") -or -not $copied.ContainsKey("WinDivert64.sys")) {
	Write-Warning "WinDivert.dll / WinDivert64.sys were not found in expected locations. wrong_seq on Windows requires them at runtime."
}

Write-Host "Build complete:"
Get-ChildItem $releaseDir | Where-Object { $_.Name -in @("snispf_windows_amd64.exe", "WinDivert.dll", "WinDivert64.sys") } | Select-Object FullName, LastWriteTime, Length
