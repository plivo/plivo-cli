# install plivo cli — Windows PowerShell installer
#
# Usage:
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
$SumsUrl = if ($Version -eq 'latest') {
    "https://github.com/$Repo/releases/latest/download/SHA256SUMS"
} else {
    "https://github.com/$Repo/releases/download/$Version/SHA256SUMS"
}

# ─── Resolve install dir ─────────────────────────────────────────────────────
$InstallDir = if ($env:PLIVO_INSTALL_DIR) { $env:PLIVO_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Plivo\bin' }
$Target = Join-Path $InstallDir 'plivo.exe'

# ─── Download to temp, verify SHA-256, then install ──────────────────────────
# Mirror install.sh: never land an unverified binary on PATH. Pull the .exe +
# SHA256SUMS into temp, check the hash, and only then move into place.
$TmpDir  = Join-Path ([System.IO.Path]::GetTempPath()) ("plivo-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null
$TmpBin  = Join-Path $TmpDir $Asset
$TmpSums = Join-Path $TmpDir 'SHA256SUMS'
try {
    Write-Host "-> Downloading plivo for windows/$Arch ($Version)..."
    try {
        Invoke-WebRequest -Uri $Url -OutFile $TmpBin -UseBasicParsing
    } catch {
        Write-Error "Download failed: $Url`n  Check that a release exists and $Asset is published."
        exit 1
    }

    Write-Host "-> Verifying SHA-256..."
    try {
        Invoke-WebRequest -Uri $SumsUrl -OutFile $TmpSums -UseBasicParsing
    } catch {
        Write-Error "Could not download SHA256SUMS manifest: $SumsUrl`n  Aborting -- refusing to install an unverified binary."
        exit 1
    }
    # SHA256SUMS lines are '<hash>  <filename>' (filename may carry a '*' binary-mode prefix).
    $expected = $null
    foreach ($line in Get-Content $TmpSums) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1].TrimStart('*').Trim() -eq $Asset) {
            $expected = $parts[0].Trim(); break
        }
    }
    if (-not $expected) {
        Write-Error "SHA256SUMS has no entry for $Asset -- aborting."
        exit 1
    }
    $actual = (Get-FileHash -Algorithm SHA256 -Path $TmpBin).Hash
    # PowerShell string -ne is case-insensitive; SHA256SUMS is lower-case, Get-FileHash upper.
    if ($actual -ne $expected) {
        Write-Error "SHA-256 mismatch for $Asset`n    expected: $expected`n    actual:   $actual`n  Aborting -- nothing was installed."
        exit 1
    }
    Write-Host "OK Checksum verified"

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Move-Item -Force -Path $TmpBin -Destination $Target
} finally {
    if (Test-Path $TmpDir) { Remove-Item -Recurse -Force $TmpDir }
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
