$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"

Write-Host "🔨 Mengompilasi binary Linux (racks-backend)..." -ForegroundColor Cyan
go build -ldflags="-w -s" -o racks-backend main.go

if ($LASTEXITCODE -eq 0) {
    $item = Get-Item racks-backend
    $sizeMB = [math]::Round($item.Length / 1MB, 2)
    Write-Host "✅ [BERHASIL] Binary Linux 'racks-backend' berhasil dibuat ($sizeMB MB)!" -ForegroundColor Green
    Write-Host "🚀 Siap di-commit & di-push ke git!" -ForegroundColor Yellow
} else {
    Write-Host "❌ [GAGAL] Kompilasi gagal dengan exit code $LASTEXITCODE" -ForegroundColor Red
}
