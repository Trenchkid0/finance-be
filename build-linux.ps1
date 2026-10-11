param (
    [string]$arch = "arm64"
)

$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = $arch

Write-Host "========================================================"
Write-Host "Mengompilasi binary Linux (Target: $arch) -> racks-backend..."
Write-Host "========================================================"

go build -ldflags="-w -s" -o racks-backend main.go

if ($LASTEXITCODE -eq 0) {
    $item = Get-Item racks-backend
    $sizeMB = [math]::Round($item.Length / 1MB, 2)
    Write-Host ""
    Write-Host "[BERHASIL] Binary Linux $arch 'racks-backend' berhasil dibuat ($sizeMB MB)!"
    Write-Host "Siap di-commit and di-push ke git!"
} else {
    Write-Host ""
    Write-Host "[GAGAL] Kompilasi gagal dengan exit code $LASTEXITCODE"
}
