<#
.SYNOPSIS
  Builds the shippable Flow installer.

.DESCRIPTION
  Three binaries and an extension folder have to land in build\bin before NSIS
  runs, because project.nsi picks them up from there by name. Doing that by hand
  is how you ship an installer whose daemon is the wrong architecture and fails
  to start on the target machine with no useful error.

  Needs: Go, Node, the Wails CLI, and NSIS on PATH.
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    winget install NSIS.NSIS

  ponytail: no signing step. Authenticode needs a certificate this project does
  not have, so every user gets a SmartScreen warning on first run. That is the
  honest state of it — see the signing note in claude.md rather than pretending
  an unsigned installer is finished.
#>
param(
    # arm64 or amd64. One architecture per installer: the daemon has to match the
    # machine, and a dual-arch installer would need a daemon of each inside it.
    [ValidateSet("arm64", "amd64")]
    [string]$Arch = "amd64"
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$bin = Join-Path $root "build\bin"

Push-Location $root
try {
    New-Item -ItemType Directory -Force -Path $bin | Out-Null

    Write-Host "Building daemon and CLI for windows/$Arch..."
    $env:GOOS = "windows"
    $env:GOARCH = $Arch
    & go build -o "$bin\flowd.exe" ./cmd/flowd
    if ($LASTEXITCODE -ne 0) { throw "flowd build failed" }
    & go build -o "$bin\flowctl.exe" ./cmd/flowctl
    if ($LASTEXITCODE -ne 0) { throw "flowctl build failed" }

    # The extension ships alongside rather than inside: Chrome loads it from a
    # folder, and it is additive anyway — without it, domain-level blocking holds.
    Write-Host "Staging the browser extension..."
    $extDir = Join-Path $bin "extension"
    if (Test-Path $extDir) { Remove-Item -Recurse -Force $extDir }
    Copy-Item (Join-Path $root "extension") -Destination $extDir -Recurse

    Write-Host "Building the desktop app and installer..."
    & wails build -platform "windows/$Arch" -nsis
    if ($LASTEXITCODE -ne 0) { throw "wails build failed" }

    $installer = Get-ChildItem "$bin\*installer.exe" | Select-Object -First 1
    Write-Host ""
    Write-Host "Installer: $($installer.FullName)"
    Write-Host "It requests elevation, registers the daemon service, and installs"
    Write-Host "the extension to <install dir>\extension for a manual Chrome load."
}
finally {
    Pop-Location
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}
