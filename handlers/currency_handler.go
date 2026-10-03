package handlers

import (
	"net/http"

	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

// CurrencyRatesHandler handles GET /api/currency/rates?base=IDR
func CurrencyRatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	base := r.URL.Query().Get("base")
	if base == "" {
		base = "IDR"
	}

	rates, err := services.GetExchangeRates(base)
	if err != nil {
		utils.HandleDBError(w, err, "fetch exchange rates")
		return
	}

	utils.JSONResponse(w, http.StatusOK, rates)
}
