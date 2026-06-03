# install plivo cli — Windows PowerShell installer
#
# Usage (while the code lives on the beta branch, pre-v1.0):
#   irm https://raw.githubusercontent.com/plivo/plivo-cli/beta/install.ps1 | iex
#
# At the v1.0 cut the canonical install.ps1 moves to the default branch:
#   irm https://raw.githubusercontent.com/plivo/plivo-cli/main/install.ps1 | iex
#
# On macOS / Linux / WSL / Git Bash, use install.sh instead.
#
# Requires the repo to be public and a published GitHub release whose assets
# are named plivo_windows_<arch>.exe (see `make build-all`).
#
# Env overrides:
#   $env:PLIVO_CLI_VERSION   tag to install (default: latest)
#   $env:PLIVO_INSTALL_DIR   install dir (default: %LOCALAPPDATA%\Plivo\bin)

$ErrorActionPreference = 'Stop'

$Repo = 'plivo/plivo-cli'
$Version = if ($env:PLIVO_CLI_VERSION) { $env:PLIVO_CLI_VERSION } else { 'latest' }

# ─── Detect architecture ─────────────────────────────────────────────────────
$archRaw = $env:PROCESSOR_ARCHITECTURE
switch ($archRaw) {
    'AMD64' { $Arch = 'amd64' }
    'ARM64' { $Arch = 'arm64' }
    'x86'   { $Arch = 'amd64' }  # 32-bit shell on 64-bit OS; ship amd64
    default { Write-Error "Unsupported architecture: $archRaw (need AMD64 or ARM64)"; exit 1 }
}

# ─── Resolve download URL ────────────────────────────────────────────────────
$Asset = "plivo_windows_$Arch.exe"
$Url = if ($Version -eq 'latest') {
    "https://github.com/$Repo/releases/latest/download/$Asset"
} else {
    "https://github.com/$Repo/releases/download/$Version/$Asset"
}

# ─── Resolve install dir ─────────────────────────────────────────────────────
$InstallDir = if ($env:PLIVO_INSTALL_DIR) { $env:PLIVO_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Plivo\bin' }
$Target = Join-Path $InstallDir 'plivo.exe'

Write-Host "-> Downloading plivo for windows/$Arch ($Version)..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
try {
    Invoke-WebRequest -Uri $Url -OutFile $Target -UseBasicParsing
} catch {
    Write-Error "Download failed: $Url`n  Check that a release exists and $Asset is published."
    exit 1
}

# ─── Add install dir to the user PATH (idempotent) ───────────────────────────
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not ($userPath -split ';' | Where-Object { $_ -eq $InstallDir })) {
    $newPath = if ([string]::IsNullOrEmpty($userPath)) { $InstallDir } else { "$userPath;$InstallDir" }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$InstallDir"  # make it work in the current session too
    Write-Host "-> Added $InstallDir to your user PATH (restart other shells to pick it up)."
}

Write-Host ""
$ver = & $Target --version 2>$null
Write-Host "OK Installed: $(if ($ver) { $ver } else { 'plivo (run plivo --version)' })"
Write-Host "OK Location:  $Target"
Write-Host ""
Write-Host "Next: plivo login"
