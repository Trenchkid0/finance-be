package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

func resolveReceiptURL(r *http.Request, url *string) *string {
	if url == nil || *url == "" {
		return url
	}
	if strings.HasPrefix(*url, "/uploads/") {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		}
		fullURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, *url)
		return &fullURL
	}
	return url
}

type TransactionRequest struct {
	AccountID       string                   `json:"accountId" validate:"required"`
	CategoryID      *string                  `json:"categoryId"`
	Type            database.TransactionType `json:"type" validate:"required,oneof=income|expense|transfer"`
	Amount          float64                  `json:"amount" validate:"required,min=0.01"`
	AdminFee        *float64                 `json:"adminFee"`
	Description     string                   `json:"description"`
	Note            string                   `json:"note"`
	Date            string                   `json:"date"` // ISO string e.g. "2026-06-05T00:00:00Z"
	TransferToID    *string                  `json:"transferToId"`
	ReceiptImageURL *string                  `json:"receiptImageUrl"` // URL foto struk
}

type TransactionsListResponse struct {
	Transactions []database.Transaction `json:"transactions"`
	Total        int64                  `json:"total"`
	Income       float64                `json:"income"`
	Expense      float64                `json:"expense"`
}

// TransactionsHandler routes listing and creating transactions
func TransactionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// ✅ PERF: Redis cache — serve cached response if available (2-min TTL)
		cacheKey := utils.BuildCacheKey("transactions", userID, r.URL.RawQuery)
		var cachedResponse TransactionsListResponse
		if err := utils.CacheGet(cacheKey, &cachedResponse); err == nil {
			for i := range cachedResponse.Transactions {
				cachedResponse.Transactions[i].ReceiptImageURL = resolveReceiptURL(r, cachedResponse.Transactions[i].ReceiptImageURL)
			}
			utils.JSONResponse(w, http.StatusOK, cachedResponse)
			return
		}

		// Filters
		accountID := r.URL.Query().Get("accountId")
		txType := r.URL.Query().Get("type")
		categoryID := r.URL.Query().Get("categoryId")
		startDateStr := r.URL.Query().Get("startDate")
		endDateStr := r.URL.Query().Get("endDate")
		search := r.URL.Query().Get("search")

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		baseQuery := database.DB.Model(&database.Transaction{}).Where("user_id = ?", userID)

		if accountID != "" && accountID != "all" {
			baseQuery = baseQuery.Where("account_id = ? OR transfer_to_id = ?", accountID, accountID)
		}
		if txType != "" && txType != "all" {
			baseQuery = baseQuery.Where("type = ?", txType)
		}
		if categoryID != "" && categoryID != "all" {
			if categoryID == "none" {
				baseQuery = baseQuery.Where("category_id IS NULL")
			} else {
				baseQuery = baseQuery.Where("category_id = ?", categoryID)
			}
		}
		if startDateStr != "" {
			if parsed, err := time.Parse("2006-01-02", startDateStr); err == nil {
				baseQuery = baseQuery.Where("date >= ?", parsed)
			} else if parsed, err := time.Parse(time.RFC3339, startDateStr); err == nil {
				baseQuery = baseQuery.Where("date >= ?", parsed)
			}
		}
		if endDateStr != "" {
			if parsed, err := time.Parse("2006-01-02", endDateStr); err == nil {
				parsedNext := parsed.AddDate(0, 0, 1)
				baseQuery = baseQuery.Where("date < ?", parsedNext)
			} else if parsed, err := time.Parse(time.RFC3339, endDateStr); err == nil {
				baseQuery = baseQuery.Where("date <= ?", parsed)
			}
		}
		if search != "" {
			baseQuery = baseQuery.Where("description LIKE ? OR note LIKE ?", "%"+search+"%", "%"+search+"%")
		}

		// Count total before pagination
		var total int64
		if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			utils.HandleDBError(w, err, "count transactions")
			return
		}

		// Aggregate income and expense sums
		type SumResult struct {
			Type string  `gorm:"column:type"`
			Sum  float64 `gorm:"column:sum"`
		}
		var sumResults []SumResult
		if err := baseQuery.Session(&gorm.Session{}).Select("type, SUM(amount) as sum").Group("type").Scan(&sumResults).Error; err != nil {
			utils.HandleDBError(w, err, "aggregate transactions")
			return
		}

		var totalIncome float64
		var totalExpense float64
		for _, s := range sumResults {
			if s.Type == string(database.TransactionTypeIncome) {
				totalIncome = s.Sum
			} else if s.Type == string(database.TransactionTypeExpense) {
				totalExpense = s.Sum
			}
		}

		// Pagination
		limit := 50
		if limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		if limit > 200 {
			limit = 200
		}
		offset := 0
		if offsetStr != "" {
			if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
				offset = parsed
			}
		}

		var transactions []database.Transaction
		if err := baseQuery.Session(&gorm.Session{}).Preload("Category").Preload("Account").Preload("TransferTo").
			Order("date desc, created_at desc").Limit(limit).Offset(offset).Find(&transactions).Error; err != nil {
			utils.HandleDBError(w, err, "retrieve transactions")
			return
		}

		for i := range transactions {
			transactions[i].ReceiptImageURL = resolveReceiptURL(r, transactions[i].ReceiptImageURL)
		}

		response := TransactionsListResponse{
			Transactions: transactions,
			Total:        total,
			Income:       totalIncome,
			Expense:      totalExpense,
		}

		// ✅ PERF: Store in Redis cache (2-min TTL — short enough for fresh data, long enough for page navigations)
		_ = utils.CacheSet(cacheKey, response, 2*time.Minute)

		utils.JSONResponse(w, http.StatusOK, response)

	case http.MethodPost:
		var req TransactionRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		parsedDate := time.Now()
		if req.Date != "" {
			if parsed, err := time.Parse(time.RFC3339, req.Date); err == nil {
				parsedDate = parsed
			} else if parsed, err := time.Parse("2006-01-02", req.Date); err == nil {
				parsedDate = parsed
			}
		}

		// Database transaction to ensure balance reconciliation consistency
		tx := database.DB.Begin()

		adminFee := 0.0
		if req.AdminFee != nil {
			adminFee = *req.AdminFee
		}

		// Reconcile Balance
		if err := services.AdjustBalances(tx, userID, req.AccountID, req.TransferToID, req.Type, req.Amount, adminFee, 1); err != nil {
			tx.Rollback()
			if strings.Contains(err.Error(), "missing") {
				utils.HandleBadRequest(w, err.Error())
			} else if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "Record NotFound") {
				utils.HandleNotFound(w, "Account")
			} else {
				utils.HandleDBError(w, err, "adjust balances")
			}
			return
		}

		// Sanitize receipt image URL to store relative path
		var receiptURL *string
		if req.ReceiptImageURL != nil && *req.ReceiptImageURL != "" {
			if idx := strings.Index(*req.ReceiptImageURL, "/uploads/"); idx != -1 {
				rel := (*req.ReceiptImageURL)[idx:]
				receiptURL = &rel
			} else {
				receiptURL = req.ReceiptImageURL
			}
		}

		transaction := database.Transaction{
			UserID:          userID,
			AccountID:       req.AccountID,
			CategoryID:      req.CategoryID,
			Type:            req.Type,
			Amount:          req.Amount,
			AdminFee:        adminFee,
			Description:     req.Description,
			Note:            req.Note,
			Date:            parsedDate,
			TransferToID:    req.TransferToID,
			ReceiptImageURL: receiptURL,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "record transaction")
			return
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "commit transaction creation")
			return
		}

		// Invalidate related caches after successful transaction
		_ = utils.CacheInvalidateUser(userID)
		utils.Log.Debug().Str("user_id", userID).Msg("Cache invalidated for user (transaction created)")

		// Preload relations for response
		database.DB.Preload("Category").Preload("Account").Preload("TransferTo").First(&transaction, "id = ?", transaction.ID)
		transaction.ReceiptImageURL = resolveReceiptURL(r, transaction.ReceiptImageURL)
		utils.JSONResponse(w, http.StatusCreated, transaction)

	case http.MethodDelete:
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if len(req.IDs) == 0 {
			utils.HandleBadRequest(w, "IDs list cannot be empty")
			return
		}

		tx := database.DB.Begin()

		// Fetch all transactions to be deleted to verify ownership and perform balance adjustments
		var transactions []database.Transaction
		if err := tx.Where("id IN ? AND user_id = ?", req.IDs, userID).Find(&transactions).Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "retrieve transactions for deletion")
			return
		}

		if len(transactions) == 0 {
			tx.Rollback()
			utils.HandleNotFound(w, "Matching transactions")
			return
		}

		// Adjust balances for each transaction (reversing their effects)
		for _, transaction := range transactions {
			if err := services.AdjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, -1); err != nil {
				tx.Rollback()
				utils.HandleDBError(w, err, "update balances during deletion")
				return
			}
		}

		// Perform bulk deletion
		if err := tx.Delete(&transactions).Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "delete transactions")
			return
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "commit transactions deletion")
			return
		}
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"message": fmt.Sprintf("Successfully deleted %d transactions", len(transactions)),
			"count":   len(transactions),
		})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// BulkRestoreTransactionsHandler restores multiple soft-deleted transactions
func BulkRestoreTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Invalid request body")
		return
	}

	if len(req.IDs) == 0 {
		utils.HandleBadRequest(w, "IDs list cannot be empty")
		return
	}

	tx := database.DB.Begin()

	var transactions []database.Transaction
	if err := tx.Unscoped().Where("id IN ? AND user_id = ? AND deleted_at IS NOT NULL", req.IDs, userID).Find(&transactions).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "retrieve transactions for restore")
		return
	}

	if len(transactions) == 0 {
		tx.Rollback()
		utils.HandleNotFound(w, "Matching transactions")
		return
	}

	// Reapply balance effects
	for _, transaction := range transactions {
		if err := services.AdjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, 1); err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "update balances during restore")
			return
		}
	}

	// Restore records
	if err := tx.Unscoped().Model(&database.Transaction{}).Where("id IN ? AND user_id = ?", req.IDs, userID).Update("deleted_at", nil).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "restore transactions")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit transactions restore")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("Successfully restored %d transactions", len(transactions)),
		"count":   len(transactions),
	})
}

// BulkEditTransactionsHandler bulk updates transaction categories/accounts
func BulkEditTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPut {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req struct {
		IDs        []string `json:"ids"`
		AccountID  string   `json:"accountId"`
		CategoryID *string  `json:"categoryId"`
	}
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Invalid request body")
		return
	}

	if len(req.IDs) == 0 {
		utils.HandleBadRequest(w, "IDs list cannot be empty")
		return
	}

	if req.AccountID == "" && req.CategoryID == nil {
		utils.HandleBadRequest(w, "Must specify accountId and/or categoryId to edit")
		return
	}

	tx := database.DB.Begin()

	var transactions []database.Transaction
	if err := tx.Where("id IN ? AND user_id = ?", req.IDs, userID).Find(&transactions).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "retrieve transactions for edit")
		return
	}

	if len(transactions) == 0 {
		tx.Rollback()
		utils.HandleNotFound(w, "Matching transactions")
		return
	}

	for _, transaction := range transactions {
		updated := false

		// 1. Bulk edit account and adjust balances
		if req.AccountID != "" && req.AccountID != transaction.AccountID {
			// Roll back old account balance
			if err := services.AdjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, -1); err != nil {
				tx.Rollback()
				utils.HandleDBError(w, err, "rollback balance for old account")
				return
			}
			// Apply new account balance
			if err := services.AdjustBalances(tx, userID, req.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, 1); err != nil {
				tx.Rollback()
				utils.HandleDBError(w, err, "apply balance for new account")
				return
			}
			transaction.AccountID = req.AccountID
			updated = true
		}

		// 2. Bulk edit category
		if req.CategoryID != nil {
			var newCatID *string
			if *req.CategoryID != "" && *req.CategoryID != "none" {
				newCatID = req.CategoryID
			}
			transaction.CategoryID = newCatID
			updated = true
		}

		if updated {
			transaction.UpdatedAt = time.Now()
			if err := tx.Save(&transaction).Error; err != nil {
				tx.Rollback()
				utils.HandleDBError(w, err, "save updated transaction")
				return
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit bulk edits")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("Successfully updated %d transactions", len(transactions)),
		"count":   len(transactions),
	})
}



