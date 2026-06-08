$ErrorActionPreference = "Stop"

$Root = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$OutDir = if ($args.Count -gt 0) { $args[0] } else { Join-Path $Root "dist\release" }
$Version = if ($env:VERSION) { $env:VERSION } else { "dev" }
$Version = $Version -replace "[/\\]", "-"

$Target = "windows-amd64"
$DepsDir = Join-Path $Root ".release-deps\$Target"
$StageDir = Join-Path $OutDir "stage\$Target"
$ArchivePath = Join-Path $OutDir "better-recoll-$Target-$Version.zip"

New-Item -ItemType Directory -Force -Path $DepsDir, (Join-Path $StageDir "libs"), $OutDir | Out-Null
if (Test-Path $StageDir) {
    Remove-Item -Recurse -Force $StageDir
}
New-Item -ItemType Directory -Force -Path (Join-Path $StageDir "libs") | Out-Null

$env:Path = "C:\msys64\mingw64\bin;$env:Path"
$env:CC = "gcc"
$env:CXX = "g++"
$env:CGO_ENABLED = "1"

$modInfo = go mod download -json github.com/daulet/tokenizers | ConvertFrom-Json
$modDir = $modInfo.Dir
if (-not $modDir) {
    throw "failed to resolve github.com/daulet/tokenizers module directory"
}
$modDir = $modDir.Trim()
Push-Location $modDir
cargo build --release --target x86_64-pc-windows-gnu -p tokenizers-ffi
Pop-Location
Copy-Item (Join-Path $modDir "target\x86_64-pc-windows-gnu\release\libtokenizers_ffi.a") (Join-Path $DepsDir "libtokenizers.a") -Force

$env:CGO_LDFLAGS = "-L$($DepsDir -replace '\\','/') -lntdll"

$onnxZip = Join-Path $DepsDir "onnxruntime-win-x64-1.23.2.zip"
Invoke-WebRequest "https://github.com/microsoft/onnxruntime/releases/download/v1.23.2/onnxruntime-win-x64-1.23.2.zip" -OutFile $onnxZip
Expand-Archive -Path $onnxZip -DestinationPath $DepsDir -Force
Copy-Item (Join-Path $DepsDir "onnxruntime-win-x64-1.23.2\lib\onnxruntime.dll") (Join-Path $StageDir "libs\onnxruntime.dll") -Force
Copy-Item (Join-Path $Root "LICENSE") (Join-Path $StageDir "LICENSE") -Force
Copy-Item (Join-Path $Root "README.md") (Join-Path $StageDir "README.md") -Force

go build -o (Join-Path $StageDir "sfs.exe") ./cmd/sfs
go build -o (Join-Path $StageDir "sfs-server.exe") ./cmd/sfs-server

@"
better-recoll package: $Target

1. First-time model download:
   .\sfs.exe setup --light

2. Index a folder:
   .\sfs.exe index C:\path\to\documents

3. Start the web app:
   .\sfs-server.exe

Open http://localhost:8765 after the server starts.

Notes:
- Models are not bundled in this zip; they are downloaded into %USERPROFILE%\.sfs on first run.
- Windows release currently ships CLI + web server. The Spotlight-style app remains macOS-first.
"@ | Set-Content -Path (Join-Path $StageDir "RUN-FIRST.txt")

if (Test-Path $ArchivePath) {
    Remove-Item -Force $ArchivePath
}
Compress-Archive -Path (Join-Path $StageDir "*") -DestinationPath $ArchivePath -Force
Write-Host "created $ArchivePath"
