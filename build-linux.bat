@echo off
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
echo Building Linux binary (racks-backend)...
go build -ldflags="-w -s" -o racks-backend main.go
if %ERRORLEVEL% equ 0 (
    echo [SUCCESS] Binary 'racks-backend' built successfully!
) else (
    echo [ERROR] Build failed with code %ERRORLEVEL%!
)
