param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    if (-not (Get-Command $Go -ErrorAction SilentlyContinue) -and (Test-Path '.tools/go/bin/go.exe')) { $Go = Join-Path $projectRoot '.tools/go/bin/go.exe' }
    New-Item -ItemType Directory -Force build,dist | Out-Null
    $env:CGO_ENABLED = '0'
    foreach ($targetArch in @('arm64','amd64')) {
        $env:GOOS = 'darwin'
        $env:GOARCH = $targetArch
        & $Go build -trimpath -ldflags '-s -w' -o "build/openpixelbar-darwin-$targetArch" ./cmd/openpixelbar
        if ($LASTEXITCODE -ne 0) { throw "macOS $targetArch build failed" }
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        & $Go run ./cmd/package-macos -binary "build/openpixelbar-darwin-$targetArch" -out "dist/OpenHaloPixelBar-macos-$targetArch.zip"
        if ($LASTEXITCODE -ne 0) { throw "macOS $targetArch packaging failed" }
    }
} finally { Pop-Location }
