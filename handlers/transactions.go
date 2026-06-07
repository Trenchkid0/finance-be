package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type TransactionRequest struct {
	AccountID    string                   `json:"accountId"`
	CategoryID   *string                  `json:"categoryId"`
	Type         database.TransactionType `json:"type"`
	Amount       float64                  `json:"amount"`
	Description  string                   `json:"description"`
	Note         string                   `json:"note"`
	Date         string                   `json:"date"` // ISO string e.g. "2026-06-05T00:00:00Z"
	TransferToID *string                  `json:"transferToId"`
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

		if req.AccountID == "" || req.Type == "" || req.Amount <= 0 {
			utils.ErrorResponse(w, http.StatusBadRequest, "AccountId, Type, and positive Amount are required")
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

		// Verify account ownership
		var sourceAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", req.AccountID, userID).First(&sourceAcc).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusNotFound, "Source account not found")
			return
		}

		// Reconcile Balance
		switch req.Type {
		case database.TransactionTypeIncome:
			sourceAcc.Balance += req.Amount
			if err := tx.Save(&sourceAcc).Error; err != nil {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update account balance")
				return
			}
		case database.TransactionTypeExpense:
			sourceAcc.Balance -= req.Amount
			if err := tx.Save(&sourceAcc).Error; err != nil {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update account balance")
				return
			}
		case database.TransactionTypeTransfer:
			if req.TransferToID == nil || *req.TransferToID == "" {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusBadRequest, "TransferToId is required for transfer transactions")
				return
			}
			var destAcc database.FinanceAccount
			if err := tx.Where("id = ? AND user_id = ?", *req.TransferToID, userID).First(&destAcc).Error; err != nil {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusNotFound, "Destination account not found")
				return
			}
			sourceAcc.Balance -= req.Amount
			destAcc.Balance += req.Amount
			if err := tx.Save(&sourceAcc).Error; err != nil || tx.Save(&destAcc).Error != nil {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to reconcile transfer balances")
				return
			}
		}

		transaction := database.Transaction{
			ID:           uuid.New().String(),
			UserID:       userID,
			AccountID:    req.AccountID,
			CategoryID:   req.CategoryID,
			Type:         req.Type,
			Amount:       req.Amount,
			Description:  req.Description,
			Note:         req.Note,
			Date:         parsedDate,
			TransferToID: req.TransferToID,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to record transaction")
			return
		}

		tx.Commit()

		// Preload relations for response
		database.DB.Preload("Category").Preload("Account").Preload("TransferTo").First(&transaction, "id = ?", transaction.ID)
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
			if err := adjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, -1); err != nil {
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

// TransactionDetailHandler handles updating or deleting a single transaction
func TransactionDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	txID := r.PathValue("id")
	if txID == "" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing transaction ID")
		return
	}

	var transaction database.Transaction
	if err := database.DB.Where("id = ? AND user_id = ?", txID, userID).First(&transaction).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Transaction not found")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req TransactionRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		// Reconcile changes
		tx := database.DB.Begin()

		// 1. Rollback old transaction balance effects
		if err := adjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, -1); err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to roll back old transaction balances")
			return
		}

		// 2. Apply new transaction settings
		targetAccID := transaction.AccountID
		if req.AccountID != "" {
			targetAccID = req.AccountID
		}
		targetType := transaction.Type
		if req.Type != "" {
			targetType = req.Type
		}
		targetAmount := transaction.Amount
		if req.Amount > 0 {
			targetAmount = req.Amount
		}
		targetTransferToID := transaction.TransferToID
		if req.TransferToID != nil {
			targetTransferToID = req.TransferToID
		}

		if err := adjustBalances(tx, userID, targetAccID, targetTransferToID, targetType, targetAmount, 1); err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to reconcile new transaction balances")
			return
		}

		// 3. Update transaction struct
		transaction.AccountID = targetAccID
		transaction.Type = targetType
		transaction.Amount = targetAmount
		transaction.TransferToID = targetTransferToID
		transaction.CategoryID = req.CategoryID
		if req.Description != "" {
			transaction.Description = req.Description
		}
		transaction.Note = req.Note
		if req.Date != "" {
			if parsed, err := time.Parse(time.RFC3339, req.Date); err == nil {
				transaction.Date = parsed
			}
		}
		transaction.UpdatedAt = time.Now()

		if err := tx.Save(&transaction).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update transaction record")
			return
		}

		tx.Commit()

		// Preload relations for response
		database.DB.Preload("Category").Preload("Account").Preload("TransferTo").First(&transaction, "id = ?", transaction.ID)
		utils.JSONResponse(w, http.StatusOK, transaction)

	case http.MethodDelete:
		tx := database.DB.Begin()

		// Roll back balance effects before deleting
		if err := adjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, -1); err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update balances during deletion")
			return
		}

		if err := tx.Delete(&transaction).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to delete transaction")
			return
		}

		tx.Commit()
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Transaction deleted successfully"})

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ExportTransactionsHandler exports user transactions to CSV file
func ExportTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var transactions []database.Transaction
	err := database.DB.Model(&database.Transaction{}).
		Preload("Category").
		Preload("Account").
		Preload("TransferTo").
		Where("user_id = ?", userID).
		Order("date desc").
		Find(&transactions).Error

	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve transactions for export")
		return
	}

	// Set headers
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=transactions.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV header
	_ = writer.Write([]string{"Date", "Type", "Source Account", "Destination Account", "Category", "Amount", "Description", "Note"})

	for _, tx := range transactions {
		destAcc := ""
		if tx.TransferTo != nil {
			destAcc = tx.TransferTo.Name
		}
		catName := "Uncategorized"
		if tx.Category != nil {
			catName = tx.Category.Name
		}

		record := []string{
			tx.Date.Format("2006-01-02"),
			string(tx.Type),
			tx.Account.Name,
			destAcc,
			catName,
			fmt.Sprintf("%.2f", tx.Amount),
			tx.Description,
			tx.Note,
		}
		_ = writer.Write(record)
	}
}

// adjustBalances helper shifts the balance of accounts when transactions are deleted, created or changed.
// multiplier: +1 to apply transaction, -1 to roll back transaction
func adjustBalances(tx *gorm.DB, userID string, accountID string, transferToID *string, txType database.TransactionType, amount float64, multiplier float64) error {
	var sourceAcc database.FinanceAccount
	if err := tx.Where("id = ? AND user_id = ?", accountID, userID).First(&sourceAcc).Error; err != nil {
		return err
	}

	switch txType {
	case database.TransactionTypeIncome:
		sourceAcc.Balance += (amount * multiplier)
		return tx.Save(&sourceAcc).Error
	case database.TransactionTypeExpense:
		sourceAcc.Balance -= (amount * multiplier)
		return tx.Save(&sourceAcc).Error
	case database.TransactionTypeTransfer:
		if transferToID == nil || *transferToID == "" {
			return fmt.Errorf("transfer target account is missing")
		}
		var destAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *transferToID, userID).First(&destAcc).Error; err != nil {
			return err
		}
		sourceAcc.Balance -= (amount * multiplier)
		destAcc.Balance += (amount * multiplier)
		if err := tx.Save(&sourceAcc).Error; err != nil {
			return err
		}
		return tx.Save(&destAcc).Error
	}

	return nil
}
