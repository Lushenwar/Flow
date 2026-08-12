<#
.SYNOPSIS
  Installs Flow: builds the binaries, registers the service, and points you at the
  browser extension.

.DESCRIPTION
  Run from an elevated prompt. Service registration needs the SCM, and the
  enforcement layers need to write hosts and repoint the resolver.

  ponytail: not a signed MSI. Code signing needs a certificate this project does
  not have, and an unsigned MSI is no more trustworthy than a script you can read.
  The uninstall path is `flowd.exe uninstall`, which is what matters.
#>
param(
    [int]$Port = 8787,
    [string]$Block = "preset.adult,preset.gambling"
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$installDir = "$env:ProgramFiles\Flow"

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
    & go build -o "$root\flowd.exe" ./cmd/flowd
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
    & go build -o "$root\flowctl.exe" ./cmd/flowctl
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
}
finally { Pop-Location }

# Report the signature honestly rather than staying quiet about it. An unsigned
# binary that installs a LocalSystem service is something the user should know
# they are trusting.
foreach ($exe in "$root\flowd.exe", "$root\flowctl.exe") {
    $sig = Get-AuthenticodeSignature $exe
    if ($sig.Status -eq "Valid") {
        Write-Host "Signed by $($sig.SignerCertificate.Subject)"
    } else {
        Write-Host "NOT SIGNED ($($sig.Status)). See sign.ps1 — signing needs a certificate this project does not ship."
    }
}

Write-Host "Installing to $installDir..."
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Copy-Item "$root\flowd.exe","$root\flowctl.exe" -Destination $installDir -Force
Copy-Item "$root\extension" -Destination $installDir -Recurse -Force

# Checksums let someone verify later that the installed binary is the one that
# was built, which is the part of "signed" that is actually achievable here.
Get-FileHash "$installDir\flowd.exe","$installDir\flowctl.exe" -Algorithm SHA256 |
    ForEach-Object { "$($_.Hash)  $(Split-Path $_.Path -Leaf)" } |
    Set-Content "$installDir\checksums.txt" -Encoding ascii

# Remove any previous registration first, so re-running is safe.
& "$installDir\flowd.exe" uninstall 2>&1 | Out-Null
& "$installDir\flowd.exe" install -port $Port
if ($LASTEXITCODE -ne 0) { throw "service install failed" }

Write-Host ""
Write-Host "Service installed and started. It auto-starts on boot."
Write-Host "  Verify:   $installDir\flowctl.exe health"
Write-Host "  Remove:   $installDir\flowd.exe uninstall"
Write-Host ""
Write-Host "One manual step — the browser extension:"
Write-Host "  1. Open chrome://extensions"
Write-Host "  2. Turn on Developer mode (top right)"
Write-Host "  3. Load unpacked -> $installDir\extension"
Write-Host ""
Write-Host "The extension is additive. Without it, domain-level blocking still holds;"
Write-Host "with it, youtube.com/watch is blocked while a channel URL stays reachable,"
Write-Host "and tabs that were already open when a session starts get closed."
