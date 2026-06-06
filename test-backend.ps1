# Script untuk test backend Maybe Finance
# Menjalankan backend dan memverifikasi koneksi database

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Maybe Finance Backend Test Script" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# 1. Cek apakah file database ada
Write-Host "[1/4] Checking database file..." -ForegroundColor Yellow
if (Test-Path "maybe.db") {
    Write-Host "✅ Database file found: maybe.db" -ForegroundColor Green
    $dbSize = (Get-Item "maybe.db").Length / 1KB
    Write-Host "   Size: $([math]::Round($dbSize, 2)) KB" -ForegroundColor Gray
} else {
    Write-Host "⚠️  Database file not found. Will be created on first run." -ForegroundColor Yellow
}
Write-Host ""

# 2. Cek apakah .env file ada
Write-Host "[2/4] Checking .env configuration..." -ForegroundColor Yellow
if (Test-Path ".env") {
    Write-Host "✅ .env file found" -ForegroundColor Green
    Write-Host "   Configuration:" -ForegroundColor Gray
    Get-Content ".env" | Where-Object { $_ -notmatch "^#" -and $_ -ne "" } | ForEach-Object {
        Write-Host "   $_" -ForegroundColor Gray
    }
} else {
    Write-Host "⚠️  .env file not found. Using defaults:" -ForegroundColor Yellow
    Write-Host "   PORT=8080" -ForegroundColor Gray
    Write-Host "   DATABASE_URL=maybe.db" -ForegroundColor Gray
    Write-Host "   ALLOWED_ORIGIN=http://localhost:5173" -ForegroundColor Gray
}
Write-Host ""

# 3. Cek apakah port 8080 sudah digunakan
Write-Host "[3/4] Checking if port 8080 is available..." -ForegroundColor Yellow
$port = 8080
$portInUse = Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue
if ($portInUse) {
    Write-Host "⚠️  Port $port is already in use!" -ForegroundColor Red
    Write-Host "   Process using port: $($portInUse.OwningProcess)" -ForegroundColor Gray
    Write-Host "   You may need to stop the existing process or change PORT in .env" -ForegroundColor Yellow
} else {
    Write-Host "✅ Port $port is available" -ForegroundColor Green
}
Write-Host ""

# 4. Jalankan backend
Write-Host "[4/4] Starting backend server..." -ForegroundColor Yellow
Write-Host "   Press Ctrl+C to stop the server" -ForegroundColor Gray
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Jalankan backend executable jika ada, atau go run
if (Test-Path "maybe-finance-backend.exe") {
    Write-Host "Running: ./maybe-finance-backend.exe" -ForegroundColor Cyan
    & "./maybe-finance-backend.exe"
} elseif (Test-Path "maybe-backend.exe") {
    Write-Host "Running: ./maybe-backend.exe" -ForegroundColor Cyan
    & "./maybe-backend.exe"
} else {
    Write-Host "Running: go run main.go" -ForegroundColor Cyan
    go run main.go
}

# Made with Bob
