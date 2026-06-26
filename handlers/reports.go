package handlers

import (
	"net/http"
	"sort"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type ReportSummary struct {
	TotalIncome        float64 `json:"totalIncome"`
	TotalExpense       float64 `json:"totalExpense"`
	NetSavings         float64 `json:"netSavings"`
	SavingsRate        float64 `json:"savingsRate"`
	TotalTaxDeductible float64 `json:"totalTaxDeductible"`
}

type ReportBreakdownItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Amount     float64 `json:"amount"`
	Color      string  `json:"color"`
	Icon       string  `json:"icon"`
	Percentage float64 `json:"percentage"`
}

type ReportSeriesItem struct {
	Date    string  `json:"date"` // e.g. "2026-06"
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
	Savings float64 `json:"savings"`
}

type ReportsResponse struct {
	Summary   ReportSummary         `json:"summary"`
	Breakdown []ReportBreakdownItem `json:"breakdown"`
	Series    []ReportSeriesItem    `json:"series"`
}

// ReportsHandler handles dynamic user reporting queries
func ReportsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	// 1. Get query params
	startDateStr := r.URL.Query().Get("startDate")
	endDateStr := r.URL.Query().Get("endDate")
	reportType := r.URL.Query().Get("type") // "income", "expense", "all"
	groupBy := r.URL.Query().Get("groupBy")     // "category", "account"
	accountID := r.URL.Query().Get("accountId")
	categoryID := r.URL.Query().Get("categoryId")

	if groupBy == "" {
		groupBy = "category"
	}
	if reportType == "" {
		reportType = "all"
	}

	// 2. Parse dates
	var startDate, endDate time.Time
	var err error

	if startDateStr != "" {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			startDate, _ = time.Parse(time.RFC3339, startDateStr)
		}
	} else {
		startDate = time.Now().AddDate(0, 0, -30) // Default 30 days ago
	}

	if endDateStr != "" {
		endDate, err = time.Parse("2006-01-02", endDateStr)
		if err != nil {
			endDate, _ = time.Parse(time.RFC3339, endDateStr)
		}
	} else {
		endDate = time.Now()
	}

	// Make sure dates include start of day and end of day
	startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, startDate.Location())
	endDate = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 999999999, endDate.Location())

	// Cache lookup
	cacheKey := utils.BuildCacheKey("reports", userID, startDateStr, endDateStr, reportType, groupBy, accountID, categoryID)
	var cachedResponse ReportsResponse
	if err := utils.CacheGet(cacheKey, &cachedResponse); err == nil {
		utils.JSONResponse(w, http.StatusOK, cachedResponse)
		return
	}

	// 3. Query all transactions for user in range, preload Category and Account
	var transactions []database.Transaction
	query := database.DB.Preload("Category").Preload("Account").Preload("TransferTo").
		Where("user_id = ? AND date >= ? AND date <= ?", userID, startDate, endDate)

	if accountID != "" && accountID != "all" {
		query = query.Where("account_id = ? OR transfer_to_id = ?", accountID, accountID)
	}
	if categoryID != "" && categoryID != "all" {
		query = query.Where("category_id = ?", categoryID)
	}

	if err := query.Order("date asc").Find(&transactions).Error; err != nil {
		utils.HandleDBError(w, err, "fetch transactions for report")
		return
	}

	// 4. Aggregate report data in Go memory (100% dialect-safe & fast)
	var (
		totalIncome        float64
		totalExpense       float64
		totalTaxDeductible float64
	)

	// Maps for breakdown
	breakdownMap := make(map[string]*ReportBreakdownItem)
	// Maps for monthly series
	seriesMap := make(map[string]*ReportSeriesItem)

	for _, tx := range transactions {
		// Calculate tax deductibility:
		isTaxDeductible := tx.TaxDeductible || (tx.Category != nil && tx.Category.TaxDeductible)

		// Filter by reportType if applicable
		matchesType := true
		if reportType == "income" && tx.Type != database.TransactionTypeIncome {
			matchesType = false
		}
		if reportType == "expense" && tx.Type != database.TransactionTypeExpense {
			matchesType = false
		}

		// Update global totals (regardless of matchedType filter for netSavings calculation)
		if tx.Type == database.TransactionTypeIncome {
			totalIncome += tx.Amount
		} else if tx.Type == database.TransactionTypeExpense {
			totalExpense += tx.Amount + tx.AdminFee
		}

		if isTaxDeductible {
			if tx.Type == database.TransactionTypeExpense {
				totalTaxDeductible += tx.Amount + tx.AdminFee
			} else if tx.Type == database.TransactionTypeIncome {
				totalTaxDeductible += tx.Amount
			}
		}

		// Skip if doesn't match selected type for breakdown and series
		if !matchesType {
			continue
		}

		// Series key (YYYY-MM)
		monthKey := tx.Date.Format("2006-02")
		if _, exists := seriesMap[monthKey]; !exists {
			seriesMap[monthKey] = &ReportSeriesItem{
				Date: monthKey,
			}
		}
		if tx.Type == database.TransactionTypeIncome {
			seriesMap[monthKey].Income += tx.Amount
			seriesMap[monthKey].Savings += tx.Amount
		} else if tx.Type == database.TransactionTypeExpense {
			seriesMap[monthKey].Expense += tx.Amount + tx.AdminFee
			seriesMap[monthKey].Savings -= (tx.Amount + tx.AdminFee)
		}

		// Breakdown key
		var bID, bName, bColor, bIcon string
		if groupBy == "account" {
			if tx.Account != nil {
				bID = tx.Account.ID
				bName = tx.Account.Name
				bColor = tx.Account.Color
				bIcon = tx.Account.Icon
			} else {
				bID = "unknown"
				bName = "Akun Tidak Dikenal"
				bColor = "#8B949E"
				bIcon = "💳"
			}
		} else { // default category
			if tx.Category != nil {
				bID = tx.Category.ID
				bName = tx.Category.Name
				bColor = tx.Category.Color
				bIcon = tx.Category.Icon
			} else {
				bID = "uncategorized"
				if tx.Type == database.TransactionTypeTransfer {
					bName = "Transfer"
					bColor = "#388BFD"
					bIcon = "🔄"
				} else {
					bName = "Tanpa Kategori"
					bColor = "#8B949E"
					bIcon = "📦"
				}
			}
		}

		if item, exists := breakdownMap[bID]; exists {
			if tx.Type == database.TransactionTypeExpense {
				item.Amount += tx.Amount + tx.AdminFee
			} else {
				item.Amount += tx.Amount
			}
		} else {
			amount := tx.Amount
			if tx.Type == database.TransactionTypeExpense {
				amount += tx.AdminFee
			}
			breakdownMap[bID] = &ReportBreakdownItem{
				ID:     bID,
				Name:   bName,
				Amount: amount,
				Color:  bColor,
				Icon:   bIcon,
			}
		}
	}

	// Calculate percentages
	var breakdownTotal float64
	for _, item := range breakdownMap {
		breakdownTotal += item.Amount
	}

	breakdownList := make([]ReportBreakdownItem, 0, len(breakdownMap))
	for _, item := range breakdownMap {
		if breakdownTotal > 0 {
			item.Percentage = (item.Amount / breakdownTotal) * 100
		}
		breakdownList = append(breakdownList, *item)
	}

	// Sort breakdown descending by amount
	sort.Slice(breakdownList, func(i, j int) bool {
		return breakdownList[i].Amount > breakdownList[j].Amount
	})

	// Format monthly series
	seriesList := make([]ReportSeriesItem, 0, len(seriesMap))
	for _, item := range seriesMap {
		seriesList = append(seriesList, *item)
	}
	sort.Slice(seriesList, func(i, j int) bool {
		return seriesList[i].Date < seriesList[j].Date
	})

	netSavings := totalIncome - totalExpense
	savingsRate := 0.0
	if totalIncome > 0 {
		savingsRate = (netSavings / totalIncome) * 100
	}

	response := ReportsResponse{
		Summary: ReportSummary{
			TotalIncome:        totalIncome,
			TotalExpense:       totalExpense,
			NetSavings:         netSavings,
			SavingsRate:        savingsRate,
			TotalTaxDeductible: totalTaxDeductible,
		},
		Breakdown: breakdownList,
		Series:    seriesList,
	}

	// Invalidate after 5 mins
	_ = utils.CacheSet(cacheKey, response, 5*time.Minute)
	utils.JSONResponse(w, http.StatusOK, response)
}
