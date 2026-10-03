package handlers

import (
	"net/http"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type CategoryHighlight struct {
	CategoryID string  `json:"categoryId"`
	Name       string  `json:"name"`
	Icon       string  `json:"icon"`
	Color      string  `json:"color"`
	Amount     float64 `json:"amount"`
	Percentage float64 `json:"percentage"`
}

type DayHighlight struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
}

type MonthlyWrappedResponse struct {
	MonthName       string              `json:"monthName"`
	Year            int                 `json:"year"`
	TotalIncome     float64             `json:"totalIncome"`
	TotalExpense    float64             `json:"totalExpense"`
	TotalSaved      float64             `json:"totalSaved"`
	SavingsRate     float64             `json:"savingsRate"`
	TopCategory     *CategoryHighlight  `json:"topCategory,omitempty"`
	BiggestDay      *DayHighlight       `json:"biggestDay,omitempty"`
	TotalCount      int                 `json:"totalCount"`
	PersonalityBadge string             `json:"personalityBadge"`
	BadgeDescription string             `json:"badgeDescription"`
}

// MonthlyWrappedHandler handles GET /api/reports/monthly-wrapped?month=2026-07
func MonthlyWrappedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	monthStr := r.URL.Query().Get("month")
	now := time.Now()
	if monthStr == "" {
		monthStr = now.Format("2006-01")
	}

	parsedTime, err := time.Parse("2006-01", monthStr)
	if err != nil {
		parsedTime = now
		monthStr = now.Format("2006-01")
	}

	// Fetch all transactions for the specified user and month
	startOfMonth := time.Date(parsedTime.Year(), parsedTime.Month(), 1, 0, 0, 0, 0, time.Local)
	endOfMonth := startOfMonth.AddDate(0, 1, 0).Add(-time.Nanosecond)

	var txs []database.Transaction
	if err := database.DB.Preload("Category").Where("user_id = ? AND date >= ? AND date <= ?", userID, startOfMonth, endOfMonth).Find(&txs).Error; err != nil {
		utils.HandleDBError(w, err, "fetch monthly transactions for wrapped")
		return
	}

	var totalIncome, totalExpense float64
	catTotals := make(map[string]*CategoryHighlight)
	dayTotals := make(map[string]float64)

	for _, tx := range txs {
		dateStr := tx.Date.Format("2006-01-02")
		switch tx.Type {
		case database.TransactionTypeIncome:
			totalIncome += tx.Amount - tx.AdminFee
		case database.TransactionTypeExpense:
			totalExpense += tx.Amount + tx.AdminFee
			dayTotals[dateStr] += tx.Amount + tx.AdminFee

			if tx.CategoryID != nil && *tx.CategoryID != "" {
				catID := *tx.CategoryID
				if _, exists := catTotals[catID]; !exists {
					name := "Lainnya"
					icon := "📦"
					color := "#6B7280"
					if tx.Category != nil {
						name = tx.Category.Name
						if tx.Category.Icon != "" {
							icon = tx.Category.Icon
						}
						if tx.Category.Color != "" {
							color = tx.Category.Color
						}
					}
					catTotals[catID] = &CategoryHighlight{
						CategoryID: catID,
						Name:       name,
						Icon:       icon,
						Color:      color,
						Amount:     0,
					}
				}
				catTotals[catID].Amount += tx.Amount + tx.AdminFee
			}
		}
	}

	totalSaved := totalIncome - totalExpense
	var savingsRate float64
	if totalIncome > 0 {
		savingsRate = (totalSaved / totalIncome) * 100
		if savingsRate < 0 {
			savingsRate = 0
		}
	}

	// Find top expense category
	var topCategory *CategoryHighlight
	var maxCatAmt float64
	for _, cat := range catTotals {
		if cat.Amount > maxCatAmt {
			maxCatAmt = cat.Amount
			topCategory = cat
		}
	}
	if topCategory != nil && totalExpense > 0 {
		topCategory.Percentage = (topCategory.Amount / totalExpense) * 100
	}

	// Find biggest expense day
	var biggestDay *DayHighlight
	var maxDayAmt float64
	for day, amt := range dayTotals {
		if amt > maxDayAmt {
			maxDayAmt = amt
			biggestDay = &DayHighlight{
				Date:   day,
				Amount: amt,
			}
		}
	}

	// Determine Personality Badge
	badge := "Balanced Strategist ⚖️"
	desc := "Anda mengelola keuangan dengan seimbang dan terkontrol."

	if savingsRate >= 40 {
		badge = "Master Saver 🛡️"
		desc = "Luar biasa! Anda berhasil mengamankan lebih dari 40% pemasukan ke tabungan."
	} else if topCategory != nil && (topCategory.Name == "Makanan" || topCategory.Name == "Kuliner" || topCategory.Name == "Food & Beverage") {
		badge = "Gourmet Explorer 🍔"
		desc = "Kuliner dan makanan lezat menjadi bagian kebahagiaan terbesar Anda bulan ini!"
	} else if len(txs) >= 30 {
		badge = "Active Financial Tracker ⚡"
		desc = "Disiplin tinggi! Anda mencatat setiap detail transaksi harian dengan sangat rapi."
	}

	indonesianMonths := map[time.Month]string{
		time.January: "Januari", time.February: "Februari", time.March: "Maret",
		time.April: "April", time.May: "Mei", time.June: "Juni",
		time.July: "Juli", time.August: "Agustus", time.September: "September",
		time.October: "Oktober", time.November: "November", time.December: "Desember",
	}

	monthName := indonesianMonths[parsedTime.Month()]

	res := MonthlyWrappedResponse{
		MonthName:        monthName,
		Year:             parsedTime.Year(),
		TotalIncome:      totalIncome,
		TotalExpense:     totalExpense,
		TotalSaved:       totalSaved,
		SavingsRate:      savingsRate,
		TopCategory:      topCategory,
		BiggestDay:       biggestDay,
		TotalCount:       len(txs),
		PersonalityBadge: badge,
		BadgeDescription: desc,
	}

	utils.JSONResponse(w, http.StatusOK, res)
}
