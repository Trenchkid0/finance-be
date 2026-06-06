package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"maybe-finance-backend/database"
	"maybe-finance-backend/handlers"
	"maybe-finance-backend/middleware"
)

func main() {
	// 1. Load env variables from .env file if it exists
	loadEnv()

	// 2. Initialize database
	dbPath := getEnv("DATABASE_URL", "maybe.db")
	// If DATABASE_URL starts with file: or sqlite:, strip it for SQLite driver
	dbPath = strings.TrimPrefix(dbPath, "file:")
	dbPath = strings.TrimPrefix(dbPath, "sqlite:")

	_, err := database.InitDB(dbPath)
	if err != nil {
		log.Fatalf("❌ Database init failed: %v", err)
	}
	log.Printf("📂 Database loaded from: %s", dbPath)

	// 3. Setup router
	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("POST /api/auth/register", handlers.RegisterHandler)
	mux.HandleFunc("POST /api/auth/login", handlers.LoginHandler)
	mux.HandleFunc("POST /api/auth/logout", handlers.LogoutHandler)

	// Secured Routes Wrapper
	secureRoute := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, middleware.AuthRequired(http.HandlerFunc(handler)))
	}

	secureRoute("GET /api/auth/me", handlers.MeHandler)
	secureRoute("GET /api/accounts", handlers.AccountsHandler)
	secureRoute("POST /api/accounts", handlers.AccountsHandler)
	secureRoute("GET /api/accounts/{id}", handlers.AccountDetailHandler)
	secureRoute("PUT /api/accounts/{id}", handlers.AccountDetailHandler)
	secureRoute("DELETE /api/accounts/{id}", handlers.AccountDetailHandler)

	secureRoute("GET /api/categories", handlers.CategoriesHandler)
	secureRoute("POST /api/categories", handlers.CategoriesHandler)

	secureRoute("GET /api/transactions", handlers.TransactionsHandler)
	secureRoute("POST /api/transactions", handlers.TransactionsHandler)
	secureRoute("GET /api/transactions/export", handlers.ExportTransactionsHandler)
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

	// Apply CORS
	allowedOrigin := getEnv("ALLOWED_ORIGIN", "http://localhost:5173")
	handler := corsMiddleware(mux, allowedOrigin)

	// 4. Start Server
	host := getEnv("HOST", "0.0.0.0")
	port := getEnv("PORT", "8080")
	bindAddr := host + ":" + port
	fmt.Printf("🚀 Maybe Finance Backend running on http://%s\n", bindAddr)
	if err := http.ListenAndServe(bindAddr, handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
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

func corsMiddleware(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// In dev or credentials mode, reflect request origin if matched, or use the allowedOrigin
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
