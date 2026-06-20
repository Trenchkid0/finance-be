package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// BackupDatabaseHandler allows downloading the SQLite database file
func BackupDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	if database.DB.Dialector.Name() != "sqlite" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Database backup is only supported for SQLite.")
		return
	}

	dbFile := database.DbPath
	file, err := os.Open(dbFile)
	if err != nil {
		utils.HandleDBError(w, err, "open database file for backup")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=maybe.db")
	_, _ = io.Copy(w, file)
}

// RestoreDatabaseHandler allows uploading and restoring a SQLite database file
func RestoreDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	if database.DB.Dialector.Name() != "sqlite" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Database restore is only supported for SQLite.")
		return
	}

	// 1. Get uploaded file
	err := r.ParseMultipartForm(10 << 20) // 10MB limit
	if err != nil {
		utils.HandleBadRequest(w, "Gagal memproses file yang diunggah.")
		return
	}

	uploadedFile, _, err := r.FormFile("file")
	if err != nil {
		utils.HandleBadRequest(w, "File belum dipilih.")
		return
	}
	defer uploadedFile.Close()

	// 2. Close GORM database connection
	sqlDB, err := database.DB.DB()
	if err != nil {
		utils.HandleDBError(w, err, "get database instance")
		return
	}
	_ = sqlDB.Close()

	// 3. Overwrite SQLite database file
	dbFile := database.DbPath
	destFile, err := os.Create(dbFile)
	if err != nil {
		utils.HandleDBError(w, err, "create destination database file")
		return
	}

	_, err = io.Copy(destFile, uploadedFile)
	destFile.Close()
	if err != nil {
		utils.HandleDBError(w, err, "copy database restore data")
		return
	}

	// 4. Re-open / Initialize database connection
	_, err = database.InitDB(dbFile)
	if err != nil {
		utils.HandleDBError(w, err, "re-initialize database connection after restore")
		return
	}

	_ = utils.CacheInvalidateAll()
	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Database restored successfully"})
}

// ExportAllDataHandler returns all transactions, accounts, and categories for the user in a single JSON
func ExportAllDataHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var accounts []database.FinanceAccount
	var transactions []database.Transaction
	var categories []database.Category

	// Fetch all accounts
	if err := database.DB.Where("user_id = ?", userID).Find(&accounts).Error; err != nil {
		utils.HandleDBError(w, err, "fetch accounts for export")
		return
	}

	// Fetch all transactions
	if err := database.DB.Where("user_id = ?", userID).Order("date DESC").Find(&transactions).Error; err != nil {
		utils.HandleDBError(w, err, "fetch transactions for export")
		return
	}

	// Fetch all custom categories + default categories
	if err := database.DB.Where("user_id IS NULL OR user_id = ?", userID).Find(&categories).Error; err != nil {
		utils.HandleDBError(w, err, "fetch categories for export")
		return
	}

	exportData := map[string]interface{}{
		"accounts":     accounts,
		"transactions": transactions,
		"categories":   categories,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=maybe_finance_export.json")
	_ = json.NewEncoder(w).Encode(exportData)
}

// SystemHealthHandler provides real-time health metrics of Racks Finance
func SystemHealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	// 1. Check Database Status & Latency
	dbStart := time.Now()
	dbStatus := "UP"
	var dbLatencyMs float64
	if database.DB == nil {
		dbStatus = "DOWN"
	} else {
		sqlDB, err := database.DB.DB()
		if err != nil {
			dbStatus = "DOWN"
		} else {
			if err := sqlDB.Ping(); err != nil {
				dbStatus = "DOWN"
			} else {
				dbLatencyMs = float64(time.Since(dbStart).Nanoseconds()) / 1e6
			}
		}
	}

	// 2. Check Redis Cache Status & Latency
	redisStart := time.Now()
	redisStatus := "UP"
	var redisLatencyMs float64
	if utils.RedisClient == nil {
		redisStatus = "DOWN"
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := utils.RedisClient.Ping(ctx).Result(); err != nil {
			redisStatus = "DOWN"
		} else {
			redisLatencyMs = float64(time.Since(redisStart).Nanoseconds()) / 1e6
		}
	}

	// 3. Get Schedulers Metrics
	autoPayMetrics, reminderMetrics, startTime := utils.Metrics.GetMetrics()

	// 4. Get System Resource Usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptimeSeconds := time.Since(startTime).Seconds()

	healthData := map[string]interface{}{
		"status": "UP",
		"timestamp": time.Now().Format(time.RFC3339),
		"uptime": map[string]interface{}{
			"seconds": uptimeSeconds,
			"human":   time.Since(startTime).Round(time.Second).String(),
		},
		"components": map[string]interface{}{
			"database": map[string]interface{}{
				"status":    dbStatus,
				"latencyMs": dbLatencyMs,
			},
			"cache": map[string]interface{}{
				"status":    redisStatus,
				"latencyMs": redisLatencyMs,
			},
		},
		"schedulers": map[string]interface{}{
			"autoPay":      autoPayMetrics,
			"billReminder": reminderMetrics,
		},
		"resources": map[string]interface{}{
			"goroutines":    runtime.NumGoroutine(),
			"memAllocMb":    float64(m.Alloc) / (1024 * 1024),
			"memTotalAlloc": float64(m.TotalAlloc) / (1024 * 1024),
			"memSysMb":      float64(m.Sys) / (1024 * 1024),
		},
	}

	// Overall status degradation check
	if dbStatus == "DOWN" {
		healthData["status"] = "DOWN"
	} else if redisStatus == "DOWN" || autoPayMetrics.LastStatus == "failed" || reminderMetrics.LastStatus == "failed" {
		healthData["status"] = "DEGRADED"
	}

	utils.JSONResponse(w, http.StatusOK, healthData)
}
