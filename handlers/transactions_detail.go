package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

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
		if req.ReceiptImageURL != nil {
			var receiptURL *string
			if *req.ReceiptImageURL != "" {
				if idx := strings.Index(*req.ReceiptImageURL, "/uploads/"); idx != -1 {
					rel := (*req.ReceiptImageURL)[idx:]
					receiptURL = &rel
				} else {
					receiptURL = req.ReceiptImageURL
				}
			}
			transaction.ReceiptImageURL = receiptURL
		}
		transaction.UpdatedAt = time.Now()

		if err := tx.Save(&transaction).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update transaction record")
			return
		}

		tx.Commit()

		// ✅ Invalidate related caches after successful update
		_ = utils.CacheInvalidateUser(userID)
		fmt.Printf("🔄 Cache invalidated for user: %s (transaction updated)\n", userID)

		// Preload relations for response
		database.DB.Preload("Category").Preload("Account").Preload("TransferTo").First(&transaction, "id = ?", transaction.ID)
		transaction.ReceiptImageURL = resolveReceiptURL(r, transaction.ReceiptImageURL)
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

		// ✅ Invalidate related caches after successful deletion
		_ = utils.CacheInvalidateUser(userID)
		fmt.Printf("🔄 Cache invalidated for user: %s (transaction deleted)\n", userID)
		
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Transaction deleted successfully"})

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
