# Build Electron app with Windows Defender workaround
# This script handles the EPERM errors by manually renaming directories

$ErrorActionPreference = "Stop"
$desktopDir = "D:\PersonalProject\Gleam\desktop"
$buildDir = Join-Path $desktopDir "build"

# Clean build directory
if (Test-Path $buildDir) {
    Remove-Item -Recurse -Force $buildDir
}

Set-Location $desktopDir

# First build attempt - will fail at rename step
Write-Host "First build attempt..."
$env:ELECTRON_MIRROR = "https://npmmirror.com/mirrors/electron/"
npx electron-builder --win --x64 2>&1 | Out-Null

# Check if win-unpacked.tmp exists and rename it
$tmpDir = Join-Path $buildDir "win-unpacked.tmp"
$unpackedDir = Join-Path $buildDir "win-unpacked"

if (Test-Path $tmpDir) {
    Write-Host "Manually renaming win-unpacked.tmp..."
    Rename-Item -Path $tmpDir -NewName "win-unpacked" -Force
}

# Second build attempt - should skip extraction and build NSIS
Write-Host "Second build attempt (NSIS packaging)..."
npx electron-builder --win --x64 2>&1

Write-Host "Build complete!"
Get-ChildItem -Path $buildDir -Filter "*.exe" | Select-Object Name, Length
