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

// BuyAssetHandler handles purchasing an asset and updating holding
func BuyAssetHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req BuyAssetRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Invalid request body")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
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
			utils.HandleNotFound(w, "Deduct account")
			return
		}

		if err := adjustBalances(tx, userID, *req.DeductFromAccountID, nil, database.TransactionTypeExpense, totalCost, adminFee, 1); err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "adjust balance for purchase")
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
			utils.HandleDBError(w, err, "record purchase transaction")
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
			utils.HandleDBError(w, err, "update asset holding")
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
			utils.HandleDBError(w, err, "create asset holding")
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit purchase changes")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, holding)
}

// SellAssetHandler handles selling a portion or all of a holding
func SellAssetHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req SellAssetRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Invalid request body")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	tx := database.DB.Begin()

	// 1. Get holding
	var holding database.AssetHolding
	if err := tx.Where("id = ? AND user_id = ?", req.HoldingID, userID).First(&holding).Error; err != nil {
		tx.Rollback()
		utils.HandleNotFound(w, "Holding")
		return
	}

	if req.Quantity > holding.Quantity {
		tx.Rollback()
		utils.HandleBadRequest(w, "Jumlah penjualan melebihi jumlah kepemilikan")
		return
	}

	// 2. Add cash to account if requested
	if req.AddToAccountID != nil && *req.AddToAccountID != "" {
		totalRevenue := req.Quantity * req.Price
		
		// Find receive account
		var receiveAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *req.AddToAccountID, userID).First(&receiveAcc).Error; err != nil {
			tx.Rollback()
			utils.HandleNotFound(w, "Receive account")
			return
		}

		if err := adjustBalances(tx, userID, *req.AddToAccountID, nil, database.TransactionTypeIncome, totalRevenue, 0, 1); err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "adjust balance for sale")
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
			utils.HandleDBError(w, err, "record sale transaction")
			return
		}
	}

	// 3. Update quantity or delete holding
	remainingQty := holding.Quantity - req.Quantity
	if remainingQty <= 0.0001 {
		// Delete holding if quantity is 0
		if err := tx.Delete(&holding).Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "remove asset holding")
			return
		}
	} else {
		holding.Quantity = remainingQty
		holding.CurrentPrice = req.Price // Update current price
		holding.UpdatedAt = time.Now()

		if err := tx.Save(&holding).Error; err != nil {
			tx.Rollback()
			utils.HandleDBError(w, err, "update asset holding")
			return
		}
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit sale changes")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Penjualan aset berhasil dicatat"})
}

// UpdatePriceHandler updates the current price of a holding
func UpdatePriceHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req UpdatePriceRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Invalid request body")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	var holding database.AssetHolding
	if err := database.DB.Where("id = ? AND user_id = ?", req.HoldingID, userID).First(&holding).Error; err != nil {
		utils.HandleNotFound(w, "Holding")
		return
	}

	holding.CurrentPrice = req.CurrentPrice
	holding.UpdatedAt = time.Now()

	if err := database.DB.Save(&holding).Error; err != nil {
		utils.HandleDBError(w, err, "update price")
		return
	}

	_ = utils.CacheInvalidateUser(userID)
	utils.JSONResponse(w, http.StatusOK, holding)
}
