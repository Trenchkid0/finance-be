# Maybe Finance — Go Backend API

Pondasi API untuk aplikasi **Maybe Finance** (atau Racks Finance). Dibuat menggunakan **Go (Golang)**, **GORM**, dan mendukung database **SQLite** atau **MySQL**. Backend ini menyediakan autentikasi berbasis JWT, manajemen keuangan, integrasi bot Telegram, serta pemrosesan AI (DeepSeek & OCR Tesseract) untuk pemindaian struk belanja dan analisis kesehatan finansial.

---

## 🛠️ Tech Stack

- **Core:** Go (Golang) versi 1.25+
- **HTTP Server:** Go standard `net/http` router (multiplexer baru di Go 1.22+)
- **ORM:** GORM (`gorm.io/gorm`)
- **Database:** SQLite (default untuk lokal) / MySQL (siap produksi)
- **Auth:** JWT (JSON Web Tokens) & Custom Cookies/Authorization Header
- **AI Integrasi:** DeepSeek API untuk OCR receipt parsing & financial insights

---

## 📂 Struktur Direktori

```text
backend/
├── database/            # Inisialisasi GORM, koneksi DB, & seeding data demo
├── handlers/            # REST API Handlers (users, accounts, transactions, dll)
├── middleware/          # JWT Auth Guard, CORS, & logging
├── services/            # Service eksternal (DeepSeek JSON API)
├── utils/               # Fungsi pembantu (helper, formatting, JSON error responder)
├── main.go              # Entry point utama & registrasi routing
├── maybe.db             # File database SQLite (terbuat otomatis)
├── start-backend.ps1    # Script runner untuk Windows
└── start-backend.sh     # Script runner untuk macOS/Linux
```

---

## 🔑 Database Schema (GORM Models)

Database dimigrasi secara otomatis saat server berjalan. Model utama meliputi:

1. **User:** Informasi kredensial & enkripsi password (bcrypt).
2. **FinanceAccount:** Menyimpan detail rekening (Bank, E-Wallet, Cash, Investment) beserta saldo aktual.
3. **Category:** Kategori pos pengeluaran/pemasukan milik user.
4. **Transaction:** Catatan alur kas masuk/keluar, terikat dengan `FinanceAccount` & `Category`.
5. **Budget:** Batasan belanja bulanan per kategori.
6. **Goal:** Target tabungan (saving goals).
7. **RecurringBill:** Tagihan berkala bulanan/tahunan (misal: Netflix, Listrik).
8. **ApiKey:** Token rahasia khusus untuk integrasi client eksternal (seperti Bot Telegram).

---

## 📡 API Endpoints

Semua endpoint dilindungi oleh middleware autentikasi, kecuali endpoint publik:

### 🔓 Public Routes
- `POST /api/auth/register` - Registrasi pengguna baru
- `POST /api/auth/login` - Login pengguna & menyimpan session cookie/token
- `POST /api/auth/logout` - Logout pengguna & membersihkan session cookie

### 🔒 Protected Routes (Membutuhkan header `Authorization: Bearer <token>` atau Session Cookie)
- **Auth & Profile:**
  - `GET /api/auth/me` - Ambil data profil pengguna yang login
  - `PUT /api/auth/me` - Update data profil (nama, mata uang default)
- **Accounts (Rekening):**
  - `GET /api/accounts` - Ambil daftar rekening aktif beserta saldonya
  - `POST /api/accounts` - Tambahkan rekening baru
  - `GET /api/accounts/{id}` - Detail riwayat rekening spesifik
  - `PUT /api/accounts/{id}` - Edit data/nama/tipe rekening
  - `DELETE /api/accounts/{id}` - Hapus/nonaktifkan rekening
- **Transactions:**
  - `GET /api/transactions` - Daftar transaksi dengan pencarian & filter
  - `POST /api/transactions` - Buat transaksi baru (otomatis menyesuaikan saldo rekening)
  - `GET /api/transactions/export` - Ekspor daftar transaksi dalam format CSV
- **Budgets & Goals:**
  - `GET /api/budgets` - Lihat alokasi anggaran bulanan vs realisasi pengeluaran
  - `POST /api/budgets` - Set target anggaran kategori baru
  - `GET /api/goals` - Daftar target tabungan & progres capaian
- **AI Features:**
  - `POST /api/ai/scan` - Mengirim teks OCR struk belanja untuk diparsing otomatis oleh AI (DeepSeek)
  - `GET /api/ai/insights` - Kalkulasi skor kesehatan finansial, dana darurat, & saran dari AI

---

## 🚀 Panduan Memulai

### 1. File Konfigurasi (`.env`)
Salin atau buat file `.env` di direktori `backend/`:
```env
PORT=8080
DATABASE_URL=maybe.db
ALLOWED_ORIGIN=http://localhost:5173
DEEPSEEK_API_KEY=your_deepseek_api_key_here  # Opsional untuk fitur AI
```

### 2. Menjalankan Server secara Lokal
Untuk menjalankan server secara instan:
* **Windows (PowerShell):**
  ```powershell
  ./start-backend.ps1
  ```
* **macOS / Linux:**
  ```bash
  chmod +x start-backend.sh
  ./start-backend.sh
  ```
* **Go CLI langsung:**
  ```bash
  go run main.go
  ```

Saat server pertama kali dijalankan, sistem akan otomatis melakukan migrasi database dan melakukan *seeding* akun demo:
- **Email:** `demo@maybe.local`
- **Password:** `password123`

### 3. Menguji Integrasi API
Anda dapat menjalankan script verifikasi endpoint untuk memastikan API berjalan sempurna:
```powershell
./test-api.ps1
```

### 🐳 4. Menjalankan via Docker
Kami telah menyediakan `Dockerfile` multi-stage minimalis untuk memudahkan deployment:
```bash
# 1. Build image docker
docker build -t maybe-finance-backend .

# 2. Jalankan container
# Disarankan mount volume '/app/data' agar database SQLite tetap persisten
docker run -d \
  -p 8080:8080 \
  -v maybe-db-volume:/app/data \
  --name maybe-backend \
  maybe-finance-backend
```

---

## 🤖 Integrasi Bot Telegram
Backend ini menyediakan endpoint API Key (`/api/api-keys`) agar bot Telegram (`bot-keuangan`) dapat berinteraksi dengan aman.
1. Dapatkan API Key di halaman **Settings -> API Keys** pada dashboard web.
2. Tempel key tersebut pada konfigurasi `.env` milik bot untuk menyinkronkan data saldo & transaksi secara instan.

### 🆔 Menghubungkan & Mendapatkan Telegram Chat ID
Untuk menghubungkan akun Telegram pengguna dengan dashboard web (berguna untuk notifikasi atau pengelolaan otomatis):
- **Otomatis via Bot:**
  Ketika bot Telegram berjalan dan pengguna mengirim pesan pertama kali (misalnya `/start` atau `/help`), middleware bot secara otomatis akan menangkap `chat.id` pengguna dan mengirimkannya ke database backend via endpoint `PUT /api/auth/me` untuk disimpan di profil pengguna (`telegramChatId`).
- **Mendapatkan Chat ID Secara Manual:**
  1. Mulai percakapan dengan bot Telegram Anda.
  2. Kirim perintah `/start` atau `/help`.
  3. Bot akan membalas dengan pesan bantuan dan menyantumkan **Telegram Chat ID Anda** berupa angka unik (misal: `123456789`).
  4. Anda dapat menggunakan ID ini untuk pengetesan manual API atau integrasi scheduler notifikasi backend.
