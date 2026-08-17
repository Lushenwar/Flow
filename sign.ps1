<#
.SYNOPSIS
  Authenticode-signs the Flow binaries and installer.

.DESCRIPTION
  Phase 7's exit criterion asks for a signed installer. The machinery is here;
  what is missing is a certificate, and that is a purchase, not a coding task.

  Deliberately NOT self-signing. A self-signed certificate produces a binary that
  still trips SmartScreen and still shows "Unknown Publisher", while looking
  signed to anyone checking a checkbox. That is worse than honestly unsigned: it
  buys nothing and hides the gap.

.EXAMPLE
  .\sign.ps1 -Thumbprint 1A2B3C...
  .\sign.ps1 -PfxPath .\flow.pfx
#>
param(
    [string]$Thumbprint,
    [string]$PfxPath,
    [string]$TimestampUrl = "http://timestamp.digicert.com"
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$targets = @("$root\flowd.exe", "$root\flowctl.exe", "$root\install.ps1") |
    Where-Object { Test-Path $_ }

if (-not $targets) { throw "Nothing to sign. Build first: go build -o flowd.exe ./cmd/flowd" }

$cert = $null
if ($Thumbprint) {
    $cert = Get-ChildItem Cert:\CurrentUser\My, Cert:\LocalMachine\My |
        Where-Object { $_.Thumbprint -eq $Thumbprint } | Select-Object -First 1
    if (-not $cert) { throw "No certificate with thumbprint $Thumbprint" }
} elseif ($PfxPath) {
    if (-not (Test-Path $PfxPath)) { throw "No PFX at $PfxPath" }
    $pw = Read-Host "PFX password" -AsSecureString
    $cert = Get-PfxCertificate -FilePath $PfxPath -Password $pw
} else {
    Write-Host "No certificate supplied. Current signature status:"
    foreach ($t in $targets) {
        $sig = Get-AuthenticodeSignature $t
        "{0,-16} {1}" -f (Split-Path $t -Leaf), $sig.Status
    }
    Write-Host ""
    Write-Host "Pass -Thumbprint or -PfxPath to sign. Until then the build is"
    Write-Host "unsigned, and SmartScreen will say so, which is accurate."
    exit 0
}

foreach ($t in $targets) {
    # Timestamping matters more than it looks: without it every signature stops
    # verifying the day the certificate expires, including on builds already
    # installed on someone's machine.
    $sig = Set-AuthenticodeSignature -FilePath $t -Certificate $cert `
        -TimestampServer $TimestampUrl -HashAlgorithm SHA256
    "{0,-16} {1}" -f (Split-Path $t -Leaf), $sig.Status
    if ($sig.Status -ne "Valid") { throw "Signing failed for $t : $($sig.StatusMessage)" }
}
Write-Host "Signed. Verify with: Get-AuthenticodeSignature .\flowd.exe"
