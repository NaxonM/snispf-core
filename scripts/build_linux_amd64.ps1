$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$releaseDir = Join-Path $repo "release"
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null

Write-Host "Running tests..."
go test ./...

Write-Host "Running vet..."
go vet ./...

Write-Host "Building Linux amd64 core binary..."
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o (Join-Path $releaseDir "snispf_linux_amd64") ./cmd/snispf

Write-Host "Build complete:"
Get-Item (Join-Path $releaseDir "snispf_linux_amd64") | Select-Object FullName, LastWriteTime, Length
