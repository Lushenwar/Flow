<#
.SYNOPSIS
  Installs Mast: builds the binaries, registers the service, and points you at the
  browser extension.

.DESCRIPTION
  Run from an elevated prompt. Service registration needs the SCM, and the
  enforcement layers need to write hosts and repoint the resolver.

  ponytail: not a signed MSI. Code signing needs a certificate this project does
  not have, and an unsigned MSI is no more trustworthy than a script you can read.
  The uninstall path is `mastd.exe uninstall`, which is what matters.
#>
param(
    [int]$Port = 8787,
    [string]$Block = "preset.adult,preset.gambling"
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$installDir = "$env:ProgramFiles\Mast"

function Assert-Elevated {
    $me = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $me.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Run this from an elevated prompt: the SCM, hosts, and DNS all need it."
    }
}

Assert-Elevated

Write-Host "Building..."
Push-Location $root
try {
    & go build -o "$root\mastd.exe" ./cmd/mastd
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
    & go build -o "$root\mastctl.exe" ./cmd/mastctl
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
}
finally { Pop-Location }

Write-Host "Installing to $installDir..."
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Copy-Item "$root\mastd.exe","$root\mastctl.exe" -Destination $installDir -Force
Copy-Item "$root\extension" -Destination $installDir -Recurse -Force

# Remove any previous registration first, so re-running is safe.
& "$installDir\mastd.exe" uninstall 2>&1 | Out-Null
& "$installDir\mastd.exe" install -port $Port
if ($LASTEXITCODE -ne 0) { throw "service install failed" }

Write-Host ""
Write-Host "Service installed and started. It auto-starts on boot."
Write-Host "  Verify:   $installDir\mastctl.exe health"
Write-Host "  Remove:   $installDir\mastd.exe uninstall"
Write-Host ""
Write-Host "One manual step — the browser extension:"
Write-Host "  1. Open chrome://extensions"
Write-Host "  2. Turn on Developer mode (top right)"
Write-Host "  3. Load unpacked -> $installDir\extension"
Write-Host ""
Write-Host "The extension is additive. Without it, domain-level blocking still holds;"
Write-Host "with it, youtube.com/watch is blocked while a channel URL stays reachable,"
Write-Host "and tabs that were already open when a session starts get closed."
