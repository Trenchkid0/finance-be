package handlers

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type NetWorthPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type SankeyDatum struct {
	Name  string  `json:"name"`
	Side  string  `json:"side"` // "source" (inflow) or "target" (outflow)
	Value float64 `json:"value"`
	Color string  `json:"color"`
}

type CashflowSummary struct {
	Total   float64       `json:"total"`
	Inflow  []SankeyDatum `json:"inflow"`
	Outflow []SankeyDatum `json:"outflow"`
	Surplus float64       `json:"surplus"`
}

type DashboardSummaryResponse struct {
	NetWorthCurrent  float64                `json:"netWorthCurrent"`
	NetWorthPrevious float64                `json:"netWorthPrevious"`
	Period           string                 `json:"period"`
	NetWorthSeries   []NetWorthPoint        `json:"netWorthSeries"`
	Cashflow         CashflowSummary        `json:"cashflow"`
	Recent           []database.Transaction `json:"recent"`
}

// SummaryHandler aggregates data for the Dashboard page with Redis caching
func SummaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "30d"
	}
	cashflowPeriod := r.URL.Query().Get("cashflow_period")
	if cashflowPeriod == "" {
		cashflowPeriod = "30d"
	}

	// ✅ Try Redis cache first (3 minutes TTL)
	cacheKey := utils.BuildCacheKey("user", userID, "summary", period, cashflowPeriod)
	var cachedResponse DashboardSummaryResponse
	
	if err := utils.CacheGet(cacheKey, &cachedResponse); err == nil {
		// Cache HIT - return immediately
		fmt.Printf("💾 Cache HIT: %s\n", cacheKey)
		utils.JSONResponse(w, http.StatusOK, cachedResponse)
		return
	}
	
	// Cache MISS - fetch from database
	fmt.Printf("❌ Cache MISS: %s - fetching from DB\n", cacheKey)

	// 1. Fetch active accounts
	var accounts []database.FinanceAccount
	if err := database.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&accounts).Error; err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to fetch accounts")
		return
	}

	netWorthCurrent := 0.0
	for _, a := range accounts {
		netWorthCurrent += a.Balance
	}

	// 2. Fetch Net Worth time-series
	netWorthSeries := getNetWorthSeries(userID, period, netWorthCurrent)

	netWorthPrevious := netWorthCurrent
	if len(netWorthSeries) > 0 {
		netWorthPrevious = netWorthSeries[0].Value
	}

	// 3. Fetch Cashflow Sankey data
	cashflow := getCashflow(userID, cashflowPeriod)

	// 4. Fetch Recent Transactions (limit 8)
	var recent []database.Transaction
	database.DB.Model(&database.Transaction{}).
		Preload("Category").
		Preload("Account").
		Preload("TransferTo").
		Where("user_id = ?", userID).
		Order("date desc, created_at desc").
		Limit(8).
		Find(&recent)

	response := DashboardSummaryResponse{
		NetWorthCurrent:  netWorthCurrent,
		NetWorthPrevious: netWorthPrevious,
		Period:           period,
		NetWorthSeries:   netWorthSeries,
		Cashflow:         cashflow,
		Recent:           recent,
	}

	// ✅ Store in cache for 3 minutes
	_ = utils.CacheSet(cacheKey, response, 3*time.Minute)
	fmt.Printf("📦 Cached: %s (3 min TTL)\n", cacheKey)

	utils.JSONResponse(w, http.StatusOK, response)
}

// Period boundaries converter
func periodToRange(period string) (time.Time, time.Time) {
	now := time.Now()
	// Stagger end to end-of-day tomorrow to cover timezone drift
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	start := end

	switch period {
	case "1d":
		start = end.AddDate(0, 0, -1)
	case "7d":
		start = end.AddDate(0, 0, -7)
	case "30d":
		start = end.AddDate(0, 0, -30)
	case "90d":
		start = end.AddDate(0, 0, -90)
	case "ytd":
		start = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	case "365d":
		start = end.AddDate(0, 0, -365)
	case "5y":
		start = end.AddDate(-5, 0, 0)
	default:
		// Default 30d
		start = end.AddDate(0, 0, -30)
	}
	return start, end
}

func getNetWorthSeries(userID string, period string, currentNetWorth float64) []NetWorthPoint {
	start, end := periodToRange(period)

	var txs []database.Transaction
	database.DB.Where("user_id = ? AND date >= ? AND date < ?", userID, start, end).
		Order("date asc").
		Find(&txs)

	// Map to track sum of net worth changes per day
	deltaByDay := make(map[string]float64)
	for _, tx := range txs {
		key := tx.Date.Format("2006-01-02")
		delta := 0.0
		if tx.Type == database.TransactionTypeIncome {
			delta = tx.Amount
		} else if tx.Type == database.TransactionTypeExpense {
			delta = -tx.Amount
		}
		// Transfers don't change overall net worth
		deltaByDay[key] += delta
	}

	// Generate daily dates
	var points []NetWorthPoint
	cursor := start
	for cursor.Before(end) {
		points = append(points, NetWorthPoint{
			Date:  cursor.Format("2006-01-02"),
			Value: 0.0,
		})
		cursor = cursor.AddDate(0, 0, 1)
	}

	// Roll backwards starting from the current value
	if len(points) > 0 {
		points[len(points)-1].Value = currentNetWorth
		for i := len(points) - 2; i >= 0; i-- {
			nextDay := points[i+1].Date
			nextDelta := deltaByDay[nextDay]
			points[i].Value = points[i+1].Value - nextDelta
		}
	}

	return points
}

func getCashflow(userID string, period string) CashflowSummary {
	start, end := periodToRange(period)

	type GroupResult struct {
		Type       database.TransactionType
		CategoryID *string
		Total      float64
	}

	var results []GroupResult
	database.DB.Model(&database.Transaction{}).
		Select("type, category_id, SUM(amount) as total").
		Where("user_id = ? AND type IN (?, ?) AND date >= ? AND date < ?", userID, database.TransactionTypeIncome, database.TransactionTypeExpense, start, end).
		Group("type, category_id").
		Scan(&results)

	// Get categories
	var categoryIDs []string
	for _, r := range results {
		if r.CategoryID != nil && *r.CategoryID != "" {
			categoryIDs = append(categoryIDs, *r.CategoryID)
		}
	}

	categoryMeta := make(map[string]database.Category)
	if len(categoryIDs) > 0 {
		var categories []database.Category
		database.DB.Where("id IN ?", categoryIDs).Find(&categories)
		for _, c := range categories {
			categoryMeta[c.ID] = c
		}
	}

	var inflow []SankeyDatum
	var outflow []SankeyDatum
	totalIn := 0.0
	totalOut := 0.0

	for _, g := range results {
		if g.Total <= 0 {
			continue
		}

		name := "Lainnya"
		color := "#8B949E"

		if g.CategoryID != nil {
			if cat, found := categoryMeta[*g.CategoryID]; found {
				name = cat.Name
				color = cat.Color
			}
		}

		if name == "Lainnya" {
			if g.Type == database.TransactionTypeIncome {
				name = "Pemasukan"
				color = "#2EA043"
			} else {
				color = "#F85149"
			}
		}

		datum := SankeyDatum{
			Name:  name,
			Value: g.Total,
			Color: color,
		}

		if g.Type == database.TransactionTypeIncome {
			datum.Side = "source"
			inflow = append(inflow, datum)
			totalIn += g.Total
		} else {
			datum.Side = "target"
			outflow = append(outflow, datum)
			totalOut += g.Total
		}
	}

	// Sort biggest first
	sort.Slice(inflow, func(i, j int) bool {
		return inflow[i].Value > inflow[j].Value
	})
	sort.Slice(outflow, func(i, j int) bool {
		return outflow[i].Value > outflow[j].Value
	})

	surplus := math.Max(0, totalIn-totalOut)

	return CashflowSummary{
		Total:   totalIn,
		Inflow:  inflow,
		Outflow: outflow,
		Surplus: surplus,
	}
}
