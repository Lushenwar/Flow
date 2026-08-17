<#
.SYNOPSIS
  Verifies that a locked session survives a real reboot.

.DESCRIPTION
  The last exit criterion never exercised outside an injected clock. Run from an
  elevated prompt.

    .\reboot-test.ps1 -Arm      installs the service, starts a 60-minute session,
                                records the expected remaining time, then STOPS.
                                Reboot the machine yourself.
    .\reboot-test.ps1 -Check    after the reboot, compares actual remaining time
                                against what was predicted.

  It does not reboot for you on purpose. A script that restarts someone's machine
  because a test wanted it to is not a test, it is an outage.
#>
param(
    [switch]$Arm,
    [switch]$Check,
    [int]$Minutes = 60
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$statePath = "$env:ProgramData\Flow\reboot-test.json"

function Assert-Elevated {
    $me = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $me.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Run from an elevated prompt."
    }
}

function Get-Token { (Get-Content "$env:ProgramData\Flow\token" -Raw).Trim() }
function Get-State {
    Invoke-RestMethod -Uri http://127.0.0.1:8787/api/state -Headers @{Authorization = "Bearer $(Get-Token)" }
}

Assert-Elevated

if ($Arm) {
    & "$root\flowd.exe" uninstall 2>&1 | Out-Null
    & "$root\flowd.exe" install -port 8787
    Start-Sleep -Seconds 6

    $body = @{ mode = "commitment"; durationMinutes = $Minutes; blocklistIds = @("preset.video") } | ConvertTo-Json
    Invoke-RestMethod -Uri http://127.0.0.1:8787/api/session -Method Post `
        -Headers @{Authorization = "Bearer $(Get-Token)" } -ContentType application/json -Body $body | Out-Null

    Start-Sleep -Seconds 20  # let the grace window close so the session is genuinely locked
    $s = (Get-State).session

    @{
        armedAtUtc       = (Get-Date).ToUniversalTime().ToString("o")
        remainingSeconds = $s.remainingSeconds
        state            = $s.state
    } | ConvertTo-Json | Set-Content $statePath -Encoding utf8

    Write-Host ""
    Write-Host "Armed: $($s.state), $($s.remainingSeconds)s remaining."
    Write-Host "Now REBOOT this machine, then run:  .\reboot-test.ps1 -Check"
    Write-Host "Downtime is credited, so remaining should drop by roughly the"
    Write-Host "wall-clock time that passes, reboot included."
    exit 0
}

if ($Check) {
    if (-not (Test-Path $statePath)) { throw "No armed run found. Run -Arm first." }
    $before = Get-Content $statePath -Raw | ConvertFrom-Json
    $s = (Get-State).session

    $elapsedWall = ([datetime]::UtcNow - [datetime]::Parse($before.armedAtUtc).ToUniversalTime()).TotalSeconds
    $expected = $before.remainingSeconds - $elapsedWall
    $drift = [math]::Abs($s.remainingSeconds - $expected)

    Write-Host "state before reboot : $($before.state) / $($before.remainingSeconds)s"
    Write-Host "state now           : $($s.state) / $($s.remainingSeconds)s"
    Write-Host "wall clock elapsed  : $([math]::Round($elapsedWall))s"
    Write-Host "expected remaining  : $([math]::Round($expected))s"
    Write-Host "drift               : $([math]::Round($drift))s"
    Write-Host ""

    if ($s.state -ne "FOCUS" -and $s.state -ne "RELEASING") {
        Write-Host "FAIL: the session did not survive the reboot (state $($s.state))."
    } elseif ($drift -gt 30) {
        Write-Host "FAIL: drift of $([math]::Round($drift))s. Downtime credit is wrong."
    } else {
        Write-Host "PASS: session survived, remaining time within $([math]::Round($drift))s of expected."
    }
    Write-Host "Clean up with:  .\flowd.exe uninstall"
    exit 0
}

Write-Host "Pass -Arm or -Check. See the comment block at the top."
