package handlers

import (
	"fmt"
	"net/http"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

type BulkTransactionItem struct {
	AccountID       string                   `json:"accountId" validate:"required"`
	CategoryID      *string                  `json:"categoryId"`
	Type            database.TransactionType `json:"type" validate:"required,oneof=income|expense|transfer"`
	Amount          float64                  `json:"amount" validate:"required,min=0.01"`
	AdminFee        *float64                 `json:"adminFee"`
	Description     string                   `json:"description"`
	Note            string                   `json:"note"`
	Date            string                   `json:"date"` // e.g. "2026-06-05" or RFC3339
	TransferToID    *string                  `json:"transferToId"`
	ReceiptImageURL *string                  `json:"receiptImageUrl"`
}

type BulkCreateTransactionsRequest struct {
	Transactions []BulkTransactionItem `json:"transactions" validate:"required,dive"`
}

// BulkCreateTransactionsHandler creates multiple transactions in a single DB transaction
func BulkCreateTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	var req BulkCreateTransactionsRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	if len(req.Transactions) == 0 {
		utils.HandleBadRequest(w, "Harap berikan minimal satu transaksi.")
		return
	}

	// Fetch all user accounts to verify ownership
	var allAccounts []database.FinanceAccount
	database.DB.Where("user_id = ?", userID).Find(&allAccounts)
	accMap := make(map[string]bool, len(allAccounts))
	for _, a := range allAccounts {
		accMap[a.ID] = true
	}

	tx := database.DB.Begin()
	var batchTx []database.Transaction
	now := time.Now()

	for i, item := range req.Transactions {
		// Verify account ownership
		if !accMap[item.AccountID] {
			tx.Rollback()
			utils.HandleBadRequest(w, fmt.Sprintf("Transaksi ke-%d: Akun sumber tidak valid atau bukan milik Anda.", i+1))
			return
		}

		if item.Type == database.TransactionTypeTransfer && item.TransferToID != nil {
			if !accMap[*item.TransferToID] {
				tx.Rollback()
				utils.HandleBadRequest(w, fmt.Sprintf("Transaksi ke-%d: Akun tujuan transfer tidak valid atau bukan milik Anda.", i+1))
				return
			}
		}

		parsedDate := now
		if item.Date != "" {
			if parsed, err := time.Parse(time.RFC3339, item.Date); err == nil {
				parsedDate = parsed
			} else if parsed, err := time.Parse("2006-01-02", item.Date); err == nil {
				parsedDate = parsed
			}
		}

		adminFee := 0.0
		if item.AdminFee != nil {
			adminFee = *item.AdminFee
		}

		// Adjust Balance for this transaction
		if err := services.AdjustBalances(tx, userID, item.AccountID, item.TransferToID, item.Type, item.Amount, adminFee, 1); err != nil {
			tx.Rollback()
			utils.HandleBadRequest(w, fmt.Sprintf("Transaksi ke-%d: Gagal menyesuaikan saldo: %v", i+1, err))
			return
		}

		transaction := database.Transaction{
			UserID:          userID,
			AccountID:       item.AccountID,
			CategoryID:      item.CategoryID,
			Type:            item.Type,
			Amount:          item.Amount,
			AdminFee:        adminFee,
			Description:     item.Description,
			Note:            item.Note,
			Date:            parsedDate,
			TransferToID:    item.TransferToID,
			ReceiptImageURL: item.ReceiptImageURL,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		batchTx = append(batchTx, transaction)
	}

	// Batch insert all transactions
	if err := tx.CreateInBatches(&batchTx, 50).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "insert bulk transactions")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit bulk transactions")
		return
	}

	// Invalidate cache
	_ = utils.CacheInvalidateUser(userID)

	utils.JSONResponse(w, http.StatusCreated, map[string]interface{}{
		"ok":       true,
		"imported": len(batchTx),
	})
}
