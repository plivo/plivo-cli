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

    # Provenance: the checksum above proves the bytes are intact, this proves
    # they came from us.
    #
    # Releases from $FirstSignedRelease onward MUST carry a verifiable signature.
    # Previously the broad catch below set $haveSig = $false and installation
    # continued, so an attacker who could serve a modified binary and its
    # matching manifest only had to break the signature download to remove the
    # signer check. What the attacker controls is now fatal; whether cosign is
    # installed is not something they control, so that stays a warning.
    $FirstSignedRelease = 'v0.3.0'
    $TrustedIdentity = 'cx-tech@plivo.com'
    $TrustedIssuers  = @('https://accounts.google.com', 'https://github.com/login/oauth')
    $SigUrl  = "$SumsUrl.sig"
    $CertUrl = "$SumsUrl.pem"
    $TmpSig  = Join-Path $TmpDir 'SHA256SUMS.sig'
    $TmpCert = Join-Path $TmpDir 'SHA256SUMS.pem'
    # Signature required unless this version predates signing. An unparseable
    # version fails closed.
    $MustVerify = $true
    # Only an explicit truthy value overrides: in PowerShell a non-empty "0"
    # is truthy, so a plain `if ($env:...)` would treat =0 as "skip the check".
    if ($env:PLIVO_ALLOW_UNSIGNED -and
        @('1','true','yes','on') -contains $env:PLIVO_ALLOW_UNSIGNED.Trim().ToLower()) {
        $MustVerify = $false
    } elseif ($Version -ne 'latest' -and $Version -match '^v?(\d+)\.(\d+)\.') {
        $maj = [int]$Matches[1]; $min = [int]$Matches[2]
        if ($maj -eq 0 -and $min -lt 3) { $MustVerify = $false }
    }

    $haveSig = $false
    try {
        Invoke-WebRequest -Uri $SigUrl  -OutFile $TmpSig  -UseBasicParsing -ErrorAction Stop
        Invoke-WebRequest -Uri $CertUrl -OutFile $TmpCert -UseBasicParsing -ErrorAction Stop
        $haveSig = $true
    } catch { $haveSig = $false }

    if (-not $haveSig -and $MustVerify) {
        Write-Error ("Could not download the signature for $Version.`n" +
            "  Releases from $FirstSignedRelease onward must be signed. Refusing to install unverified code.`n" +
            "  Set PLIVO_ALLOW_UNSIGNED=1 to override if you accept the risk.")
        exit 1
    }

    if ($haveSig) {
        $cosign = Get-Command cosign -ErrorAction SilentlyContinue
        if ($cosign) {
            $sigOk = $false
            foreach ($iss in $TrustedIssuers) {
                & $cosign.Source verify-blob $TmpSums `
                    --signature $TmpSig `
                    --certificate $TmpCert `
                    --certificate-identity $TrustedIdentity `
                    --certificate-oidc-issuer $iss 2>$null 1>$null
                if ($LASTEXITCODE -eq 0) { $sigOk = $true; break }
            }
            if ($sigOk) {
                Write-Host "OK Signature verified ($TrustedIdentity)"
            } else {
                Write-Error "The SHA256SUMS signature did NOT verify -- refusing to install.`n  Expected signer: $TrustedIdentity"
                exit 1
            }
        } else {
            # Not fatal: an attacker cannot uninstall the user's cosign, and the
            # checksum still binds the binary to its manifest.
            Write-Host "-- Signature published but cosign is not installed; provenance not checked."
            Write-Host "   Install it from https://docs.sigstore.dev/cosign/system_config/installation/"
        }
    }

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
