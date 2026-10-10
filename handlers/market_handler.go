package handlers

import (
	"net/http"

	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

// MarketOverviewHandler handles GET /api/market/overview
func MarketOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	data, err := services.GetMarketOverview()
	if err != nil {
		utils.HandleDBError(w, err, "fetch market overview")
		return
	}

	utils.JSONResponse(w, http.StatusOK, data)
}

// MarketChartHandler handles GET /api/market/chart?symbol=BBCA.JK&range=1y
func MarketChartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		utils.HandleBadRequest(w, "symbol parameter is required")
		return
	}

	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "1y"
	}

	data, err := services.GetAssetChart(symbol, rangeStr)
	if err != nil {
		utils.HandleDBError(w, err, "fetch asset chart")
		return
	}

	utils.JSONResponse(w, http.StatusOK, data)
}

// MarketSearchHandler handles GET /api/market/search?q=TLKM
func MarketSearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		utils.JSONResponse(w, http.StatusOK, []interface{}{})
		return
	}

	results, err := services.SearchYahooAssets(q)
	if err != nil {
		utils.HandleDBError(w, err, "search assets")
		return
	}

	utils.JSONResponse(w, http.StatusOK, results)
}
