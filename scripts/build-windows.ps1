param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
    if (-not (Get-Command $Go -ErrorAction SilentlyContinue) -and (Test-Path '.tools/go/bin/go.exe')) { $Go = Join-Path $projectRoot '.tools/go/bin/go.exe' }
    New-Item -ItemType Directory -Force dist | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & $Go run github.com/tc-hib/go-winres@v0.3.3 make --in packaging/winres.json --out cmd/openpixelbar/rsrc --arch amd64
    if ($LASTEXITCODE -ne 0) { throw 'Windows icon/resource generation failed' }
    & $Go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    & $Go build -trimpath -ldflags '-s -w -H=windowsgui' -o dist/OpenHaloPixelBar.exe ./cmd/openpixelbar
    if ($LASTEXITCODE -ne 0) { throw 'Windows build failed' }
    Write-Host (Join-Path $projectRoot 'dist/OpenHaloPixelBar.exe')
} finally { Pop-Location }
