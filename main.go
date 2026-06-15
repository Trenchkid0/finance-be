package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"maybe-finance-backend/database"
	"maybe-finance-backend/handlers"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

func main() {
	// Set global timezone to Asia/Jakarta (WIB)
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err == nil {
		time.Local = loc
		log.Println("🌐 Timezone configured to Asia/Jakarta (WIB) globally")
	} else {
		time.Local = time.FixedZone("WIB", 7*60*60)
		log.Printf("🌐 Failed to load Asia/Jakarta timezone, using fallback WIB offset: %v", err)
	}

	// 1. Load env variables from .env file if it exists
	loadEnv()

	// 2. Initialize database
	dbPath := getEnv("DATABASE_URL", "maybe.db")
	// If DATABASE_URL starts with file: or sqlite:, strip it for SQLite driver
	dbPath = strings.TrimPrefix(dbPath, "file:")
	dbPath = strings.TrimPrefix(dbPath, "sqlite:")

	_, err = database.InitDB(dbPath)
	if err != nil {
		log.Fatalf("❌ Database init failed: %v", err)
	}
	log.Printf("📂 Database loaded from: %s", dbPath)

	// 3. Initialize Redis cache
	utils.InitRedis()
	defer utils.CloseRedis()

	// Start Auto-Pay background scheduler
	autoPayCtx, autoPayCancel := context.WithCancel(context.Background())
	defer autoPayCancel()
	startAutoPayScheduler(autoPayCtx)

	// Start Telegram Reminder background scheduler
	reminderCtx, reminderCancel := context.WithCancel(context.Background())
	defer reminderCancel()
	startReminderScheduler(reminderCtx)

	// 4. Setup router
	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("POST /api/auth/login", handlers.LoginHandler)
	mux.HandleFunc("POST /api/auth/register", handlers.RegisterHandler)
	mux.HandleFunc("POST /api/auth/logout", handlers.LogoutHandler)

	// Secured Routes Wrapper
	secureRoute := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, middleware.AuthRequired(http.HandlerFunc(handler)))
	}

	secureRoute("GET /api/auth/me", handlers.MeHandler)
	secureRoute("PUT /api/auth/me", handlers.MeHandler)
	secureRoute("GET /api/accounts", handlers.AccountsHandler)
	secureRoute("POST /api/accounts", handlers.AccountsHandler)
	secureRoute("GET /api/accounts/{id}", handlers.AccountDetailHandler)
	secureRoute("PUT /api/accounts/{id}", handlers.AccountDetailHandler)
	secureRoute("DELETE /api/accounts/{id}", handlers.AccountDetailHandler)

	secureRoute("GET /api/categories", handlers.CategoriesHandler)
	secureRoute("POST /api/categories", handlers.CategoriesHandler)

	secureRoute("GET /api/transactions", handlers.TransactionsHandler)
	secureRoute("POST /api/transactions", handlers.TransactionsHandler)
	secureRoute("DELETE /api/transactions", handlers.TransactionsHandler)
	secureRoute("GET /api/transactions/export", handlers.ExportTransactionsHandler)
	secureRoute("POST /api/transactions/import", handlers.ImportTransactionsHandler)
	secureRoute("GET /api/transactions/{id}", handlers.TransactionDetailHandler)
	secureRoute("PUT /api/transactions/{id}", handlers.TransactionDetailHandler)
	secureRoute("DELETE /api/transactions/{id}", handlers.TransactionDetailHandler)

	secureRoute("GET /api/budgets", handlers.BudgetsHandler)
	secureRoute("POST /api/budgets", handlers.BudgetsHandler)
	secureRoute("DELETE /api/budgets/{id}", handlers.BudgetDetailHandler)

	secureRoute("GET /api/api-keys", handlers.ApiKeysHandler)
	secureRoute("POST /api/api-keys", handlers.ApiKeysHandler)
	secureRoute("DELETE /api/api-keys/{id}", handlers.ApiKeyDetailHandler)

	secureRoute("GET /api/summary", handlers.SummaryHandler)
	secureRoute("POST /api/ai/scan", handlers.AIScanHandler)
	secureRoute("GET /api/ai/status", handlers.AIStatusHandler)
	secureRoute("GET /api/ai/insights", handlers.InsightsHandler)

	secureRoute("GET /api/goals", handlers.GoalsHandler)
	secureRoute("POST /api/goals", handlers.GoalsHandler)
	secureRoute("PUT /api/goals/{id}", handlers.GoalDetailHandler)
	secureRoute("DELETE /api/goals/{id}", handlers.GoalDetailHandler)

	secureRoute("GET /api/recurring", handlers.RecurringHandler)
	secureRoute("POST /api/recurring", handlers.RecurringHandler)
	secureRoute("PUT /api/recurring/{id}", handlers.RecurringDetailHandler)
	secureRoute("DELETE /api/recurring/{id}", handlers.RecurringDetailHandler)
	secureRoute("POST /api/recurring/{id}/pay", handlers.RecurringPayHandler)
	secureRoute("POST /api/recurring/test-telegram", handlers.TestTelegramHandler)

	// Upload endpoint
	secureRoute("POST /api/upload/receipt", handlers.UploadReceiptHandler)

	// Investment Portfolio endpoints
	secureRoute("GET /api/investments", handlers.InvestmentsHandler)
	secureRoute("POST /api/investments/buy", handlers.BuyAssetHandler)
	secureRoute("POST /api/investments/sell", handlers.SellAssetHandler)
	secureRoute("POST /api/investments/update-price", handlers.UpdatePriceHandler)

	// Telegram Bot Webhook (public - called by Telegram servers)
	mux.HandleFunc("POST /webhook/telegram", handlers.TelegramWebhookHandler)

	// Serve uploaded files
	uploadsFS := http.FileServer(http.Dir("uploads"))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads", uploadsFS))

	// Apply CORS
	allowedOrigin := getEnv("ALLOWED_ORIGIN", "*")
	handler := corsMiddleware(mux, allowedOrigin)

	// ✅ PERF: Gzip compression — reduces JSON payload size 60–80%
	handler = gzipMiddleware(handler)

	// ✅ PERF: Request timeout — prevents slow queries from holding connections forever
	handler = http.TimeoutHandler(handler, 30*time.Second, `{"error":"request timeout"}`)

	// 5. Start Server with graceful shutdown
	host := getEnv("HOST", "0.0.0.0")
	port := getEnv("PORT", "8080")
	bindAddr := host + ":" + port

	srv := &http.Server{
		Addr:         bindAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Channel to listen for errors from the server
	serverErrors := make(chan error, 1)

	go func() {
		fmt.Printf("🚀 Maybe Finance Backend running on http://%s\n", bindAddr)
		serverErrors <- srv.ListenAndServe()
	}()

	// Listen for OS signals (SIGINT = Ctrl+C, SIGTERM = Docker/K8s stop)
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatalf("❌ Server error: %v", err)
	case sig := <-shutdown:
		log.Printf("🛑 Shutdown signal received (%v). Draining active connections...", sig)

		// Cancel scheduler contexts
		autoPayCancel()
		reminderCancel()

		// Give outstanding requests 10 seconds to complete
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("⚠️ Graceful shutdown timed out: %v", err)
			srv.Close()
		}
		log.Println("✅ Server stopped gracefully")
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func loadEnv() {
	file, err := os.Open(".env")
	if err != nil {
		// No .env file, just proceed with system environments
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Strip quotes if any
			value = strings.Trim(value, `"'`)
			os.Setenv(key, value)
		}
	}
}

func isLocalNetworkOrigin(origin string) bool {
	// Must start with http:// or https://
	if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		return false
	}

	// Strip scheme
	host := origin
	if strings.HasPrefix(host, "http://") {
		host = host[7:]
	} else if strings.HasPrefix(host, "https://") {
		host = host[8:]
	}

	// Strip port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Check if localhost or 127.0.0.1
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}

	// Check private network patterns (IPv4)
	if strings.HasPrefix(host, "192.168.") {
		return true
	}
	if strings.HasPrefix(host, "100.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		// 172.16.0.0 - 172.31.255.255
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			var secondOctet int
			if _, err := fmt.Sscanf(parts[1], "%d", &secondOctet); err == nil {
				if secondOctet >= 16 && secondOctet <= 31 {
					return true
				}
			}
		}
	}

	return false
}

func corsMiddleware(next http.Handler, allowedOrigin string) http.Handler {
	allowAll := allowedOrigin == "*"

	// Build allowed origins set from env + common dev ports
	allowedOrigins := map[string]bool{
		allowedOrigin: true,
	}

	// Add common dev localhost variants
	for _, o := range []string{
		"http://localhost:3000", "http://localhost:5173", "http://localhost:5174",
		"http://localhost:8080", "http://localhost:4173",
		"http://127.0.0.1:3000", "http://127.0.0.1:5173", "http://127.0.0.1:5174",
	} {
		allowedOrigins[o] = true
	}

	// Also parse comma-separated ALLOWED_ORIGINS env for multi-origin setups
	if extra := getEnv("ALLOWED_ORIGINS", ""); extra != "" {
		for _, o := range strings.Split(extra, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				allowedOrigins[o] = true
			}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowAll && origin != "" {
			// ✅ ALLOWED_ORIGIN=* → reflect request origin (works with credentials)
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if origin != "" && (allowedOrigins[origin] || isLocalNetworkOrigin(origin)) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		} else {
			fmt.Printf("⚠️ CORS blocked origin: %s\n", origin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ✅ PERF: gzipMiddleware compresses responses for clients that accept gzip encoding
// Uses a sync.Pool to reuse gzip writers and avoid allocation pressure
var gzipPool = sync.Pool{New: func() interface{} { return gzip.NewWriter(io.Discard) }}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)
		gz.Reset(w)
		defer gz.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length") // length unknown after compression
		w.Header().Set("Vary", "Accept-Encoding")

		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

func startAutoPayScheduler(ctx context.Context) {
	log.Println("⏰ Auto-Pay background scheduler started")
	
	// Check immediately on startup, then every 4 hours
	go checkAutoPayBills()
	
	ticker := time.NewTicker(4 * time.Hour)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				log.Println("⏰ Auto-Pay scheduler stopped")
				return
			case <-ticker.C:
				checkAutoPayBills()
			}
		}
	}()
}

func checkAutoPayBills() {
	db := database.DB
	if db == nil {
		return
	}

	var bills []database.RecurringBill
	err := db.Where("auto_pay = ? AND account_id IS NOT NULL AND account_id != ''", true).Find(&bills).Error
	if err != nil {
		log.Printf("⚠️ [AutoPay] Failed to fetch auto-pay bills: %v", err)
		return
	}

	now := time.Now()
	todayDay := now.Day()
	
	// Helper to check if today is the last day of the month
	isLastDayOfMonth := func(t time.Time) bool {
		return t.AddDate(0, 0, 1).Day() == 1
	}

	for _, bill := range bills {
		// 1. Check if already paid this month
		if bill.LastPaidAt != nil && bill.LastPaidAt.Year() == now.Year() && bill.LastPaidAt.Month() == now.Month() {
			continue
		}

		// 2. Check if today is the due day
		isDue := false
		if bill.DayOfMonth == todayDay {
			isDue = true
		} else if isLastDayOfMonth(now) && bill.DayOfMonth > todayDay {
			isDue = true
		}

		if !isDue {
			continue
		}

		// 3. Process payment in transaction
		tx := db.Begin()
		
		// Get account
		var acc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *bill.AccountID, bill.UserID).First(&acc).Error; err != nil {
			tx.Rollback()
			log.Printf("❌ [AutoPay] Account not found for bill '%s': %v", bill.Name, err)
			continue
		}

		newBalance := acc.Balance - (bill.Amount + bill.AdminFee)
		if err := tx.Model(&acc).Update("balance", newBalance).Error; err != nil {
			tx.Rollback()
			log.Printf("❌ [AutoPay] Failed to update balance for bill '%s': %v", bill.Name, err)
			continue
		}

		// Create Transaction
		transaction := database.Transaction{
			UserID:      bill.UserID,
			AccountID:   *bill.AccountID,
			CategoryID:  bill.CategoryID,
			Type:        database.TransactionTypeExpense,
			Amount:      bill.Amount,
			AdminFee:    bill.AdminFee,
			Description: fmt.Sprintf("Auto-Pay: %s", bill.Name),
			Note:        fmt.Sprintf("Pembayaran tagihan rutin '%s' secara otomatis oleh sistem.", bill.Name),
			Date:        now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			log.Printf("❌ [AutoPay] Failed to create transaction for bill '%s': %v", bill.Name, err)
			continue
		}

		// Update LastPaidAt
		bill.LastPaidAt = &now
		if err := tx.Save(&bill).Error; err != nil {
			tx.Rollback()
			log.Printf("❌ [AutoPay] Failed to update bill paid date for '%s': %v", bill.Name, err)
			continue
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			log.Printf("❌ [AutoPay] Failed to commit transaction for bill '%s': %v", bill.Name, err)
			continue
		}

		// Invalidate cache
		_ = utils.CacheInvalidateUser(bill.UserID)
		log.Printf("⏰ [AutoPay] Successfully paid bill '%s' (Rp %.0f) for user: %s", bill.Name, bill.Amount, bill.UserID)
	}
}

func startReminderScheduler(ctx context.Context) {
	log.Println("⏰ Reminder background scheduler started")
	
	// Check immediately on startup, then every 5 minutes
	go handlers.CheckReminderBills()
	
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				log.Println("⏰ Reminder scheduler stopped")
				return
			case <-ticker.C:
				handlers.CheckReminderBills()
			}
		}
	}()
}
