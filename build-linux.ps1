param (
    [string]$arch = "arm64"
)

$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = $arch

Write-Host "========================================================" -ForegroundColor DarkGray
Write-Host "🔨 Mengompilasi binary Linux (Target: $arch) -> racks-backend..." -ForegroundColor Cyan
Write-Host "========================================================" -ForegroundColor DarkGray

go build -ldflags="-w -s" -o racks-backend main.go

if ($LASTEXITCODE -eq 0) {
    $item = Get-Item racks-backend
    $sizeMB = [math]::Round($item.Length / 1MB, 2)
    Write-Host ""
    Write-Host "✅ [BERHASIL] Binary Linux $arch 'racks-backend' berhasil dibuat ($sizeMB MB)!" -ForegroundColor Green
    Write-Host "🚀 Siap di-commit & di-push ke git!" -ForegroundColor Yellow
} else {
    Write-Host ""
    Write-Host "❌ [GAGAL] Kompilasi gagal dengan exit code $LASTEXITCODE" -ForegroundColor Red
}
