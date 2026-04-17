$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$outDir = Join-Path $repo "release\openwrt"
New-Item -ItemType Directory -Path $outDir -Force | Out-Null

function Set-Or-ClearEnv {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [AllowNull()][string]$Value
    )
    if ($null -eq $Value -or $Value -eq "") {
        Remove-Item ("Env:{0}" -f $Name) -ErrorAction SilentlyContinue
    } else {
        [Environment]::SetEnvironmentVariable($Name, $Value, "Process")
    }
}

function Invoke-Go {
    param(
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [Parameter(Mandatory = $true)][string]$What
    )
    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw ("go {0} failed with exit code {1}" -f $What, $LASTEXITCODE)
    }
}

$savedEnv = @{
    GOOS = $env:GOOS
    GOARCH = $env:GOARCH
    GOARM = $env:GOARM
    GOMIPS = $env:GOMIPS
    CGO_ENABLED = $env:CGO_ENABLED
}

Set-Or-ClearEnv -Name "GOOS" -Value $null
Set-Or-ClearEnv -Name "GOARCH" -Value $null
Set-Or-ClearEnv -Name "GOARM" -Value $null
Set-Or-ClearEnv -Name "GOMIPS" -Value $null
Set-Or-ClearEnv -Name "CGO_ENABLED" -Value $null

Write-Host "Running tests..."
Invoke-Go -Arguments @("test", "./...") -What "test ./..."

Write-Host "Running vet..."
Invoke-Go -Arguments @("vet", "./...") -What "vet ./..."

$targets = @(
    # Primary target for ipq40xx/generic devices (for example Linksys EA8300).
    @{ GOOS = "linux"; GOARCH = "arm"; GOARM = "7"; Out = "snispf_openwrt_armv7" },
    # Optional fallback for older ARM devices.
    @{ GOOS = "linux"; GOARCH = "arm"; GOARM = "6"; Out = "snispf_openwrt_armv6" },
    # Common legacy OpenWrt targets.
    @{ GOOS = "linux"; GOARCH = "mipsle"; GOMIPS = "softfloat"; Out = "snispf_openwrt_mipsle_softfloat" },
    @{ GOOS = "linux"; GOARCH = "mips"; GOMIPS = "softfloat"; Out = "snispf_openwrt_mips_softfloat" },
    # Newer 64-bit OpenWrt devices.
    @{ GOOS = "linux"; GOARCH = "arm64"; Out = "snispf_openwrt_arm64" }
)

$ldflags = "-s -w -buildid="

Write-Host "Building OpenWrt matrix..."
foreach ($t in $targets) {
    Write-Host (" - {0}/{1} -> {2}" -f $t.GOOS, $t.GOARCH, $t.Out)

    $env:CGO_ENABLED = "0"
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH

    if ($t.ContainsKey("GOARM")) { $env:GOARM = $t.GOARM } else { Remove-Item Env:GOARM -ErrorAction SilentlyContinue }
    if ($t.ContainsKey("GOMIPS")) { $env:GOMIPS = $t.GOMIPS } else { Remove-Item Env:GOMIPS -ErrorAction SilentlyContinue }

    Invoke-Go -Arguments @("build", "-trimpath", "-ldflags", $ldflags, "-o", (Join-Path $outDir $t.Out), "./cmd/snispf") -What ("build {0}" -f $t.Out)
}

$openwrtHelperSrc = Join-Path $repo "scripts\openwrt_snispf.sh"
if (Test-Path $openwrtHelperSrc) {
    Copy-Item -Path $openwrtHelperSrc -Destination (Join-Path $outDir "openwrt_snispf.sh") -Force
}
else {
    Write-Warning "OpenWrt helper script not found at scripts/openwrt_snispf.sh"
}

Set-Or-ClearEnv -Name "GOOS" -Value $savedEnv.GOOS
Set-Or-ClearEnv -Name "GOARCH" -Value $savedEnv.GOARCH
Set-Or-ClearEnv -Name "GOARM" -Value $savedEnv.GOARM
Set-Or-ClearEnv -Name "GOMIPS" -Value $savedEnv.GOMIPS
Set-Or-ClearEnv -Name "CGO_ENABLED" -Value $savedEnv.CGO_ENABLED

$artifacts = Get-ChildItem -Path $outDir -File | Sort-Object Name
if ($artifacts.Count -eq 0) {
    throw "No OpenWrt artifacts were produced"
}

$hashEntries = @()
foreach ($a in $artifacts) {
    $h = Get-FileHash -Path $a.FullName -Algorithm SHA256
    $hashEntries += [PSCustomObject]@{
        name   = $a.Name
        sha256 = $h.Hash.ToLowerInvariant()
        bytes  = $a.Length
    }
}

$checksumsPath = Join-Path $outDir "checksums.txt"
$hashEntries |
    ForEach-Object { "{0}  {1}" -f $_.sha256, $_.name } |
    Set-Content -Path $checksumsPath -Encoding ascii

$manifestPath = Join-Path $outDir "release_manifest.json"
$manifest = [ordered]@{
    project          = "snispf-core-openwrt"
    generated_at_utc = (Get-Date).ToUniversalTime().ToString("o")
    artifacts        = $hashEntries
}
($manifest | ConvertTo-Json -Depth 5) | Set-Content -Path $manifestPath -Encoding utf8

Write-Host "OpenWrt artifacts:"
$artifacts | Select-Object Name, Length, LastWriteTime
Write-Host "Generated metadata:"
Get-Item $checksumsPath, $manifestPath | Select-Object Name, Length, LastWriteTime
