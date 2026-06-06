# Backend Testing Guide

Panduan untuk menjalankan dan memverifikasi backend Maybe Finance.

## 📋 Informasi Backend

- **Framework:** Go (Golang)
- **Database:** SQLite (`maybe.db`)
- **Port Default:** `8080`
- **CORS Origin:** `http://localhost:5173` (Vite frontend)

## 🚀 Cara Menjalankan Backend

### Opsi 1: Menggunakan Script Test (Recommended)

```powershell
cd maybe-finance/backend
./test-backend.ps1
```

Script ini akan:
1. ✅ Memeriksa apakah file database ada
2. ✅ Memeriksa konfigurasi `.env`
3. ✅ Memeriksa apakah port 8080 tersedia
4. ✅ Menjalankan backend server

### Opsi 2: Menjalankan Langsung

```powershell
# Jika sudah di-compile
./maybe-finance-backend.exe

# Atau menggunakan Go
go run main.go
```

## 🧪 Cara Test API Endpoints

Setelah backend berjalan, buka terminal baru dan jalankan:

```powershell
cd maybe-finance/backend
./test-api.ps1
```

Script ini akan menguji:
1. ✅ Koneksi ke server
2. ✅ Login dengan demo user
3. ✅ Mendapatkan profil user
4. ✅ Mendapatkan daftar accounts
5. ✅ Mendapatkan daftar transactions

## 📊 Output yang Diharapkan

### Saat Backend Berhasil Jalan:

```
🚀 Maybe Finance Backend running on http://localhost:8080
📂 Database loaded from: maybe.db
```

### Jika Database Kosong (First Run):

```
🌱 Database is empty. Seeding default data...
✅ Seeding complete.
   Demo User Email: demo@maybe.local
   Demo API Key (Full): demo_api_key_demo_secret_key_1234567890abcdef
```

## 🔑 Demo User Credentials

Setelah seeding, gunakan kredensial ini untuk login:

- **Email:** `demo@maybe.local`
- **Password:** `password123`

## 📡 API Endpoints

### Public Endpoints
- `POST /api/auth/register` - Registrasi user baru
- `POST /api/auth/login` - Login user
- `POST /api/auth/logout` - Logout user

### Protected Endpoints (Butuh Authorization Header)
- `GET /api/auth/me` - Get user profile
- `GET /api/accounts` - Get all accounts
- `POST /api/accounts` - Create new account
- `GET /api/transactions` - Get all transactions
- `POST /api/transactions` - Create new transaction
- `GET /api/categories` - Get all categories
- `GET /api/summary` - Get financial summary
- `POST /api/ai/scan` - AI financial analysis

## 🔧 Konfigurasi (.env)

Buat file `.env` di folder backend dengan isi:

```env
PORT=8080
DATABASE_URL=maybe.db
ALLOWED_ORIGIN=http://localhost:5173
```

## ❓ Troubleshooting

### Port 8080 sudah digunakan

**Solusi 1:** Ubah port di `.env`:
```env
PORT=8081
```

**Solusi 2:** Stop proses yang menggunakan port 8080:
```powershell
# Cari proses yang menggunakan port 8080
Get-NetTCPConnection -LocalPort 8080

# Stop proses (ganti PID dengan ID proses yang ditemukan)
Stop-Process -Id <PID> -Force
```

### Database error

Jika ada error database, coba hapus file `maybe.db` dan jalankan ulang:
```powershell
Remove-Item maybe.db
./test-backend.ps1
```

Database baru akan dibuat dengan data demo.

### CORS error dari frontend

Pastikan `ALLOWED_ORIGIN` di `.env` sesuai dengan URL frontend:
```env
ALLOWED_ORIGIN=http://localhost:5173
```

## 📝 Test Manual dengan cURL

### Login
```powershell
curl -X POST http://localhost:8080/api/auth/login `
  -H "Content-Type: application/json" `
  -d '{"email":"demo@maybe.local","password":"password123"}'
```

### Get Accounts (dengan token)
```powershell
curl http://localhost:8080/api/accounts `
  -H "Authorization: Bearer YOUR_TOKEN_HERE"
```

## 🎯 Checklist Verifikasi

- [ ] Backend berjalan di port 8080
- [ ] Database `maybe.db` terbuat
- [ ] Seeding data berhasil
- [ ] Login dengan demo user berhasil
- [ ] API endpoints merespons dengan benar
- [ ] CORS tidak ada error saat akses dari frontend

## 📚 Struktur Database

Backend menggunakan SQLite dengan tabel:
- `users` - Data user
- `finance_accounts` - Akun keuangan (bank, wallet, cash)
- `categories` - Kategori income/expense
- `transactions` - Transaksi keuangan
- `budgets` - Budget planning
- `api_keys` - API keys untuk bot/automation

Semua tabel akan dibuat otomatis saat pertama kali backend dijalankan (auto-migration).