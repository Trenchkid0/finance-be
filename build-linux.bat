@echo off
set CGO_ENABLED=0
set GOOS=linux

:: Default target: arm64 (untuk Raspberry Pi, Orange Pi, SBC, Armbian, atau devmon server)
:: Jika butuh x86_64/amd64, jalankan: build-linux.bat amd64
if "%1"=="" (
    set GOARCH=arm64
) else (
    set GOARCH=%1
)

echo ========================================================
echo Compiling Linux Binary (racks-backend)
echo Target: GOOS=%GOOS% ^| GOARCH=%GOARCH%
echo ========================================================

go build -ldflags="-w -s" -o racks-backend main.go
if %ERRORLEVEL% equ 0 (
    echo.
    echo [BERHASIL] Binary 'racks-backend' (Linux %GOARCH%) berhasil dibuat!
    echo Siap di-commit dan di-push ke Git!
) else (
    echo.
    echo [GAGAL] Build gagal dengan code %ERRORLEVEL%!
)
