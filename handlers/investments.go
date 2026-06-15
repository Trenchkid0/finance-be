package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type BuyAssetRequest struct {
	AccountID           string   `json:"accountId"`
	Symbol              string   `json:"symbol"`
	Name                string   `json:"name"`
	Quantity            float64  `json:"quantity"`
	Price               float64  `json:"price"`
	AdminFee            *float64 `json:"adminFee"`
	DeductFromAccountID *string  `json:"deductFromAccountId"` // Optional
}

type SellAssetRequest struct {
	HoldingID      string  `json:"holdingId"`
	Quantity       float64 `json:"quantity"`
	Price          float64 `json:"price"`
	AddToAccountID *string `json:"addToAccountId"` // Optional
}

type UpdatePriceRequest struct {
	HoldingID    string  `json:"holdingId"`
	CurrentPrice float64 `json:"currentPrice"`
}

// InvestmentsHandler returns all holdings for the authenticated user
func InvestmentsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if r.Method != http.MethodGet {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var holdings []database.AssetHolding
	if err := database.DB.Preload("Account").Where("user_id = ?", userID).Find(&holdings).Error; err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve holdings")
		return
	}

	utils.JSONResponse(w, http.StatusOK, holdings)
}

// BuyAssetHandler handles purchasing an asset and updating holding
func BuyAssetHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req BuyAssetRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.AccountID == "" || req.Symbol == "" || req.Name == "" || req.Quantity <= 0 || req.Price <= 0 {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing required fields or invalid values")
		return
	}

	tx := database.DB.Begin()

	// 1. Optional: Deduct balance from account
	if req.DeductFromAccountID != nil && *req.DeductFromAccountID != "" {
		adminFee := 0.0
		if req.AdminFee != nil {
			adminFee = *req.AdminFee
		}
		totalCost := req.Quantity * req.Price
		
		// Find deduct account
		var deductAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *req.DeductFromAccountID, userID).First(&deductAcc).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusBadRequest, "Deduct account not found")
			return
		}

		if err := adjustBalances(tx, userID, *req.DeductFromAccountID, nil, database.TransactionTypeExpense, totalCost, adminFee, 1); err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to adjust balance")
			return
		}

		// Find Investment Category if exists
		var catID *string
		var invCategory database.Category
		if err := tx.Where("name LIKE ? AND type = ?", "%Investasi%", "expense").First(&invCategory).Error; err == nil {
			catID = &invCategory.ID
		}

		// Create Transaction record
		transaction := database.Transaction{
			UserID:      userID,
			AccountID:   *req.DeductFromAccountID,
			CategoryID:  catID,
			Type:        database.TransactionTypeExpense,
			Amount:      totalCost,
			AdminFee:    adminFee,
			Description: fmt.Sprintf("Beli Aset: %s (%s)", req.Name, req.Symbol),
			Note:        fmt.Sprintf("Pembelian %g unit @ %s", req.Quantity, formatCurrencySimple(req.Price)),
			Date:        time.Now(),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to record purchase transaction")
			return
		}
	}

	// 2. Fetch existing holding for symbol in this investment account
	var holding database.AssetHolding
	err := tx.Where("user_id = ? AND account_id = ? AND symbol = ?", userID, req.AccountID, req.Symbol).First(&holding).Error

	if err == nil {
		// Existing holding: update average cost (BuyPrice) and quantity
		newQty := holding.Quantity + req.Quantity
		newAvgPrice := ((holding.Quantity * holding.BuyPrice) + (req.Quantity * req.Price)) / newQty
		
		holding.Quantity = newQty
		holding.BuyPrice = newAvgPrice
		holding.CurrentPrice = req.Price // Update current price to latest buy price
		holding.UpdatedAt = time.Now()

		if err := tx.Save(&holding).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update asset holding")
			return
		}
	} else {
		// New holding: create holding record
		holding = database.AssetHolding{
			ID:           uuid.New().String(),
			UserID:       userID,
			AccountID:    req.AccountID,
			Symbol:       req.Symbol,
			Name:         req.Name,
			Quantity:     req.Quantity,
			BuyPrice:     req.Price,
			CurrentPrice: req.Price,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		if err := tx.Create(&holding).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create asset holding")
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to commit changes")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, holding)
}

// SellAssetHandler handles selling a portion or all of a holding
func SellAssetHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req SellAssetRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.HoldingID == "" || req.Quantity <= 0 || req.Price <= 0 {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing required fields or invalid values")
		return
	}

	tx := database.DB.Begin()

	// 1. Get holding
	var holding database.AssetHolding
	if err := tx.Where("id = ? AND user_id = ?", req.HoldingID, userID).First(&holding).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusNotFound, "Holding not found")
		return
	}

	if req.Quantity > holding.Quantity {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusBadRequest, "Jumlah penjualan melebihi jumlah kepemilikan")
		return
	}

	// 2. Add cash to account if requested
	if req.AddToAccountID != nil && *req.AddToAccountID != "" {
		totalRevenue := req.Quantity * req.Price
		
		// Find receive account
		var receiveAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *req.AddToAccountID, userID).First(&receiveAcc).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusBadRequest, "Receive account not found")
			return
		}

		if err := adjustBalances(tx, userID, *req.AddToAccountID, nil, database.TransactionTypeIncome, totalRevenue, 0, 1); err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to adjust balance")
			return
		}

		// Find Investment Category if exists
		var catID *string
		var invCategory database.Category
		if err := tx.Where("name LIKE ? AND type = ?", "%Investasi%", "expense").First(&invCategory).Error; err == nil {
			// Even though it's income, standard categories are shared or we can link it
			catID = &invCategory.ID
		}

		// Create Transaction record (Income)
		transaction := database.Transaction{
			UserID:      userID,
			AccountID:   *req.AddToAccountID,
			CategoryID:  catID,
			Type:        database.TransactionTypeIncome,
			Amount:      totalRevenue,
			Description: fmt.Sprintf("Jual Aset: %s (%s)", holding.Name, holding.Symbol),
			Note:        fmt.Sprintf("Penjualan %g unit @ %s", req.Quantity, formatCurrencySimple(req.Price)),
			Date:        time.Now(),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to record sale transaction")
			return
		}
	}

	// 3. Update quantity or delete holding
	remainingQty := holding.Quantity - req.Quantity
	if remainingQty <= 0.0001 {
		// Delete holding if quantity is 0
		if err := tx.Delete(&holding).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to remove asset holding")
			return
		}
	} else {
		holding.Quantity = remainingQty
		holding.CurrentPrice = req.Price // Update current price
		holding.UpdatedAt = time.Now()

		if err := tx.Save(&holding).Error; err != nil {
			tx.Rollback()
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update asset holding")
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to commit changes")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Penjualan aset berhasil dicatat"})
}

// UpdatePriceHandler updates the current price of a holding
func UpdatePriceHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req UpdatePriceRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.HoldingID == "" || req.CurrentPrice <= 0 {
		utils.ErrorResponse(w, http.StatusBadRequest, "HoldingID and valid CurrentPrice are required")
		return
	}

	var holding database.AssetHolding
	if err := database.DB.Where("id = ? AND user_id = ?", req.HoldingID, userID).First(&holding).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Holding not found")
		return
	}

	holding.CurrentPrice = req.CurrentPrice
	holding.UpdatedAt = time.Now()

	if err := database.DB.Save(&holding).Error; err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update price")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, holding)
}

func formatCurrencySimple(val float64) string {
	return fmt.Sprintf("Rp %.0f", val)
}
