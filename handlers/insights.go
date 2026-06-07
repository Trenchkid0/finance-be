package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

type InsightsResponse struct {
	SavingsRate         float64            `json:"savingsRate"`
	EmergencyFundMonths float64            `json:"emergencyFundMonths"`
	DebtToIncome        float64            `json:"debtToIncome"`
	Score               int                `json:"score"`
	Allocations         map[string]float64 `json:"allocations"`
	Recommendations     []string           `json:"recommendations"`
	AiSummary           string             `json:"aiSummary"`
}

type AIResponseText struct {
	Summary string `json:"summary"`
}

func InsightsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "id"
	}

	// 1. Fetch user accounts
	var accounts []database.FinanceAccount
	if err := database.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&accounts).Error; err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve accounts")
		return
	}

	var netWorth float64
	var liquidBalance float64
	var totalDebt float64
	allocBalances := map[string]float64{
		"bank":       0,
		"wallet":     0,
		"cash":       0,
		"investment": 0,
	}

	for _, acc := range accounts {
		balance := acc.Balance
		netWorth += balance

		if balance < 0 {
			totalDebt += math.Abs(balance)
		}

		switch acc.Type {
		case database.AccountTypeBank:
			allocBalances["bank"] += balance
			liquidBalance += balance
		case database.AccountTypeWallet:
			allocBalances["wallet"] += balance
			liquidBalance += balance
		case database.AccountTypeCash:
			allocBalances["cash"] += balance
			liquidBalance += balance
		case database.AccountTypeInvestment:
			allocBalances["investment"] += balance
		}
	}

	allocations := map[string]float64{
		"bank":       0,
		"wallet":     0,
		"cash":       0,
		"investment": 0,
	}
	if netWorth > 0 {
		for k, v := range allocBalances {
			pct := (v / netWorth) * 100
			if pct < 0 {
				pct = 0
			}
			allocations[k] = math.Round(pct*10) / 10
		}
	}

	// 2. Fetch income and expenses for past 30 days
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	var transactions []database.Transaction
	database.DB.Where("user_id = ? AND date >= ?", userID, thirtyDaysAgo).Find(&transactions)

	var monthlyIncome float64
	var monthlyExpense float64

	for _, tx := range transactions {
		if tx.Type == database.TransactionTypeIncome {
			monthlyIncome += tx.Amount
		} else if tx.Type == database.TransactionTypeExpense {
			monthlyExpense += tx.Amount
		}
	}

	// 3. Fetch past 90 days expenses for average monthly expenses
	ninetyDaysAgo := time.Now().AddDate(0, 0, -90)
	var ninetyDaysExpenses float64
	database.DB.Model(&database.Transaction{}).
		Where("user_id = ? AND type = ? AND date >= ?", userID, database.TransactionTypeExpense, ninetyDaysAgo).
		Select("COALESCE(SUM(amount), 0)").
		Row().
		Scan(&ninetyDaysExpenses)

	avgMonthlyExpense := ninetyDaysExpenses / 3.0
	if avgMonthlyExpense <= 0 {
		avgMonthlyExpense = monthlyExpense
	}

	// 4. Calculate core ratios
	savingsRate := 0.0
	if monthlyIncome > 0 {
		savingsRate = ((monthlyIncome - monthlyExpense) / monthlyIncome) * 100
	}

	emergencyFundMonths := 0.0
	if avgMonthlyExpense > 0 {
		emergencyFundMonths = liquidBalance / avgMonthlyExpense
	} else if liquidBalance > 0 {
		emergencyFundMonths = 12.0 // no expenses, safe
	}

	debtToIncome := 0.0
	if monthlyIncome > 0 {
		debtToIncome = totalDebt / monthlyIncome
	}

	// 5. Calculate Score
	score := 100

	// Savings rate penalty
	if savingsRate < 0 {
		score -= 30
	} else if savingsRate < 10 {
		score -= 20
	} else if savingsRate < 20 {
		score -= 10
	}

	// Emergency Fund penalty
	if emergencyFundMonths < 1 {
		score -= 30
	} else if emergencyFundMonths < 3 {
		score -= 20
	} else if emergencyFundMonths < 6 {
		score -= 10
	}

	// Debt penalty
	if debtToIncome > 0.4 {
		score -= 15
	} else if debtToIncome > 0.2 {
		score -= 5
	}

	if score < 0 {
		score = 0
	}

	// 6. Generate Rule-Based Recommendations
	var recommendations []string
	if lang == "id" {
		if savingsRate < 10 {
			recommendations = append(recommendations, "Tingkat tabungan Anda di bawah 10%. Pertimbangkan untuk meninjau kembali pengeluaran non-esensial bulan ini.")
		} else if savingsRate >= 20 {
			recommendations = append(recommendations, "Tingkat tabungan Anda sangat baik (di atas 20%). Pertahankan konsistensi ini!")
		} else {
			recommendations = append(recommendations, "Tingkat tabungan Anda cukup baik (10-20%), namun masih bisa dioptimalkan dengan membatasi pos pengeluaran impulsif.")
		}

		if emergencyFundMonths < 3 {
			recommendations = append(recommendations, "Dana darurat Anda hanya cukup untuk kurang dari 3 bulan. Prioritaskan alokasi tabungan untuk memperkuat pos dana darurat Anda.")
		} else if emergencyFundMonths >= 6 {
			recommendations = append(recommendations, "Dana darurat Anda sangat aman (lebih dari 6 bulan). Anda bisa mempertimbangkan untuk mengalokasikan kelebihan dana ke pos investasi.")
		} else {
			recommendations = append(recommendations, "Dana darurat Anda berada dalam rentang aman (3-6 bulan). Pertahankan posisi ini.")
		}

		if debtToIncome > 0.3 {
			recommendations = append(recommendations, "Rasio utang terhadap pendapatan Anda tinggi. Kurangi penggunaan kartu kredit atau utang konsumtif baru.")
		}

		if allocations["investment"] < 10 && netWorth > 10000000 {
			recommendations = append(recommendations, "Alokasi investasi Anda masih rendah. Pertimbangkan menempatkan dana dingin ke reksa dana atau instrumen rendah risiko.")
		}
	} else {
		if savingsRate < 10 {
			recommendations = append(recommendations, "Your savings rate is below 10%. Consider reviewing non-essential expenses this month.")
		} else if savingsRate >= 20 {
			recommendations = append(recommendations, "Your savings rate is excellent (above 20%). Keep up the great work!")
		} else {
			recommendations = append(recommendations, "Your savings rate is decent (10-20%), but could be optimized by trimming impulse purchases.")
		}

		if emergencyFundMonths < 3 {
			recommendations = append(recommendations, "Your emergency fund covers less than 3 months. Prioritize building a stronger financial cushion.")
		} else if emergencyFundMonths >= 6 {
			recommendations = append(recommendations, "Your emergency fund is fully secure (6+ months). You can start routing excess funds to investments.")
		} else {
			recommendations = append(recommendations, "Your emergency fund is in the healthy range (3-6 months). Maintain this balance.")
		}

		if debtToIncome > 0.3 {
			recommendations = append(recommendations, "Your debt-to-income ratio is high. Focus on paying down existing debt and avoid new consumer liabilities.")
		}

		if allocations["investment"] < 10 && netWorth > 1000000 {
			recommendations = append(recommendations, "Your investment allocation is low. Consider allocating surplus funds to mutual funds or low-risk assets.")
		}
	}

	// 7. Heuristic AI Summary OR DeepSeek Call
	var aiSummary string
	if services.IsDeepSeekConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		systemPrompt := "You are a professional financial planner and AI coach. You must return a JSON object with a single string field 'summary' containing a personalized summary of the user's finances. Keep it short (2-3 sentences), highly actionable, and encouraging."
		if lang == "id" {
			systemPrompt += " Write the response in Indonesian language."
		} else {
			systemPrompt += " Write the response in English language."
		}

		userPrompt := fmt.Sprintf(
			"User Financial Summary:\n- Score: %d/100\n- Savings Rate: %.1f%%\n- Emergency Fund Months: %.1f\n- Debt-to-Income: %.1f%%\n- Liquid Balance: %.2f\n- Investment Balance: %.2f",
			score, savingsRate, emergencyFundMonths, debtToIncome*100, liquidBalance, allocBalances["investment"],
		)

		var target AIResponseText
		err := services.DeepSeekJSON(ctx, systemPrompt, userPrompt, &target)
		if err == nil && target.Summary != "" {
			aiSummary = target.Summary
		}
	}

	if aiSummary == "" {
		// Heuristic fallback summary
		if lang == "id" {
			if score >= 80 {
				aiSummary = fmt.Sprintf("Kondisi keuangan Anda sangat prima dengan skor kesehatan %d/100! Tingkat tabungan Anda berada pada %.1f%% dan dana darurat mencakup %.1f bulan pengeluaran. Teruskan kedisiplinan finansial ini.", score, savingsRate, emergencyFundMonths)
			} else if score >= 50 {
				aiSummary = fmt.Sprintf("Kesehatan finansial Anda tergolong cukup baik (skor %d/100). Meskipun tingkat tabungan Anda mencapai %.1f%%, pastikan dana darurat Anda yang saat ini setara %.1f bulan pengeluaran terus ditingkatkan ke batas aman minimum 3 bulan.", score, savingsRate, emergencyFundMonths)
			} else {
				aiSummary = fmt.Sprintf("Saat ini kesehatan keuangan Anda memerlukan perhatian ekstra (skor %d/100). Prioritaskan untuk menekan pengeluaran bulanan agar tingkat tabungan bergeser positif dari posisi %.1f%% dan isi kembali tabungan dana darurat Anda.", score, savingsRate)
			}
		} else {
			if score >= 80 {
				aiSummary = fmt.Sprintf("Your financial health is outstanding with a score of %d/100! Your savings rate is at %.1f%% and your emergency fund covers %.1f months of expenses. Keep up the disciplined work.", score, savingsRate, emergencyFundMonths)
			} else if score >= 50 {
				aiSummary = fmt.Sprintf("Your financial health is decent (score %d/100). Your savings rate is %.1f%%, but aim to boost your emergency fund from %.1f months up to the recommended 3-month safety threshold.", score, savingsRate, emergencyFundMonths)
			} else {
				aiSummary = fmt.Sprintf("Your finances currently need immediate attention (score %d/100). Focus on cutting unnecessary expenses to bring your savings rate up from %.1f%% and replenish your emergency fund.", score, savingsRate)
			}
		}
	}

	utils.JSONResponse(w, http.StatusOK, InsightsResponse{
		SavingsRate:         math.Round(savingsRate*10) / 10,
		EmergencyFundMonths: math.Round(emergencyFundMonths*10) / 10,
		DebtToIncome:        math.Round(debtToIncome*100*10) / 10,
		Score:               score,
		Allocations:         allocations,
		Recommendations:     recommendations,
		AiSummary:           aiSummary,
	})
}
