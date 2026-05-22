# build-ghostdrive.ps1 — Build GhostDrive Windows executable via Wails + MinGW-w64.
#
# Prerequisites:
#   - Go 1.21+ in PATH (or GOROOT set)
#   - Wails v2 installed (go install github.com/wailsapp/wails/v2/cmd/wails@latest)
#   - MinGW-w64 GCC in C:\Users\cyril\tools\mingw64\mingw64\bin\
#     (includes gendef.exe and dlltool.exe — used to generate libcldapi.a)
#   - WinFsp installed at C:\Program Files (x86)\WinFsp\ (for cgofuse headers)
#   - CGO_ENABLED=1 required for cfapi package (Windows CF API)
#
# Usage:
#   .\build-ghostdrive.ps1              # build ghostdrive-v2.1.0.exe
#   .\build-ghostdrive.ps1 -OutName foo # build foo.exe

param(
    [string]$OutName = "ghostdrive-v2.1.0"
)

$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# MinGW toolchain paths
# ---------------------------------------------------------------------------

$mingwBin = "C:\Users\cyril\tools\mingw64\mingw64\bin"
$mingwLib = "C:\Users\cyril\tools\mingw64\mingw64\lib"
$mingwGcc = "$mingwBin\gcc.exe"

if (-not (Test-Path $mingwGcc)) {
    Write-Error "MinGW GCC not found at $mingwGcc. Adjust `$mingwBin in this script."
    exit 1
}

# ---------------------------------------------------------------------------
# Generate libcldapi.a if absent (requires gendef + dlltool from MinGW)
# cldapi.dll ships with Windows 10 1809+ in System32.
# ---------------------------------------------------------------------------

$cldapiDll = "$env:SystemRoot\System32\cldapi.dll"
$libcldapi  = "$mingwLib\libcldapi.a"
$defOut     = "$env:TEMP\cldapi.def"

if (-not (Test-Path $libcldapi)) {
    Write-Host "[build] libcldapi.a not found — generating from cldapi.dll..." -ForegroundColor Yellow

    $gendef  = "$mingwBin\gendef.exe"
    $dlltool = "$mingwBin\dlltool.exe"

    if (-not (Test-Path $gendef)) {
        Write-Error "gendef.exe not found at $gendef. Install MinGW-w64 binutils."
        exit 1
    }
    if (-not (Test-Path $cldapiDll)) {
        Write-Error "cldapi.dll not found at $cldapiDll. Windows 10 1809+ required."
        exit 1
    }

    # gendef writes cldapi.def to the current directory.
    & $gendef $cldapiDll
    if ($LASTEXITCODE -ne 0) { Write-Error "gendef failed"; exit $LASTEXITCODE }

    Move-Item "cldapi.def" $defOut -Force

    & $dlltool -D $cldapiDll -d $defOut -l $libcldapi
    if ($LASTEXITCODE -ne 0) { Write-Error "dlltool failed"; exit $LASTEXITCODE }

    Write-Host "[build] libcldapi.a generated at $libcldapi" -ForegroundColor Green
} else {
    Write-Host "[build] libcldapi.a found at $libcldapi" -ForegroundColor DarkGray
}

# ---------------------------------------------------------------------------
# CGO environment
# ---------------------------------------------------------------------------

$env:CGO_ENABLED = "1"
$env:CC          = $mingwGcc
$env:PATH        = "$mingwBin;" + $env:PATH
$env:GOOS        = "windows"
$env:GOARCH      = "amd64"

# WinFsp FUSE headers required by github.com/winfsp/cgofuse.
# PROGRA~2 is the 8.3 alias for "Program Files (x86)" — avoids spaces in CGO_CFLAGS.
$env:CGO_CFLAGS  = "-IC:/PROGRA~2/WinFsp/inc/fuse"

# Link against cldapi (Cloud Filter API) and ole32 (CLSIDFromString used in cfapi).
$env:CGO_LDFLAGS = "-L$mingwLib -lcldapi -lole32"

Write-Host "[build] GhostDrive — CGO_ENABLED=1, CC=$mingwGcc" -ForegroundColor Cyan
Write-Host "[build] CGO_CFLAGS  = $env:CGO_CFLAGS"  -ForegroundColor DarkGray
Write-Host "[build] CGO_LDFLAGS = $env:CGO_LDFLAGS" -ForegroundColor DarkGray

# ---------------------------------------------------------------------------
# Wails build
# ---------------------------------------------------------------------------

wails build -o "$OutName.exe" -platform windows/amd64

if ($LASTEXITCODE -ne 0) {
    Write-Error "wails build failed (exit $LASTEXITCODE)"
    exit $LASTEXITCODE
}

$exePath = "build\bin\$OutName.exe"
if (Test-Path $exePath) {
    $size = [math]::Round((Get-Item $exePath).Length / 1MB, 1)
    Write-Host "[build] OK — $exePath ($size MB)" -ForegroundColor Green
} else {
    Write-Warning "Build succeeded but $exePath not found — check wails build output path."
}
