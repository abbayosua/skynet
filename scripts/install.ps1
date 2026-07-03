#!/usr/bin/env pwsh
param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\skynet"
)

$Repo = "abbayosua/skynet"
$ErrorActionPreference = "Stop"

function Write-Info($msg) { Write-Host "[INFO] $msg" -ForegroundColor Cyan }
function Write-Error($msg) { Write-Host "[ERROR] $msg" -ForegroundColor Red; exit 1 }

# Resolve latest version
if ($Version -eq "latest") {
    Write-Info "Fetching latest release..."
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
        $Version = $release.tag_name
    } catch {
        Write-Error "Failed to fetch latest version: $_"
    }
}

# Detect arch
$Arch = switch ([Environment]::Is64BitOperatingSystem) {
    $true  { "amd64" }
    $false { "386" }
}

$Archive = "skynet_${Version}_windows_${Arch}.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Version/$Archive"
$TempDir = Join-Path $env:TEMP "skynet-install"
$ZipPath = Join-Path $TempDir $Archive

Write-Info "Downloading skynet $Version for windows/${Arch}..."
Write-Host "  $DownloadUrl"

# Clean and create temp dir
if (Test-Path $TempDir) { Remove-Item -Recurse -Force $TempDir }
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null

try {
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath
} catch {
    Write-Error "Download failed: $_"
}

Write-Info "Extracting..."
Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

# Find skynet.exe (may be in a subdirectory)
$Exe = Get-ChildItem -Recurse -Filter "skynet.exe" -Path $TempDir | Select-Object -First 1
if (-not $Exe) {
    Write-Error "skynet.exe not found in archive"
}

# Create install directory
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

Write-Info "Installing to $InstallDir..."
Copy-Item -Path $Exe.FullName -Destination (Join-Path $InstallDir "skynet.exe") -Force

# Add to PATH if not already there
$UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($UserPath -notlike "*$InstallDir*") {
    Write-Info "Adding $InstallDir to user PATH..."
    [Environment]::SetEnvironmentVariable("PATH", "$UserPath;$InstallDir", "User")
    $env:PATH += ";$InstallDir"
}

# Cleanup
Remove-Item -Recurse -Force $TempDir

Write-Host ""
Write-Host "Installed skynet $Version!" -ForegroundColor Green
Write-Host "Run 'skynet --help' to get started." -ForegroundColor Green
