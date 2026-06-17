package handlers

import (
	"net/http"
	"strings"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

// TransactionDetailHandler handles updating or deleting a single transaction
func TransactionDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	txID := r.PathValue("id")
	if txID == "" {
		utils.HandleBadRequest(w, "ID transaksi tidak ditemukan.")
		return
	}

	var transaction database.Transaction
	if err := database.DB.Where("id = ? AND user_id = ?", txID, userID).First(&transaction).Error; err != nil {
		utils.HandleNotFound(w, "Transaksi")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req TransactionRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		// Reconcile changes
		tx := database.DB.Begin()

		// 1. Rollback old transaction balance effects
		if err := services.AdjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, -1); err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "roll back old transaction balances")
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
		targetAdminFee := transaction.AdminFee
		if req.AdminFee != nil {
			targetAdminFee = *req.AdminFee
		}

		if err := services.AdjustBalances(tx, userID, targetAccID, targetTransferToID, targetType, targetAmount, targetAdminFee, 1); err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "reconcile new transaction balances")
			return
		}

		// 3. Update transaction struct
		transaction.AccountID = targetAccID
		transaction.Type = targetType
		transaction.Amount = targetAmount
		transaction.AdminFee = targetAdminFee
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
			utils.HandleDBError(w, err, "update transaction record")
			return
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "commit transaction update")
			return
		}

		// Invalidate related caches after successful update
		_ = utils.CacheInvalidateUser(userID)
		utils.Log.Debug().Str("user_id", userID).Msg("Cache invalidated for user (transaction updated)")

		// Preload relations for response
		database.DB.Preload("Category").Preload("Account").Preload("TransferTo").First(&transaction, "id = ?", transaction.ID)
		transaction.ReceiptImageURL = resolveReceiptURL(r, transaction.ReceiptImageURL)
		utils.JSONResponse(w, http.StatusOK, transaction)

	case http.MethodDelete:
		tx := database.DB.Begin()

		// Roll back balance effects before deleting
		if err := services.AdjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, -1); err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "update balances during deletion")
			return
		}

		if err := tx.Delete(&transaction).Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "delete transaction")
			return
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "commit transaction deletion")
			return
		}

		// Invalidate related caches after successful deletion
		_ = utils.CacheInvalidateUser(userID)
		utils.Log.Debug().Str("user_id", userID).Msg("Cache invalidated for user (transaction deleted)")
		
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Transaction deleted successfully"})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// RestoreTransactionHandler restores a soft-deleted transaction
func RestoreTransactionHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	txID := r.PathValue("id")
	if txID == "" {
		utils.HandleBadRequest(w, "ID transaksi tidak ditemukan.")
		return
	}

	var transaction database.Transaction
	if err := database.DB.Unscoped().Where("id = ? AND user_id = ?", txID, userID).First(&transaction).Error; err != nil {
		utils.HandleNotFound(w, "Transaksi")
		return
	}

	if !transaction.DeletedAt.Valid {
		utils.ErrorResponse(w, http.StatusBadRequest, "Transaction is not deleted")
		return
	}

	tx := database.DB.Begin()

	// Reapply balance effects
	if err := services.AdjustBalances(tx, userID, transaction.AccountID, transaction.TransferToID, transaction.Type, transaction.Amount, transaction.AdminFee, 1); err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "apply balances during restore")
		return
	}

	// Clear DeletedAt
	if err := tx.Unscoped().Model(&transaction).Update("deleted_at", nil).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "restore transaction record")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit transaction restore")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Transaction restored successfully"})
}
