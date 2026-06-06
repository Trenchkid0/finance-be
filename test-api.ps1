# Script untuk test API endpoints backend
# Pastikan backend sudah berjalan sebelum menjalankan script ini

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Maybe Finance API Test Script" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

$baseUrl = "http://localhost:8080"
$testsPassed = 0
$testsFailed = 0

function Test-Endpoint {
    param(
        [string]$Name,
        [string]$Url,
        [string]$Method = "GET",
        [hashtable]$Headers = @{},
        [string]$Body = $null
    )
    
    Write-Host "Testing: $Name" -ForegroundColor Yellow
    Write-Host "  URL: $Method $Url" -ForegroundColor Gray
    
    try {
        $params = @{
            Uri = $Url
            Method = $Method
            Headers = $Headers
            TimeoutSec = 5
        }
        
        if ($Body) {
            $params.Body = $Body
            $params.ContentType = "application/json"
        }
        
        $response = Invoke-WebRequest @params -ErrorAction Stop
        
        Write-Host "  ✅ Status: $($response.StatusCode)" -ForegroundColor Green
        $script:testsPassed++
        return $response
    }
    catch {
        Write-Host "  ❌ Failed: $($_.Exception.Message)" -ForegroundColor Red
        $script:testsFailed++
        return $null
    }
    Write-Host ""
}

# Test 1: Check if server is running
Write-Host "[1/5] Checking if backend server is running..." -ForegroundColor Cyan
$serverRunning = Test-Endpoint -Name "Server Health Check" -Url "$baseUrl/api/auth/login" -Method "POST"

if (-not $serverRunning) {
    Write-Host ""
    Write-Host "❌ Backend server is not running!" -ForegroundColor Red
    Write-Host "   Please start the backend first using:" -ForegroundColor Yellow
    Write-Host "   ./test-backend.ps1" -ForegroundColor Yellow
    Write-Host ""
    exit 1
}
Write-Host ""

# Test 2: Login with demo user
Write-Host "[2/5] Testing login with demo user..." -ForegroundColor Cyan
$loginBody = @{
    email = "demo@maybe.local"
    password = "password123"
} | ConvertTo-Json

$loginResponse = Test-Endpoint -Name "Demo User Login" -Url "$baseUrl/api/auth/login" -Method "POST" -Body $loginBody

if ($loginResponse) {
    $loginData = $loginResponse.Content | ConvertFrom-Json
    $token = $loginData.token
    Write-Host "  Token received: $($token.Substring(0, 20))..." -ForegroundColor Gray
}
Write-Host ""

# Test 3: Get user profile
if ($token) {
    Write-Host "[3/5] Testing authenticated endpoint (Get Profile)..." -ForegroundColor Cyan
    $headers = @{
        "Authorization" = "Bearer $token"
    }
    Test-Endpoint -Name "Get User Profile" -Url "$baseUrl/api/auth/me" -Headers $headers
    Write-Host ""
}

# Test 4: Get accounts
if ($token) {
    Write-Host "[4/5] Testing accounts endpoint..." -ForegroundColor Cyan
    $headers = @{
        "Authorization" = "Bearer $token"
    }
    $accountsResponse = Test-Endpoint -Name "Get Accounts" -Url "$baseUrl/api/accounts" -Headers $headers
    
    if ($accountsResponse) {
        $accounts = $accountsResponse.Content | ConvertFrom-Json
        Write-Host "  Found $($accounts.Count) accounts:" -ForegroundColor Gray
        foreach ($acc in $accounts) {
            Write-Host "    - $($acc.name): Rp $($acc.balance.ToString('N0'))" -ForegroundColor Gray
        }
    }
    Write-Host ""
}

# Test 5: Get transactions
if ($token) {
    Write-Host "[5/5] Testing transactions endpoint..." -ForegroundColor Cyan
    $headers = @{
        "Authorization" = "Bearer $token"
    }
    $txResponse = Test-Endpoint -Name "Get Transactions" -Url "$baseUrl/api/transactions?limit=5" -Headers $headers
    
    if ($txResponse) {
        $txData = $txResponse.Content | ConvertFrom-Json
        Write-Host "  Found $($txData.total) total transactions" -ForegroundColor Gray
        Write-Host "  Showing first 5:" -ForegroundColor Gray
        foreach ($tx in $txData.transactions) {
            $amount = if ($tx.type -eq "expense") { "-Rp $($tx.amount.ToString('N0'))" } else { "+Rp $($tx.amount.ToString('N0'))" }
            Write-Host "    - $($tx.description): $amount" -ForegroundColor Gray
        }
    }
    Write-Host ""
}

# Summary
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Test Summary" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  ✅ Passed: $testsPassed" -ForegroundColor Green
Write-Host "  ❌ Failed: $testsFailed" -ForegroundColor Red
Write-Host ""

if ($testsFailed -eq 0) {
    Write-Host "🎉 All tests passed! Backend is working correctly." -ForegroundColor Green
} else {
    Write-Host "⚠️  Some tests failed. Please check the backend logs." -ForegroundColor Yellow
}
Write-Host ""

# Made with Bob
