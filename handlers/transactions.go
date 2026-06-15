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
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
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
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to count transactions")
			return
		}

		// Aggregate income and expense sums
		type SumResult struct {
			Type string  `gorm:"column:type"`
			Sum  float64 `gorm:"column:sum"`
		}
		var sumResults []SumResult
		if err := baseQuery.Session(&gorm.Session{}).Select("type, SUM(amount) as sum").Group("type").Scan(&sumResults).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to aggregate transactions")
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
		offset := 0
		if offsetStr != "" {
			if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
				offset = parsed
			}
		}

		var transactions []database.Transaction
		if err := baseQuery.Session(&gorm.Session{}).Preload("Category").Preload("Account").Preload("TransferTo").
			Order("date desc, created_at desc").Limit(limit).Offset(offset).Find(&transactions).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve transactions")
			return
		}

		for i := range transactions {
			transactions[i].ReceiptImageURL = resolveReceiptURL(r, transactions[i].ReceiptImageURL)
		}

		utils.JSONResponse(w, http.StatusOK, TransactionsListResponse{
			Transactions: transactions,
			Total:        total,
			Income:       totalIncome,
			Expense:      totalExpense,
		})

	case http.MethodPost:
		var req TransactionRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
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
		if err := adjustBalances(tx, userID, req.AccountID, req.TransferToID, req.Type, req.Amount, adminFee, 1); err != nil {
			tx.Rollback()
			if strings.Contains(err.Error(), "missing") {
				utils.ErrorResponse(w, http.StatusBadRequest, err.Error())
			} else if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "Record NotFound") {
				utils.ErrorResponse(w, http.StatusNotFound, err.Error())
			} else {
				utils.ErrorResponse(w, http.StatusInternalServerError, err.Error())
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
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to record transaction")
			return
		}

		tx.Commit()

		// ✅ Invalidate related caches after successful transaction
		_ = utils.CacheInvalidateUser(userID)
		fmt.Printf("🔄 Cache invalidated for user: %s (transaction created)\n", userID)

		// Preload relations for response
		database.DB.Preload("Category").Preload("Account").Preload("TransferTo").First(&transaction, "id = ?", transaction.ID)
		transaction.ReceiptImageURL = resolveReceiptURL(r, transaction.ReceiptImageURL)
		utils.JSONResponse(w, http.StatusCreated, transaction)

	case http.MethodDelete:
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if len(req.IDs) == 0 {
			utils.ErrorResponse(w, http.StatusBadRequest, "IDs list cannot be empty")
			return
		}

		tx := database.DB.Begin()

		// Fetch all transactions to be deleted to verify ownership and perform balance adjustments
		var transactions []database.Transaction
		if err := tx.Where("id IN ? AND user_id = ?", req.IDs, userID).Find(&transactions).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve transactions for deletion")
			return
		}

		if len(transactions) == 0 {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusNotFound, "No matching transactions found")
			return
		}

		// Adjust balances for each transaction (reversing their effects)
		for _, transaction := range transactions {
			if err := adjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, -1); err != nil {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update balances during deletion")
				return
			}
		}

		// Perform bulk deletion
		if err := tx.Delete(&transactions).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to delete transactions")
			return
		}

		tx.Commit()
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"message": fmt.Sprintf("Successfully deleted %d transactions", len(transactions)),
			"count":   len(transactions),
		})

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// adjustBalances helper shifts the balance of accounts when transactions are deleted, created or changed.
// multiplier: +1 to apply transaction, -1 to roll back transaction
func adjustBalances(tx *gorm.DB, userID string, accountID string, transferToID *string, txType database.TransactionType, amount float64, adminFee float64, multiplier float64) error {
	var sourceAcc database.FinanceAccount
	if err := tx.Where("id = ? AND user_id = ?", accountID, userID).First(&sourceAcc).Error; err != nil {
		return err
	}

	switch txType {
	case database.TransactionTypeIncome:
		sourceAcc.Balance += ((amount - adminFee) * multiplier)
		return tx.Save(&sourceAcc).Error
	case database.TransactionTypeExpense:
		sourceAcc.Balance -= ((amount + adminFee) * multiplier)
		return tx.Save(&sourceAcc).Error
	case database.TransactionTypeTransfer:
		if transferToID == nil || *transferToID == "" {
			return fmt.Errorf("transfer target account is missing")
		}
		var destAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *transferToID, userID).First(&destAcc).Error; err != nil {
			return err
		}
		sourceAcc.Balance -= ((amount + adminFee) * multiplier)
		destAcc.Balance += (amount * multiplier)
		if err := tx.Save(&sourceAcc).Error; err != nil {
			return err
		}
		return tx.Save(&destAcc).Error
	}

	return nil
}

