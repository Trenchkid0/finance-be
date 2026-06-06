# Start Maybe Finance Backend
Write-Host "Starting Maybe Finance Backend..." -ForegroundColor Cyan

# Check if .env exists
if (-not (Test-Path ".env")) {
    Write-Host ".env file not found!" -ForegroundColor Red
    Write-Host "Creating .env from .env.example..." -ForegroundColor Yellow
    Copy-Item ".env.example" ".env"
}

# Load environment variables from .env
Get-Content .env | ForEach-Object {
    if ($_ -match '^([^#][^=]+)=(.*)$') {
        $name = $matches[1].Trim()
        $value = $matches[2].Trim()
        Set-Item -Path "env:$name" -Value $value
        Write-Host "Set $name" -ForegroundColor Green
    }
}

# Check if backend binary exists
if (-not (Test-Path "maybe-backend.exe")) {
    Write-Host "Backend binary not found!" -ForegroundColor Red
    Write-Host "Building backend..." -ForegroundColor Yellow
    go build -o maybe-backend.exe
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build failed!" -ForegroundColor Red
        exit 1
    }
}

# Start the backend
Write-Host ""
Write-Host "Starting backend on http://localhost:$env:PORT" -ForegroundColor Green
Write-Host "CORS allowed origin: $env:ALLOWED_ORIGIN" -ForegroundColor Cyan
Write-Host "Database: $env:DATABASE_URL" -ForegroundColor Cyan
Write-Host ""

& ./maybe-backend.exe

# Made with Bob
