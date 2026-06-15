package handlers

import (
	"fmt"
	"net/http"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type BuyAssetRequest struct {
	AccountID           string   `json:"accountId" validate:"required"`
	Symbol              string   `json:"symbol" validate:"required"`
	Name                string   `json:"name" validate:"required"`
	Quantity            float64  `json:"quantity" validate:"required,min=0.01"`
	Price               float64  `json:"price" validate:"required,min=0.01"`
	AdminFee            *float64 `json:"adminFee"`
	DeductFromAccountID *string  `json:"deductFromAccountId"` // Optional
}

type SellAssetRequest struct {
	HoldingID      string  `json:"holdingId" validate:"required"`
	Quantity       float64 `json:"quantity" validate:"required,min=0.01"`
	Price          float64 `json:"price" validate:"required,min=0.01"`
	AddToAccountID *string `json:"addToAccountId"` // Optional
}

type UpdatePriceRequest struct {
	HoldingID    string  `json:"holdingId" validate:"required"`
	CurrentPrice float64 `json:"currentPrice" validate:"required,min=0.01"`
}

// InvestmentsHandler returns all holdings for the authenticated user
func InvestmentsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var holdings []database.AssetHolding
	if err := database.DB.Preload("Account").Where("user_id = ?", userID).Find(&holdings).Error; err != nil {
		utils.HandleDBError(w, err, "retrieve holdings")
		return
	}

	utils.JSONResponse(w, http.StatusOK, holdings)
}

func formatCurrencySimple(val float64) string {
	return fmt.Sprintf("Rp %.0f", val)
}
