# Racks Finance — Go Backend API

A robust, high-performance REST API for the **Racks Finance** personal finance management application. Built with **Go (Golang)**, **GORM**, and supporting both **SQLite** (development) and **MySQL** (production) databases. Features JWT authentication, Redis caching, Telegram bot integration, AI-powered receipt scanning (DeepSeek + OCR Tesseract), and comprehensive financial analytics.

---

## 🛠️ Tech Stack

| Component | Technology | Version |
|-----------|-----------|---------|
| **Language** | Go (Golang) | 1.25+ |
| **HTTP Server** | Standard `net/http` | Go 1.22+ router with path parameters |
| **ORM** | GORM | Latest |
| **Database** | SQLite / MySQL | Flexible |
| **Cache** | Redis | Optional, graceful degradation |
| **Authentication** | JWT | Custom cookies + Bearer tokens |
| **AI Integration** | DeepSeek API | OCR receipt parsing & financial insights |
| **Image Processing** | WebP conversion | Receipt image optimization |

---

## 📂 Project Structure

```
backend/
├── database/
│   ├── db.go                    # GORM initialization, migrations, demo seeding
│   ├── models.go                # Database model definitions
│   └── migrations/              # SQL migration files (performance indexes)
├── handlers/
│   ├── auth.go                  # Authentication (register, login, logout, profile)
│   ├── accounts.go              # Account CRUD operations
│   ├── transactions.go          # Transaction list + create/update
│   ├── transactions_detail.go   # Single transaction detail + delete
│   ├── transactions_export.go   # CSV export handler
│   ├── transactions_import.go   # CSV import with batch insert
│   ├── categories.go            # Category management
│   ├── budgets.go               # Budget tracking
│   ├── goals.go                 # Savings goals
│   ├── recurring.go             # Recurring bills & auto-pay
│   ├── investments.go           # Investment portfolio (buy/sell assets)
│   ├── ai.go                    # AI receipt scanning (DeepSeek OCR)
│   ├── insights.go              # Financial health scoring & AI insights
│   ├── summary.go               # Dashboard summary statistics
│   ├── telegram.go              # Telegram bot integration
│   ├── telegram_webhook.go      # Telegram webhook handler
│   ├── upload.go                # File upload (receipts)
│   ├── upload_webp.go           # WebP image conversion
│   ├── upload_nowebp.go         # Fallback upload without WebP
│   ├── api_keys.go              # API key management for external integrations
│   └── ...
├── middleware/
│   ├── auth.go                  # JWT authentication middleware
│   └── validate.go              # Struct validation (required, min, max, email, oneof)
├── services/
│   ├── deepseek.go              # DeepSeek AI API client
│   └── cache_example.go         # Redis caching examples
├── utils/
│   ├── redis.go                 # Redis client initialization & cache helpers
│   ├── cache_helpers.go         # Cache invalidation utilities
│   └── helpers.go               # General utility functions
├── uploads/                     # Uploaded receipt images
├── main.go                      # Application entry point, route registration, schedulers
├── racks.db                     # SQLite database (auto-created)
├── Dockerfile                   # Multi-stage Docker build
├── start-backend.sh             # Linux/macOS startup script
├── test-api.ps1                 # API endpoint testing script
└── test-backend.ps1             # Backend verification script
```

---

## 🗄️ Database Schema

The database is automatically migrated on first run. Core models:

### User
- User credentials, profile, Telegram integration
- Password hashed with bcrypt
- Relations: accounts, categories, transactions, budgets, goals, API keys

### FinanceAccount
- Multiple account types: Bank, Wallet, Cash, Investment
- Real-time balance tracking
- Custom icons and colors
- Currency support (default: IDR)

### Transaction
- Income, Expense, Transfer types
- Linked to accounts and categories
- Receipt image attachments
- Transfer between accounts support
- CSV export/import capabilities

### Category
- User-defined income/expense categories
- Default system categories
- Custom icons and colors

### Budget
- Monthly spending limits per category
- Real-time budget vs actual tracking

### SavingsGoal
- Target amount and deadline
- Progress tracking
- Optional account linking

### RecurringBill
- Recurring expenses (Netflix, utilities, etc.)
- Frequency: weekly, monthly, yearly
- **Auto-pay feature**: Automatically creates transactions on due date
- **Telegram Reminders**: Flexible reminder days offset (0-7 days before due date) and custom time (hour:minute)
- Telegram notifications on manual or automated payments

### AssetHolding
- Investment portfolio tracking
- Stock symbols, quantities, buy/current prices
- Real-time P&L calculation

### ApiKey
- Secure API tokens for external integrations (Telegram bot)
- SHA-256 hashed keys
- Usage tracking and revocation

---

## 📡 API Endpoints

### 🔓 Public Routes

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/auth/register` | Register new user |
| POST | `/api/auth/login` | Login (returns JWT token + cookie) |
| POST | `/api/auth/logout` | Logout (clears session cookie) |
| POST | `/webhook/telegram` | Telegram webhook (public, called by Telegram) |

### 🔒 Protected Routes (Require `Authorization: Bearer <token>` or session cookie)

#### Auth & Profile
- `GET /api/auth/me` — Get current user profile
- `PUT /api/auth/me` — Update profile (name, currency, Telegram ID)

#### Accounts
- `GET /api/accounts` — List all accounts with balances
- `POST /api/accounts` — Create new account
- `GET /api/accounts/{id}` — Get account details with transaction history
- `PUT /api/accounts/{id}` — Update account
- `DELETE /api/accounts/{id}` — Delete account

#### Transactions
- `GET /api/transactions` — List transactions (with filters, search, pagination)
- `POST /api/transactions` — Create transaction (auto-updates account balance)
- `GET /api/transactions/export` — Export transactions as CSV
- `POST /api/transactions/import` — Import transactions from CSV
- `GET /api/transactions/{id}` — Get transaction details
- `PUT /api/transactions/{id}` — Update transaction
- `DELETE /api/transactions/{id}` — Delete transaction

#### Categories
- `GET /api/categories` — List all categories
- `POST /api/categories` — Create category

#### Budgets
- `GET /api/budgets` — List budgets with spending progress
- `POST /api/budgets` — Create/update budget
- `DELETE /api/budgets/{id}` — Delete budget

#### Goals
- `GET /api/goals` — List savings goals with progress
- `POST /api/goals` — Create goal
- `PUT /api/goals/{id}` — Update goal
- `DELETE /api/goals/{id}` — Delete goal

#### Recurring Bills
- `GET /api/recurring` — List recurring bills
- `POST /api/recurring` — Create recurring bill
- `PUT /api/recurring/{id}` — Update bill
- `DELETE /api/recurring/{id}` — Delete bill
- `POST /api/recurring/{id}/pay` — Manually pay bill
- `POST /api/recurring/test-telegram` — Test Telegram notification

#### Investments
- `GET /api/investments` — Get investment portfolio with P&L
- `POST /api/investments/buy` — Buy asset
- `POST /api/investments/sell` — Sell asset
- `POST /api/investments/update-price` — Update asset current price

#### AI Features
- `POST /api/ai/scan` — Scan receipt image (OCR + AI parsing)
- `GET /api/ai/insights` — Get financial health score & AI recommendations

#### Summary & Analytics
- `GET /api/summary` — Dashboard statistics (net worth, income, expenses, trends)

#### API Keys
- `GET /api/api-keys` — List API keys
- `POST /api/api-keys` — Generate new API key
- `DELETE /api/api-keys/{id}` — Revoke API key

#### File Upload
- `POST /api/upload/receipt` — Upload receipt image

---

## 🚀 Getting Started

### Prerequisites

- **Go 1.25+** installed ([Download Go](https://golang.org/dl/))
- **Redis server** (optional, for caching)
- **DeepSeek API key** (optional, for AI features)

### 1. Environment Configuration

Create a `.env` file in the `backend/` directory:

```env
# Server Configuration
PORT=8080
HOST=0.0.0.0

# Database
# 1. SQLite (Default) - file path must end with ".db"
DATABASE_URL=maybe.db
# 2. MySQL - DSN format (username:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local)
# DATABASE_URL=cloudbeaver:passwordmu@tcp(localhost:3306)/finance_apps?charset=utf8mb4&parseTime=True&loc=Local

# CORS
ALLOWED_ORIGIN=http://localhost:5173     # Frontend URL
ALLOWED_ORIGINS=http://localhost:3000,http://localhost:8080  # Multiple origins

# Redis Cache (Optional)
REDIS_ENABLED=true
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# AI Features (Optional)
DEEPSEEK_API_KEY=your_deepseek_api_key_here

# Telegram Bot (Optional)
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
```

### 2. Running the Server

**Development Mode:**

```bash
# Navigate to backend directory
cd backend

# Run the server
go run main.go
```

**Using Startup Scripts:**

```bash
# Linux/macOS
chmod +x start-backend.sh
./start-backend.sh

# Windows (PowerShell)
./start-backend.ps1
```

**Build and Run Production Binary:**

```bash
go build -o racks-backend main.go
./racks-backend
```

The server starts on `http://localhost:8080` (or configured PORT).

**Demo Account (Auto-seeded):**
- Email: `demo@maybe.local`
- Password: `password123`

#### 🗄️ Database Setup Details
GORM dynamically selects the database driver depending on the connection string in `DATABASE_URL`:
- If the string ends with `.db`, it uses the **SQLite** driver and automatically initializes/migrates the schema in that file.
- If it contains `@tcp(`, it uses the **MySQL** driver, automatically creating tables and columns if they do not exist.

### 3. Testing the API

```bash
# Run automated API tests (Windows PowerShell)
./test-api.ps1

# Run backend verification
./test-backend.ps1

# Run Go tests
go test ./...
```

### 🐳 4. Docker Deployment

```bash
# Build Docker image
docker build -t racks-finance-backend .

# Run container with persistent volume
docker run -d \
  -p 8080:8080 \
  -v racks-db-volume:/app/data \
  -e DATABASE_URL=/app/data/racks.db \
  -e REDIS_ENABLED=false \
  --name racks-backend \
  racks-finance-backend
```

---

## ⚡ Performance Optimizations

### Redis Caching

Redis integration provides significant performance improvements with graceful degradation:

**Features:**
- **Graceful Degradation**: Falls back to direct database queries if Redis is unavailable
- **Pattern-based Invalidation**: Automatically clears cache after data mutations (POST, PUT, DELETE)
- **User-scoped Cache**: Isolates cache per user for data privacy
- **Sync.Pool for Gzip**: Reuses gzip writers to reduce memory allocation

**Cache TTL Strategy:**
- **Short (5 min)**: Frequently changing data (transactions, balances)
- **Medium (30 min)**: Moderate changes (accounts, categories)
- **Long (2 hours)**: Rarely changing data (user profile, settings)

### Gzip Compression

All JSON responses are compressed using gzip, reducing payload size by **60-80%**:

```go
// Automatic compression for clients with Accept-Encoding: gzip
handler = gzipMiddleware(handler)
```

### Request Timeout

Prevents slow queries from holding connections:

```go
handler = http.TimeoutHandler(handler, 30*time.Second, `{"error":"request timeout"}`)
```

### Database Indexes

Performance indexes on frequently queried fields:
- `user_id`, `account_id`, `category_id`, `date` on transactions
- `user_id` on accounts, categories, budgets, goals
- Composite indexes for complex queries

See `database/migrations/001_add_performance_indexes.sql` for details.

---

## 🤖 Telegram Bot Integration

The backend provides API key authentication for the Telegram bot (`bot-finance`) to securely interact with user accounts.

### Setup Flow

1. **Generate API Key**: In the web dashboard, go to **Settings → API Keys** and create a new key
2. **Configure Bot**: Add the API key to the bot's `.env` file
3. **Auto-sync**: Bot transactions automatically sync to the dashboard

### Telegram Chat ID Linking

**Automatic (via Bot):**
When a user sends `/start` or `/help` to the bot, the bot captures the `chat.id` and sends it to the backend via `PUT /api/auth/me`, storing it in the user profile (`telegramChatId`).

**Manual:**
1. Start a conversation with your Telegram bot
2. Send `/start` or `/help`
3. Bot replies with your **Telegram Chat ID** (e.g., `123456789`)
4. Use this ID for manual API testing or notification scheduling

---

## 📸 Receipt Scanning (AI-Powered)

### Upload Flow

**Via Web App:**
1. Upload receipt image through transaction form
2. Image saved to `uploads/receipts/`
3. Optional: Send to AI for OCR parsing

**Via Telegram Bot:**
1. Send receipt photo to bot
2. Bot performs OCR
3. AI (DeepSeek) analyzes and extracts: merchant, items, total amount, date
4. Transaction auto-created with receipt flag

### AI Scan Endpoint

```bash
POST /api/ai/scan
Content-Type: application/json

{
  "ocrText": "Extracted text from receipt image..."
}
```

**Response:**
```json
{
  "merchant": "Starbucks",
  "total": 85000,
  "date": "2024-01-15",
  "items": ["Caffe Latte", "Croissant"],
  "confidence": 0.95
}
```

---

## ⏰ Auto-Pay & Reminder Schedulers

### Auto-Pay Scheduler
The backend runs a background scheduler that automatically processes recurring bills with `autoPay` enabled:
- **Frequency**: Checks on startup, then every 4 hours
- **Logic**: Creates expense transaction on due date, deducts from linked account
- **Safety**: Transaction-wrapped with rollback on failure
- **Notifications**: Optional Telegram alerts on successful payment

### Reminder Scheduler
A secondary background scheduler runs to trigger Telegram alerts for upcoming manual or auto-pay bills:
- **Frequency**: Checks on startup, then every 5 minutes
- **Logic**: Evaluates customizable reminder settings (`reminderDaysBefore` and `reminderTime`) against the next due date
- **Safety**: Uses a `lastRemindedAt` timestamp to prevent duplicate notifications during the same billing cycle
- **Notifications**: Delivers rich HTML formatted Telegram alerts detailing bill name, amount, and due date

---

## 🔒 Security Features

- **Password Hashing**: bcrypt with salt rounds
- **JWT Authentication**: Secure token-based auth with expiration
- **CORS Protection**: Configurable allowed origins with local network detection
- **API Key Hashing**: SHA-256 hashed API tokens
- **Request Validation**: Reflection-based struct validation middleware (`validate` tags: required, min, max, email, oneof)
- **SQL Injection Prevention**: GORM parameterized queries

---

## 🧪 Testing & Troubleshooting

### Common Issues

**Port 8080 Already in Use:**

```bash
# Change port in .env
PORT=8081

# Or kill process on port 8080 (Windows PowerShell)
Get-NetTCPConnection -LocalPort 8080 | Select-Object -ExpandProperty OwningProcess | Stop-Process -Force
```

**Database Reset:**

```bash
# Delete SQLite database and restart (auto-migrates + seeds demo data)
rm racks.db
go run main.go
```

**Redis Connection Failed:**

Backend automatically falls back to direct database queries. Check Redis configuration in `.env`.

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -cover ./...

# Specific package
go test ./handlers
```

---

## 📊 Environment Variables Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `HOST` | `0.0.0.0` | Bind address |
| `DATABASE_URL` | `racks.db` | Database connection string |
| `ALLOWED_ORIGIN` | `*` | CORS allowed origin |
| `ALLOWED_ORIGINS` | `` | Comma-separated additional origins |
| `REDIS_ENABLED` | `false` | Enable Redis caching |
| `REDIS_HOST` | `localhost` | Redis server host |
| `REDIS_PORT` | `6379` | Redis server port |
| `REDIS_PASSWORD` | `` | Redis password (if auth enabled) |
| `REDIS_DB` | `0` | Redis database index (0-15) |
| `DEEPSEEK_API_KEY` | `` | DeepSeek API key for AI features |
| `TELEGRAM_BOT_TOKEN` | `` | Telegram bot token for notifications |

---

## 🎯 Key Features

✅ **Multi-account Support** — Track bank accounts, wallets, cash, investments  
✅ **Transaction Management** — Income, expenses, transfers with receipt attachments  
✅ **Budget Tracking** — Category-based spending limits with progress visualization  
✅ **Savings Goals** — Visual progress tracking for financial targets  
✅ **Recurring Bills & Timezone Reminders** — Auto-pay subscriptions and bills with scheduler synchronized to `Asia/Jakarta` (`WIB`) timezone and instant Telegram notification dispatch when reminders trigger.  
✅ **Dynamic Reminder Reset** — Modifying recurring bill properties automatically triggers scheduler recalculations so reminders are aligned with updated payment timings.  
✅ **Investment Portfolio** — Track stocks, crypto with real-time P&L  
✅ **AI Receipt Scanning** — OCR + DeepSeek AI for automatic transaction extraction  
✅ **Financial Insights** — AI-powered health scoring and recommendations  
✅ **CSV Import/Export** — Bulk transaction management  
✅ **Telegram Integration** — Real-time notifications and bot support  
✅ **Redis Caching** — High-performance with graceful degradation  
✅ **Docker Ready** — Multi-stage build for production deployment  

---

## 🔗 Related Projects

- **Frontend Dashboard (racks-finance-frontend)**: [GitHub Repository (finance-fe)](https://github.com/Trenchkid0/finance-fe)
- **Backend API (racks-finance-backend)**: [GitHub Repository (finance-be)](https://github.com/Trenchkid0/finance-be)
- **Telegram Bot (bot-finance)**: [GitHub Repository (bot-finance)](https://github.com/Trenchkid0/bot-finance)

---

## 🤝 Contributing

For issues, feature requests, or contributions, please open an issue or pull request.

---

**Built with ❤️ using Go, GORM, and Redis**
