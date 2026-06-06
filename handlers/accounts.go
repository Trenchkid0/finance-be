package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type AccountRequest struct {
	Name     string               `json:"name"`
	Type     database.AccountType `json:"type"`
	Balance  float64              `json:"balance"`
	Currency string               `json:"currency"`
	Icon     string               `json:"icon"`
	Color    string               `json:"color"`
	IsActive *bool                `json:"isActive"`
}

type AccountListItemResponse struct {
	database.FinanceAccount
	TransactionCount int64 `json:"transactionCount"`
}

// AccountsHandler routes account list and creation
func AccountsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		var accounts []database.FinanceAccount

		query := database.DB.Where("user_id = ?", userID)
		if status != "all" {
			query = query.Where("is_active = ?", true)
		}

		if err := query.Order("is_active desc, name asc").Find(&accounts).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve accounts")
			return
		}

		response := make([]AccountListItemResponse, 0, len(accounts))
		for _, a := range accounts {
			var txCount int64
			var tfCount int64
			database.DB.Model(&database.Transaction{}).Where("account_id = ?", a.ID).Count(&txCount)
			database.DB.Model(&database.Transaction{}).Where("transfer_to_id = ?", a.ID).Count(&tfCount)

			response = append(response, AccountListItemResponse{
				FinanceAccount:   a,
				TransactionCount: txCount + tfCount,
			})
		}

		utils.JSONResponse(w, http.StatusOK, response)

	case http.MethodPost:
		var req AccountRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.Name == "" || req.Type == "" {
			utils.ErrorResponse(w, http.StatusBadRequest, "Name and Type are required")
			return
		}

		currency := req.Currency
		if currency == "" {
			currency = "IDR"
		}

		isActive := true
		if req.IsActive != nil {
			isActive = *req.IsActive
		}

		account := database.FinanceAccount{
			ID:        uuid.New().String(),
			UserID:    userID,
			Name:      req.Name,
			Type:      req.Type,
			Balance:   req.Balance,
			Currency:  currency,
			Icon:      req.Icon,
			Color:     req.Color,
			IsActive:  isActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := database.DB.Create(&account).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create account")
			return
		}
		utils.JSONResponse(w, http.StatusCreated, account)

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// AccountDetailHandler handles CRUD operations on a single account
func AccountDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Path parameter in Go 1.22+
	accountID := r.PathValue("id")
	if accountID == "" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing account ID")
		return
	}

	// Find the account and ensure ownership
	var account database.FinanceAccount
	if err := database.DB.Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Account not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		utils.JSONResponse(w, http.StatusOK, account)

	case http.MethodPut:
		var req AccountRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.Name != "" {
			account.Name = req.Name
		}
		if req.Type != "" {
			account.Type = req.Type
		}
		account.Balance = req.Balance
		if req.Currency != "" {
			account.Currency = req.Currency
		}
		if req.Icon != "" {
			account.Icon = req.Icon
		}
		if req.Color != "" {
			account.Color = req.Color
		}
		if req.IsActive != nil {
			account.IsActive = *req.IsActive
		}
		account.UpdatedAt = time.Now()

		if err := database.DB.Save(&account).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update account")
			return
		}
		utils.JSONResponse(w, http.StatusOK, account)

	case http.MethodDelete:
		// GORM will automatically delete Cascade because of the constraint in models if set up.
		// Alternatively, we can let GORM delete the account. GORM Cascade delete needs DB support,
		// SQLite foreign keys default to off unless enabled. We'll delete related transactions manually or rely on GORM.
		// Let's delete related transactions to be safe and robust across database providers.
		tx := database.DB.Begin()
		if err := tx.Where("account_id = ? OR transfer_to_id = ?", accountID, accountID).Delete(&database.Transaction{}).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to delete associated transactions")
			return
		}

		if err := tx.Delete(&account).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to delete account")
			return
		}

		tx.Commit()
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Account deleted successfully"})

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
