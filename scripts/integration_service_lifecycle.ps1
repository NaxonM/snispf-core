$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

$releaseDir = Join-Path $repo "release"
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null
$bin = Join-Path $releaseDir "snispf_windows_amd64.exe"
if (!(Test-Path $bin)) {
    go build -o $bin ./cmd/snispf
}

$svc = $null
$parent = $null
try {
    Get-CimInstance Win32_Process -Filter "name='snispf_windows_amd64.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like '*--service*--service-addr 127.0.0.1:8797*' } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }

    $parent = Start-Process powershell -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 120' -PassThru
    $ts = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
    $svc = Start-Process $bin -ArgumentList '--service','--service-addr','127.0.0.1:8797','--service-parent-pid',$parent.Id,'--service-parent-start-unix-ms',$ts -PassThru

    $ready = $false
    for ($i = 0; $i -lt 30; $i++) {
        try {
            $status = Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/status' -Method Get -TimeoutSec 2
            if ($status.api_version -eq 'v1') {
                $ready = $true
                break
            }
        } catch {}
        Start-Sleep -Milliseconds 300
    }
    if (-not $ready) {
        throw 'service did not become ready'
    }

    [void](Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/validate' -Method Get -TimeoutSec 5)
    [void](Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/health' -Method Get -TimeoutSec 5)
    [void](Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/logs?limit=50&level=ALL' -Method Get -TimeoutSec 5)

    [void](Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/start' -Method Post -TimeoutSec 5)
    Start-Sleep -Milliseconds 500
    $running = Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/status' -Method Get -TimeoutSec 5
    if (-not $running.running) {
        throw 'core did not report running after /v1/start'
    }

    [void](Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/stop' -Method Post -TimeoutSec 5)
    Start-Sleep -Milliseconds 500
    $stopped = Invoke-RestMethod -Uri 'http://127.0.0.1:8797/v1/status' -Method Get -TimeoutSec 5
    if ($stopped.running) {
        throw 'core still running after /v1/stop'
    }

    Stop-Process -Id $parent.Id -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 3
    $stillThere = Get-CimInstance Win32_Process -Filter "name='snispf_windows_amd64.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like '*--service*--service-addr 127.0.0.1:8797*' }
    if ($stillThere) {
        throw 'service did not exit after parent process termination'
    }

    Write-Host 'Lifecycle integration test passed.'
}
finally {
    if ($parent) {
        Stop-Process -Id $parent.Id -Force -ErrorAction SilentlyContinue
    }
    if ($svc) {
        Stop-Process -Id $svc.Id -Force -ErrorAction SilentlyContinue
    }
    Get-CimInstance Win32_Process -Filter "name='snispf_windows_amd64.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like '*--service*--service-addr 127.0.0.1:8797*' -or $_.CommandLine -like '*--run-core*' } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}
